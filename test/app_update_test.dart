import 'package:flutter_test/flutter_test.dart';
import 'package:material_ui/material_ui.dart';
import 'package:test1/screens/profile/update_screen.dart';
import 'package:test1/services/apk_download.dart';
import 'package:test1/services/app_update.dart';

class _FakeDownloader extends ApkDownloader {
  @override
  Future<void> start(Uri url, {required String fileName, int totalBytes = 0}) {
    state.value = const ApkDownloadState(
      status: ApkDownloadStatus.downloading,
      received: 10,
      total: 100,
    );
    return Future<void>.value();
  }

  @override
  Future<void> pause() async {
    state.value = const ApkDownloadState(
      status: ApkDownloadStatus.paused,
      received: 10,
      total: 100,
    );
  }

  @override
  Future<void> cancel() async {
    state.value = const ApkDownloadState();
  }
}

const _release = AppRelease(
  version: '1.1.0',
  name: 'Zanime v1.1.0',
  notes: 'Full Changelog',
  url: 'https://github.com/ozan-fn/zanime/releases/tag/v1.1.0',
  assets: [
    ReleaseAsset(
      name: 'zanime-v1.1.0-arm64-v8a.apk',
      url: 'https://example.com/arm64.apk',
      size: 100,
    ),
    ReleaseAsset(
      name: 'zanime-v1.1.0-armeabi-v7a.apk',
      url: 'https://example.com/arm.apk',
      size: 100,
    ),
  ],
);

void main() {
  test('shortVersion keeps only major.minor.patch', () {
    expect(shortVersion('v1.0.1+8'), '1.0.1');
    expect(shortVersion('1.2.3'), '1.2.3');
    expect(shortVersion('v2.0.0-beta.1'), '2.0.0');
    expect(shortVersion('nightly'), isNull);
    expect(parseVersion('v1.0.1+8'), [1, 0, 1]);
  });

  test('version comparison ignores build metadata', () {
    expect(isNewerVersion('v1.0.1+8', '1.0.1'), isFalse);
    expect(isNewerVersion('v1.0.1+9', '1.0.1'), isFalse);
    expect(isNewerVersion('1.1.0', '1.0.1'), isTrue);
    expect(isNewerVersion('1.0.0', '1.0.1'), isFalse);
    expect(isNewerVersion('2.0.0', '1.9.9'), isTrue);
    expect(isNewerVersion('nightly', '1.0.1'), isFalse);
  });

  test('selectApkAsset picks the device ABI and never a different one', () {
    expect(
      selectApkAsset(_release.assets, 'arm64-v8a')?.url,
      contains('arm64'),
    );
    expect(
      selectApkAsset(_release.assets, 'armeabi-v7a')?.url,
      contains('/arm.apk'),
    );
    expect(
      selectApkAsset(_release.assets, 'x86_64'),
      isNull,
      reason: 'no matching ABI: do not install an incompatible APK',
    );
    expect(
      selectApkAsset(const [
        ReleaseAsset(name: 'zanime-universal.apk', url: 'u'),
      ], 'arm64-v8a')?.url,
      'u',
      reason: 'a universal APK has no ABI token and works everywhere',
    );
    expect(selectApkAsset(const [], 'arm64-v8a'), isNull);
  });

  test('formatFileSize shows KB below one megabyte', () {
    expect(formatFileSize(0), '0 MB');
    expect(formatFileSize(512 * 1024), '512 KB');
    expect(formatFileSize(2 * 1024 * 1024), '2.0 MB');
  });

  testWidgets('UpdateScreen lists versions and offers an in-app download', (
    tester,
  ) async {
    await tester.pumpWidget(
      MaterialApp(
        home: UpdateScreen(
          loadVersion: () async => '1.0.1',
          loadRelease: () async => _release,
          abi: 'arm64-v8a',
          downloader: _FakeDownloader(),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Cek update'), findsOneWidget);
    expect(find.text('Versi terpasang'), findsOneWidget);
    expect(find.text('1.0.1'), findsOneWidget);
    expect(find.text('Versi terbaru'), findsOneWidget);
    expect(find.text('1.1.0'), findsOneWidget);
    expect(find.text('Update tersedia'), findsOneWidget);
    expect(find.text('Versi 1.0.1 → 1.1.0'), findsOneWidget);
    expect(find.text('Unduh update'), findsOneWidget);
    expect(find.text('Buka halaman rilis'), findsNothing);
    expect(find.text('Catatan rilis'), findsOneWidget);
  });

  testWidgets('UpdateScreen offers pause and cancel while downloading', (
    tester,
  ) async {
    final downloader = _FakeDownloader();
    await tester.pumpWidget(
      MaterialApp(
        home: UpdateScreen(
          loadVersion: () async => '1.0.1',
          loadRelease: () async => _release,
          abi: 'arm64-v8a',
          downloader: downloader,
        ),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.text('Unduh update'));
    await tester.pump();

    expect(find.text('Pause'), findsOneWidget);
    expect(find.text('Batal'), findsOneWidget);
    expect(find.byType(LinearProgressIndicator), findsOneWidget);

    await tester.tap(find.text('Pause'));
    await tester.pump();

    expect(find.text('Lanjutkan'), findsOneWidget);
    expect(find.text('Batal'), findsOneWidget);
    expect(find.text('Pause'), findsNothing);
  });

  testWidgets('UpdateScreen installs a downloaded APK in the app', (
    tester,
  ) async {
    final downloader = _FakeDownloader();
    final installed = <String>[];
    await tester.pumpWidget(
      MaterialApp(
        home: UpdateScreen(
          loadVersion: () async => '1.0.1',
          loadRelease: () async => _release,
          abi: 'arm64-v8a',
          downloader: downloader,
          install: (path) async => installed.add(path),
        ),
      ),
    );
    await tester.pumpAndSettle();

    downloader.state.value = const ApkDownloadState(
      status: ApkDownloadStatus.downloaded,
      received: 100,
      total: 100,
      filePath: 'C:/tmp/zanime.apk',
    );
    await tester.pump();

    expect(find.text('Pasang update'), findsOneWidget);
    await tester.tap(find.text('Pasang update'));
    await tester.pump();

    expect(installed, ['C:/tmp/zanime.apk']);
  });

  testWidgets('UpdateScreen ignores the release build number', (tester) async {
    await tester.pumpWidget(
      MaterialApp(
        home: UpdateScreen(
          loadVersion: () async => '1.0.1',
          loadRelease: () async => const AppRelease(
            version: '1.0.1',
            name: 'Zanime v1.0.1',
            notes: '',
            url: '',
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Sudah versi terbaru'), findsOneWidget);
    expect(find.text('Update tersedia'), findsNothing);
    expect(find.text('Unduh update'), findsNothing);
    expect(find.text('Buka halaman rilis'), findsNothing);
  });

  testWidgets('UpdateScreen offers a retry when the check fails', (
    tester,
  ) async {
    await tester.pumpWidget(
      MaterialApp(
        home: UpdateScreen(
          loadVersion: () async => '1.0.1',
          loadRelease: () async => throw Exception('offline'),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Gagal mengecek update'), findsOneWidget);
    expect(
      find.text('1.0.1'),
      findsOneWidget,
      reason: 'still shows app version',
    );
    expect(find.byTooltip('Periksa lagi'), findsOneWidget);
  });
}
