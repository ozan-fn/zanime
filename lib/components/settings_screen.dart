import 'package:cached_network_image/cached_network_image.dart';
import 'package:material_ui/material_ui.dart';

import '../services/anilist_auth.dart';
import '../services/app_settings.dart';
import '../services/image_cache.dart';
import 'anilist_list_screen.dart';

const _zinc900 = Color(0xFF18181B);
const _zinc800 = Color(0xFF27272A);

const _statusColors = {
  'CURRENT': Color(0xFF4ADE80),
  'COMPLETED': Color(0xFF60A5FA),
  'PLANNING': Color(0xFFFBBF24),
  'REPEATING': Color(0xFF22D3EE),
  'PAUSED': Color(0xFFA78BFA),
  'DROPPED': Color(0xFFF87171),
};

const _statusIcons = {
  'CURRENT': Icons.play_arrow,
  'COMPLETED': Icons.check,
  'PLANNING': Icons.bookmark_outline,
  'REPEATING': Icons.replay,
  'PAUSED': Icons.pause,
  'DROPPED': Icons.close,
};

/// Profile tab: AniList account, list totals and app settings.
class SettingsScreen extends StatefulWidget {
  const SettingsScreen({
    super.key,
    this.cacheSize = imageCacheSize,
    this.clearCache = clearImageCache,
    this.loadProfile = loadAniListProfile,
    this.loadLists = fetchAniListLists,
    this.signIn = signInWithAniList,
    this.signOut = signOutAniList,
  });

  /// Injected in tests.
  final Future<int> Function() cacheSize;
  final Future<void> Function() clearCache;
  final Future<AniListProfile?> Function() loadProfile;
  final Future<Map<String, List<WatchingAnime>>?> Function() loadLists;
  final Future<void> Function() signIn;
  final Future<void> Function() signOut;

  @override
  State<SettingsScreen> createState() => _SettingsScreenState();
}

