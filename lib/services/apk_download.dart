import 'dart:async';
import 'dart:ffi';
import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:open_filex/open_filex.dart';
import 'package:path_provider/path_provider.dart';

import 'app_update.dart';

/// Android ABI token used by the release asset names (`-arm64-v8a.apk`, ...).
String? currentApkAbi() {
  switch (Abi.current()) {
    case Abi.androidArm64:
      return 'arm64-v8a';
    case Abi.androidArm:
      return 'armeabi-v7a';
    case Abi.androidX64:
      return 'x86_64';
    case Abi.androidIA32:
      return 'x86';
    default:
      return null;
  }
}

const _abiTokens = ['arm64-v8a', 'armeabi-v7a', 'x86_64', 'x86'];

/// Picks the release APK that matches [abi]. Falls back to a universal APK
/// (one whose name carries no ABI token); never guesses a different ABI.
ReleaseAsset? selectApkAsset(List<ReleaseAsset> assets, String? abi) {
  final apks = assets
      .where((asset) => asset.name.toLowerCase().endsWith('.apk'))
      .toList();
  if (apks.isEmpty) return null;
  if (abi != null) {
    for (final asset in apks) {
      if (asset.name.toLowerCase().contains('-${abi.toLowerCase()}')) {
        return asset;
      }
    }
  }
  for (final asset in apks) {
    final name = asset.name.toLowerCase();
    if (!_abiTokens.any(name.contains)) return asset;
  }
  return null;
}

/// Human-readable size, e.g. `18.4 MB`.
String formatFileSize(int bytes) {
  if (bytes <= 0) return '0 MB';
  const mb = 1024 * 1024;
  if (bytes >= mb) return '${(bytes / mb).toStringAsFixed(1)} MB';
  return '${(bytes / 1024).toStringAsFixed(0)} KB';
}

enum ApkDownloadStatus { idle, downloading, paused, downloaded, failed }

@immutable
class ApkDownloadState {
  const ApkDownloadState({
    this.status = ApkDownloadStatus.idle,
    this.received = 0,
    this.total = 0,
    this.filePath,
    this.error,
  });

  final ApkDownloadStatus status;
  final int received;
  final int total;
  final String? filePath;
  final String? error;

  /// 0..1, or null when the server did not send a length.
  double? get progress =>
      total > 0 ? (received / total).clamp(0.0, 1.0).toDouble() : null;

  bool get canInstall =>
      status == ApkDownloadStatus.downloaded && filePath != null;
}

/// Downloads a release APK with pause/resume (HTTP `Range`) and cancel.
class ApkDownloader {
  ApkDownloader({Future<Directory> Function()? directory})
    : _directory = directory ?? getApplicationSupportDirectory;

  /// Recreated per attempt: pausing closes the socket, so the resume needs a
  /// fresh one. libcurl buffers whole bodies, which is no way to fetch an APK.
  HttpClient? _client;
  final Future<Directory> Function() _directory;
  final ValueNotifier<ApkDownloadState> state = ValueNotifier(
    const ApkDownloadState(),
  );

  StreamSubscription<List<int>>? _subscription;
  IOSink? _sink;
  File? _file;
  Completer<void>? _done;
  int _received = 0;
  int _total = 0;
  bool _paused = false;
  bool _disposed = false;

  /// Downloads [url] or resumes a paused download from its partial file.
  Future<void> start(
    Uri url, {
    required String fileName,
    int totalBytes = 0,
  }) async {
    if (_disposed || state.value.status == ApkDownloadStatus.downloading) {
      return;
    }
    final file = _file ??= await _targetFile(fileName);
    final resuming = state.value.status == ApkDownloadStatus.paused;
    var offset = resuming && await file.exists() ? await file.length() : 0;
    _received = offset;
    _total = totalBytes > 0 ? totalBytes : state.value.total;
    _paused = false;
    _publish(ApkDownloadStatus.downloading);
    try {
      final request = await (_client ??= HttpClient()).getUrl(url);
      // GitHub's asset CDN honours Range, so a paused download resumes
      // instead of starting from zero.
      if (offset > 0) {
        request.headers.set(HttpHeaders.rangeHeader, 'bytes=$offset-');
      }
      final response = await request.close();
      if (_disposed) return;
      if (response.statusCode != 200 && response.statusCode != 206) {
        throw HttpException('HTTP ${response.statusCode}', uri: url);
      }
      final appended = offset > 0 && response.statusCode == 206;
      if (!appended) offset = 0;
      _received = offset;
      final length = response.contentLength;
      if (length > 0) _total = offset + length;
      _sink = file.openWrite(mode: appended ? FileMode.append : FileMode.write);
      _publish(ApkDownloadStatus.downloading);
      _done = Completer<void>();
      _subscription = response.listen(
        (chunk) {
          _received += chunk.length;
          _sink?.add(chunk);
          _publish(ApkDownloadStatus.downloading);
        },
        onError: (Object error, StackTrace _) => unawaited(_fail(file, error)),
        onDone: () => unawaited(_finish(file)),
        cancelOnError: true,
      );
      await _done?.future;
    } catch (error) {
      if (!_paused) await _fail(file, error);
    }
  }

  Future<void> pause() async {
    if (state.value.status != ApkDownloadStatus.downloading) return;
    _paused = true;
    await _stopStream();
    _publish(ApkDownloadStatus.paused);
    _complete();
  }

  Future<void> cancel() async {
    _paused = true;
    await _stopStream();
    final file = _file;
    _file = null;
    _received = 0;
    _total = 0;
    _paused = false;
    if (file != null && await file.exists()) await file.delete();
    if (_disposed) return;
    state.value = const ApkDownloadState();
    _complete();
  }

  Future<void> dispose() async {
    _disposed = true;
    _paused = true;
    await _stopStream();
    state.dispose();
    _complete();
  }

  Future<File> _targetFile(String fileName) async {
    final dir = await _directory();
    final safe = fileName.replaceAll(RegExp(r'[^\w.+-]'), '_');
    final file = File('${dir.path}${Platform.pathSeparator}$safe');
    await file.parent.create(recursive: true);
    return file;
  }

  Future<void> _finish(File file) async {
    await _stopStream();
    if (_paused) return;
    _publish(ApkDownloadStatus.downloaded, path: file.path);
    _complete();
  }

  Future<void> _fail(File file, Object error) async {
    await _stopStream();
    if (_paused) return;
    _paused = false;
    if (_disposed) return;
    state.value = ApkDownloadState(
      status: ApkDownloadStatus.failed,
      received: _received,
      total: _total,
      filePath: file.path,
      error: '$error',
    );
    _complete();
  }

  Future<void> _stopStream() async {
    await _subscription?.cancel();
    _subscription = null;
    _client?.close(force: true);
    _client = null;
    try {
      await _sink?.flush();
      await _sink?.close();
    } catch (_) {
      // The file may already be closed after an error.
    }
    _sink = null;
  }

  void _publish(ApkDownloadStatus status, {String? path}) {
    if (_disposed) return;
    state.value = ApkDownloadState(
      status: status,
      received: _received,
      total: _total,
      filePath: path,
    );
  }

  void _complete() {
    if (_done?.isCompleted == false) _done!.complete();
    _done = null;
  }
}

/// Hands the downloaded APK to Android's package installer.
Future<void> installApkFile(String path) async {
  final result = await OpenFilex.open(path);
  if (result.type != ResultType.done) {
    throw StateError(
      result.message.isEmpty ? 'Installer tidak terbuka' : result.message,
    );
  }
}
