import 'dart:async';

import 'package:flutter_markdown/flutter_markdown.dart';
import 'package:material_ui/material_ui.dart';
import 'package:url_launcher/url_launcher.dart';

import '../../services/apk_download.dart';
import '../../services/app_update.dart';

/// Update check as a settings list page: current version, latest release,
/// release notes and an in-app APK download with pause/cancel/install.
class UpdateScreen extends StatefulWidget {
  const UpdateScreen({
    super.key,
    this.loadRelease = fetchLatestRelease,
    this.loadVersion = installedAppVersion,
    this.downloader,
    this.install,
    this.abi,
  });

  /// Injected in tests.
  final Future<AppRelease> Function() loadRelease;
  final Future<String> Function() loadVersion;
  final ApkDownloader? downloader;
  final Future<void> Function(String path)? install;
  final String? abi;

  @override
  State<UpdateScreen> createState() => _UpdateScreenState();
}

class _UpdateScreenState extends State<UpdateScreen> {
  String? _version;
  AppRelease? _release;
  bool _loading = true;
  bool _failed = false;
  Timer? _timer;
  late final ApkDownloader _downloader;
  late final bool _ownsDownloader;

  @override
  void initState() {
    super.initState();
    _ownsDownloader = widget.downloader == null;
    _downloader = widget.downloader ?? ApkDownloader();
    // Checked when the page opens, then re-checked once a minute while it is
    // open (the manual refresh button bypasses the wait).
    _check();
    _timer = Timer.periodic(
      const Duration(minutes: 1),
      (_) => _check(silent: true),
    );
  }

  @override
  void dispose() {
    _timer?.cancel();
    if (_ownsDownloader) unawaited(_downloader.dispose());
    super.dispose();
  }

  Future<void> _check({bool silent = false}) async {
    if (!silent) {
      setState(() {
        _loading = true;
        _failed = false;
      });
    }
    String? version;
    AppRelease? release;
    var failed = false;
    try {
      version = await widget.loadVersion();
      release = await widget.loadRelease();
    } catch (_) {
      failed = true;
    }
    if (!mounted) return;
    // A background refresh must not wipe the result already on screen.
    if (silent && failed) return;
    if (version != null && release != null) {
      cacheUpdateInfo(version, release);
    }
    setState(() {
      if (version != null) _version = version;
      _release = release;
      _loading = false;
      _failed = failed;
    });
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final release = _release;
    final version = _version;
    final newer =
        release != null &&
        version != null &&
        isNewerVersion(release.version, version);
    final asset = release == null
        ? null
        : selectApkAsset(release.assets, widget.abi ?? currentApkAbi());
    return Scaffold(
      appBar: AppBar(
        title: const Text('Cek update'),
        actions: [
          IconButton(
            onPressed: _loading ? null : _check,
            tooltip: 'Periksa lagi',
            icon: const Icon(Icons.refresh),
          ),
        ],
      ),
      body: ListView(
        children: [
          if (_loading)
            const ListTile(
              leading: SizedBox(
                width: 24,
                height: 24,
                child: CircularProgressIndicator(strokeWidth: 2),
              ),
              title: Text('Mengecek update...'),
            ),
          if (_failed)
            ListTile(
              leading: Icon(
                Icons.error_outline,
                color: theme.colorScheme.error,
              ),
              title: const Text('Gagal mengecek update'),
              subtitle: const Text('Periksa koneksi lalu coba lagi'),
            ),
          if (version != null)
            ListTile(
              leading: const Icon(Icons.phone_android_outlined),
              title: const Text('Versi terpasang'),
              trailing: Text(version),
            ),
          if (release != null) ...[
            ListTile(
              leading: const Icon(Icons.new_releases_outlined),
              title: const Text('Versi terbaru'),
              trailing: Text(release.version),
            ),
            ListTile(
              leading: Icon(
                newer ? Icons.system_update_alt : Icons.check_circle_outline,
              ),
              title: Text(newer ? 'Update tersedia' : 'Sudah versi terbaru'),
              subtitle: Text(
                newer
                    ? 'Versi $version → ${release.version}'
                    : 'Berdasarkan release di GitHub',
              ),
            ),
            if (newer)
              if (asset == null)
                ListTile(
                  leading: Icon(
                    Icons.error_outline,
                    color: theme.colorScheme.error,
                  ),
                  title: const Text('APK tidak tersedia di rilis ini'),
                  subtitle: const Text('Coba periksa lagi nanti'),
                )
              else
                ValueListenableBuilder<ApkDownloadState>(
                  valueListenable: _downloader.state,
                  builder: (context, download, _) =>
                      _downloadSection(download, asset),
                ),
            if (release.name.isNotEmpty && release.name != release.version)
              ListTile(
                leading: const Icon(Icons.sell_outlined),
                title: const Text('Rilis'),
                subtitle: Text(release.name),
              ),
            if (release.notes.isNotEmpty) ...[
              const Padding(
                padding: EdgeInsets.fromLTRB(16, 16, 16, 8),
                child: Row(
                  children: [
                    Icon(Icons.notes_outlined, size: 18),
                    SizedBox(width: 8),
                    Text('Catatan rilis'),
                  ],
                ),
              ),
              Padding(
                padding: const EdgeInsets.fromLTRB(4, 0, 16, 24),
                // Markdown, full length: the page itself scrolls.
                child: MarkdownBody(
                  data: release.notes,
                  onTapLink: (_, href, _) {
                    if (href != null) launchUrl(Uri.parse(href));
                  },
                ),
              ),
            ],
          ],
        ],
      ),
    );
  }