class _SettingsScreenState extends State<SettingsScreen> {
  int? _size;
  bool _clearing = false;
  AniListProfile? _profile;
  Map<String, List<WatchingAnime>>? _lists;
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
    final lists = profile == null ? null : await widget.loadLists();
    if (!mounted) return;
    setState(() {
      _profile = profile;
      _lists = lists;
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
    setState(() {
      _profile = null;
      _lists = null;
    });
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
      appBar: AppBar(title: const Text('Profil')),
      body: RefreshIndicator(
        onRefresh: _refreshProfile,
        child: ListView(
          physics: const AlwaysScrollableScrollPhysics(),
          children: [
            _account(),
            if (_profile != null) ...[
              _statusGrid(),
              const Divider(height: 1),
            ],
            _fontSetting(),
            const Divider(height: 1),
            ListTile(
              leading: const Icon(Icons.image_outlined),
              title: const Text('Cache gambar'),
              subtitle: Text(
                size == null ? 'Menghitung...' : formatCacheSize(size),
              ),
            ),
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 8, 16, 24),
              child: FilledButton(
                onPressed: _clearing || size == null ? null : _clear,
                child: Text(_clearing ? 'Menghapus...' : 'Hapus cache'),
              ),
            ),
          ],
        ),
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

    return Column(
      children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(16, 16, 16, 8),
          child: ClipRRect(
            borderRadius: BorderRadius.circular(3),
            child: SizedBox(
              height: 132,
              child: Stack(
                fit: StackFit.expand,
                children: [
                  const DecoratedBox(
                    decoration: BoxDecoration(
                      gradient: LinearGradient(
                        begin: Alignment.topLeft,
                        end: Alignment.bottomRight,
                        colors: [_zinc800, _zinc900],
                      ),
                    ),
                  ),
                  if ((profile.bannerUrl ?? '').isNotEmpty)
                    CachedNetworkImage(
                      imageUrl: profile.bannerUrl!,
                      fit: BoxFit.cover,
                      errorWidget: (_, _, _) => const SizedBox.shrink(),
                    ),
                  const DecoratedBox(
                    decoration: BoxDecoration(
                      gradient: LinearGradient(
                        begin: Alignment.topCenter,
                        end: Alignment.bottomCenter,
                        colors: [Colors.transparent, _zinc900],
                      ),
                    ),
                  ),
                  Positioned(
                    left: 16,
                    right: 16,
                    bottom: 14,
                    child: Row(
                      children: [
                        CircleAvatar(
                          radius: 26,
                          backgroundColor: _zinc800,
                          backgroundImage: profile.avatarUrl.isEmpty
                              ? null
                              : CachedNetworkImageProvider(profile.avatarUrl),
                          child: profile.avatarUrl.isEmpty
                              ? const Icon(Icons.person_outline)
                              : null,
                        ),
                        const SizedBox(width: 12),
                        Expanded(
                          child: Text(
                            profile.name,
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            style: Theme.of(context).textTheme.titleLarge
                                ?.copyWith(
                                  fontWeight: FontWeight.bold,
                                  color: Colors.white,
                                ),
                          ),
                        ),
                      ],
                    ),
                  ),
                ],
              ),
            ),
          ),
        ),
        Padding(
          padding: const EdgeInsets.fromLTRB(16, 0, 8, 4),
          child: Row(
            children: [
              Expanded(
                child: Text(
                  'ID ${profile.id} · ${profile.animeCount} anime · '
                  '${profile.episodesWatched} episode',
                  style: Theme.of(context).textTheme.bodySmall,
                ),
              ),
              TextButton(onPressed: _logout, child: const Text('Keluar')),
            ],
          ),
        ),
      ],
    );
  }

  Widget _statusGrid() {
    final lists = _lists;
    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 8, 16, 8),
      child: GridView.builder(
        shrinkWrap: true,
        physics: const NeverScrollableScrollPhysics(),
        gridDelegate: const SliverGridDelegateWithFixedCrossAxisCount(
          crossAxisCount: 3,
          crossAxisSpacing: 8,
          mainAxisSpacing: 8,
          // Fixed height so the text (and the font-size setting) cannot
          // overflow the tile.
          mainAxisExtent: 104,
        ),
        itemCount: aniListStatusOrder.length,
        itemBuilder: (context, index) {
          final status = aniListStatusOrder[index];
          return _statusTile(status, lists?[status]);
        },
      ),
    );
  }

  Widget _statusTile(String status, List<WatchingAnime>? entries) {
    final accent = _statusColors[status]!;
    final label = aniListStatusLabels[status]!;
    return Material(
      color: _zinc900,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(3),
        side: const BorderSide(color: _zinc800),
      ),
      child: InkWell(
        borderRadius: BorderRadius.circular(3),
        onTap: entries == null
            ? null
            : () => Navigator.of(context).push(
                MaterialPageRoute<void>(
                  builder: (_) =>
                      AniListListScreen(title: label, entries: entries),
                ),
              ),
        child: Padding(
          padding: const EdgeInsets.all(8),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            mainAxisAlignment: MainAxisAlignment.spaceBetween,
            children: [
              Icon(_statusIcons[status], size: 18, color: accent),
              Text(
                entries == null ? '–' : '${entries.length}',
                style: Theme.of(context).textTheme.titleLarge?.copyWith(
                  fontWeight: FontWeight.bold,
                ),
              ),
              Text(
                label,
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: Theme.of(context).textTheme.labelSmall?.copyWith(
                  color: Theme.of(context).colorScheme.onSurfaceVariant,
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }

  Widget _fontSetting() {
    return ValueListenableBuilder<double>(
      valueListenable: subtitlePx,
      builder: (context, value, _) => ListTile(
        leading: const Icon(Icons.subtitles_outlined),
        title: const Text('Ukuran subtitle'),
        trailing: Text('${value.round()} px'),
        subtitle: Slider(
          min: minSubtitlePx,
          max: maxSubtitlePx,
          divisions: (maxSubtitlePx - minSubtitlePx).round(),
          value: value.clamp(minSubtitlePx, maxSubtitlePx),
          label: '${value.round()} px',
          onChanged: setSubtitlePx,
        ),
      ),
    );
  }
}
