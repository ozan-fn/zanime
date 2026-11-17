import 'dart:async';

import 'package:flutter_markdown/flutter_markdown.dart';
import 'package:material_ui/material_ui.dart';
import 'package:url_launcher/url_launcher.dart';

import '../../services/app_update.dart';

/// Update check as a settings list page: current version, latest release and
/// what changed. No dialog.
class UpdateScreen extends StatefulWidget {
  const UpdateScreen({
    super.key,
    this.loadRelease = fetchLatestRelease,
    this.loadVersion = installedAppVersion,
  });

  /// Injected in tests.
  final Future<AppRelease> Function() loadRelease;
  final Future<String> Function() loadVersion;

  @override
  State<UpdateScreen> createState() => _UpdateScreenState();
}

class _UpdateScreenState extends State<UpdateScreen> {
  String? _version;
  AppRelease? _release;
  bool _loading = true;
  bool _failed = false;
  Timer? _timer;

  @override
  void initState() {
    super.initState();
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
              Padding(
                padding: const EdgeInsets.fromLTRB(16, 4, 16, 8),
                child: FilledButton.icon(
                  onPressed: () => _openReleases(context),
                  icon: const Icon(Icons.open_in_browser, size: 18),
                  label: const Text('Buka halaman rilis'),
                ),
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

  Future<void> _openReleases(BuildContext context) async {
    final opened = await openLatestRelease();
    if (!opened && context.mounted) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('Tidak bisa membuka browser')),
      );
    }
  }
}
