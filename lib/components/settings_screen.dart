import 'package:cached_network_image/cached_network_image.dart';
import 'package:material_ui/material_ui.dart';

import '../services/anilist_auth.dart';
import '../services/image_cache.dart';

/// Settings tab: AniList account and image cache.
class SettingsScreen extends StatefulWidget {
  const SettingsScreen({
    super.key,
    this.cacheSize = imageCacheSize,
    this.clearCache = clearImageCache,
    this.loadProfile = loadAniListProfile,
    this.signIn = signInWithAniList,
    this.signOut = signOutAniList,
  });

  /// Injected in tests.
  final Future<int> Function() cacheSize;
  final Future<void> Function() clearCache;
  final Future<AniListProfile?> Function() loadProfile;
  final Future<void> Function() signIn;
  final Future<void> Function() signOut;

  @override
  State<SettingsScreen> createState() => _SettingsScreenState();
}

class _SettingsScreenState extends State<SettingsScreen> {
  int? _size;
  bool _clearing = false;
  AniListProfile? _profile;
  bool _loadingProfile = true;
  bool _signingIn = false;

  @override
  void initState() {
    super.initState();
    _load();
    _refreshProfile();
  }

  Future<void> _load() async {
    final size = await widget.cacheSize();
    if (mounted) setState(() => _size = size);
  }

  Future<void> _refreshProfile() async {
    final profile = await widget.loadProfile();
    if (!mounted) return;
    setState(() {
      _profile = profile;
      _loadingProfile = false;
    });
  }

  Future<void> _login() async {
    setState(() => _signingIn = true);
    try {
      await widget.signIn();
      await _refreshProfile();
    } catch (_) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(content: Text('Login AniList gagal')),
        );
      }
    } finally {
      if (mounted) setState(() => _signingIn = false);
    }
  }

  Future<void> _logout() async {
    await widget.signOut();
    if (!mounted) return;
    setState(() => _profile = null);
    ScaffoldMessenger.of(context)
        .showSnackBar(const SnackBar(content: Text('Logout dari AniList')));
  }

  Future<void> _clear() async {
    setState(() => _clearing = true);
    await widget.clearCache();
    if (!mounted) return;
    setState(() {
      _size = 0;
      _clearing = false;
    });
    ScaffoldMessenger.of(context)
        .showSnackBar(const SnackBar(content: Text('Cache dihapus')));
  }

  @override
  Widget build(BuildContext context) {
    final size = _size;
    return Scaffold(
      appBar: AppBar(title: const Text('Setting')),
      body: ListView(
        children: [
          _account(),
          const Divider(height: 1),
          ListTile(
            leading: const Icon(Icons.image_outlined),
            title: const Text('Cache gambar'),
            subtitle: Text(
              size == null ? 'Menghitung...' : formatCacheSize(size),
            ),
          ),
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 8, 16, 0),
            child: FilledButton(
              onPressed: _clearing || size == null ? null : _clear,
              child: Text(_clearing ? 'Menghapus...' : 'Hapus cache'),
            ),
          ),
        ],
      ),
    );
  }

  Widget _account() {
    if (_loadingProfile) {
      return const ListTile(
        leading: SizedBox(
          width: 32,
          height: 32,
          child: CircularProgressIndicator(strokeWidth: 2),
        ),
        title: Text('Memuat profil AniList...'),
      );
    }

    final profile = _profile;
    if (profile == null) {
      return ListTile(
        leading: const Icon(Icons.login),
        title: const Text('Login dengan AniList'),
        subtitle: const Text('Belum login'),
        trailing: _signingIn
            ? const SizedBox(
                width: 20,
                height: 20,
                child: CircularProgressIndicator(strokeWidth: 2),
              )
            : const Icon(Icons.chevron_right),
        onTap: _signingIn ? null : _login,
      );
    }

    return ListTile(
      leading: CircleAvatar(
        backgroundImage: profile.avatarUrl.isEmpty
            ? null
            : CachedNetworkImageProvider(profile.avatarUrl),
        child: profile.avatarUrl.isEmpty
            ? const Icon(Icons.person_outline)
            : null,
      ),
      title: Text(profile.name),
      subtitle: Text(
        'ID ${profile.id} · ${profile.animeCount} anime · '
        '${profile.episodesWatched} episode',
      ),
      trailing: TextButton(onPressed: _logout, child: const Text('Keluar')),
    );
  }
}
