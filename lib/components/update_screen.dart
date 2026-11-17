import 'package:material_ui/material_ui.dart';
import 'package:url_launcher/url_launcher.dart';

import '../services/app_update.dart';

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

  @override
  void initState() {
    super.initState();
    _check();
  }

  Future<void> _check() async {
    setState(() {
      _loading = true;
      _failed = false;
    });
    String? version;
    AppRelease? release;
    try {
      version = await widget.loadVersion();
      release = await widget.loadRelease();
    } catch (_) {
      _failed = true;
    }
    if (!mounted) return;
    setState(() {
      if (version != null) _version = version;
      _release = release;
      _loading = false;
    });
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final release = _release;
    final version = _version;
    final newer = release != null &&
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
              title: Text(
                newer ? 'Update tersedia' : 'Sudah versi terbaru',
              ),
              subtitle: Text(
                newer
                    ? 'Versi $version → ${release.version}'
                    : 'Berdasarkan release di GitHub',
              ),
            ),
            if (newer)
              ListTile(
                leading: const Icon(Icons.open_in_browser),
                title: const Text('Buka halaman rilis'),
                subtitle: const Text(releasesUrl),
                trailing: const Icon(Icons.chevron_right),
                onTap: () => _openReleases(context),
              ),
            if (release.name.isNotEmpty && release.name != release.version)
              ListTile(
                leading: const Icon(Icons.sell_outlined),
                title: const Text('Rilis'),
                subtitle: Text(release.name),
              ),
            if (release.notes.isNotEmpty)
              ListTile(
                leading: const Icon(Icons.notes_outlined),
                title: const Text('Catatan rilis'),
                subtitle: Text(release.notes),
              ),
          ],
        ],
      ),
    );
  }

  Future<void> _openReleases(BuildContext context) async {
    final opened = await launchUrl(
      Uri.parse(releasesUrl),
      mode: LaunchMode.externalApplication,
    );
    if (!opened && context.mounted) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('Tidak bisa membuka browser')),
      );
    }
  }
}