  Widget _downloadSection(ApkDownloadState download, ReleaseAsset asset) {
    final theme = Theme.of(context);
    final status = download.status;
    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 4, 16, 8),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          if (status == ApkDownloadStatus.idle)
            FilledButton.icon(
              onPressed: () => _start(asset),
              icon: const Icon(Icons.download, size: 18),
              label: const Text('Unduh update'),
            ),
          if (status == ApkDownloadStatus.failed) ...[
            Text(
              'Unduhan gagal',
              style: theme.textTheme.titleSmall?.copyWith(
                color: theme.colorScheme.error,
              ),
            ),
            if (download.error != null)
              Text(
                download.error!,
                maxLines: 2,
                overflow: TextOverflow.ellipsis,
                style: theme.textTheme.bodySmall?.copyWith(
                  color: theme.colorScheme.onSurfaceVariant,
                ),
              ),
            const SizedBox(height: 8),
            FilledButton.icon(
              onPressed: () => _start(asset),
              icon: const Icon(Icons.download, size: 18),
              label: const Text('Unduh ulang'),
            ),
          ],
          if (status == ApkDownloadStatus.downloading ||
              status == ApkDownloadStatus.paused) ...[
            LinearProgressIndicator(value: download.progress),
            const SizedBox(height: 8),
            Text(
              [
                status == ApkDownloadStatus.paused
                    ? 'Dihentikan sementara'
                    : 'Mengunduh APK',
                if (download.total > 0)
                  '${formatFileSize(download.received)} / '
                      '${formatFileSize(download.total)}',
              ].join(' · '),
              style: theme.textTheme.bodySmall,
            ),
            const SizedBox(height: 8),
            Row(
              children: [
                Expanded(
                  child: status == ApkDownloadStatus.paused
                      ? FilledButton.icon(
                          onPressed: () => _start(asset),
                          icon: const Icon(Icons.play_arrow, size: 18),
                          label: const Text('Lanjutkan'),
                        )
                      : OutlinedButton.icon(
                          onPressed: _downloader.pause,
                          icon: const Icon(Icons.pause, size: 18),
                          label: const Text('Pause'),
                        ),
                ),
                const SizedBox(width: 8),
                Expanded(
                  child: OutlinedButton(
                    onPressed: _downloader.cancel,
                    child: const Text('Batal'),
                  ),
                ),
              ],
            ),
          ],
          if (status == ApkDownloadStatus.downloaded) ...[
            Text(
              'Unduhan selesai · ${formatFileSize(download.total)}',
              style: theme.textTheme.bodySmall,
            ),
            const SizedBox(height: 8),
            FilledButton.icon(
              onPressed: () => _install(download),
              icon: const Icon(Icons.install_mobile, size: 18),
              label: const Text('Pasang update'),
            ),
            const SizedBox(height: 8),
            OutlinedButton(
              onPressed: _downloader.cancel,
              child: const Text('Batal'),
            ),
          ],
        ],
      ),
    );
  }

  Future<void> _start(ReleaseAsset asset) async {
    await _downloader.start(
      Uri.parse(asset.url),
      fileName: asset.name,
      totalBytes: asset.size,
    );
  }

  Future<void> _install(ApkDownloadState download) async {
    final path = download.filePath;
    if (path == null) return;
    try {
      await (widget.install ?? installApkFile)(path);
    } catch (_) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(
          content: Text('Gagal membuka installer. Izinkan pemasangan APK.'),
        ),
      );
    }
  }
}
