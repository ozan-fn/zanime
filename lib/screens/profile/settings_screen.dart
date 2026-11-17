import 'package:cached_network_image/cached_network_image.dart';
import 'package:flutter_svg/flutter_svg.dart';
import 'package:material_ui/material_ui.dart';

import '../../services/anilist_auth.dart';
import '../../services/app_settings.dart';
import '../../services/app_update.dart';
import '../../services/image_cache.dart';
import '../../services/list_service.dart';
import '../../services/mal_auth.dart';
import 'anilist_list_screen.dart';
import 'update_screen.dart';

const _zinc900 = Color(0xFF18181B);
const _zinc800 = Color(0xFF27272A);
const _zinc100 = Color(0xFFF4F4F5);

/// Shown when an account has no avatar or banner of its own.
const _profileFallback = 'assets/images/profile_fallback.jpg';
const _bannerFallback = 'assets/images/banner_fallback.gif';

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
    this.loadProfile = loadActiveProfile,
    this.loadLists = fetchActiveLists,
    this.signIn = signInWithAniList,
    this.signInMal = signInWithMal,
    this.signOut = signOutActive,
    this.loadService = loadActiveListService,
    this.checkUpdate = checkForUpdate,
  });

  /// Injected in tests.
  final Future<int> Function() cacheSize;
  final Future<void> Function() clearCache;
  final Future<AniListProfile?> Function() loadProfile;
  final Future<Map<String, List<WatchingAnime>>?> Function() loadLists;
  final Future<void> Function() signIn;
  final Future<void> Function() signInMal;
  final Future<void> Function() signOut;
  final Future<ListService> Function() loadService;
  final Future<AppUpdateInfo?> Function() checkUpdate;

  @override
  State<SettingsScreen> createState() => _SettingsScreenState();
}

class _SettingsScreenState extends State<SettingsScreen> {
  int? _size;
  bool _clearing = false;
  AniListProfile? _profile;
  Map<String, List<WatchingAnime>>? _lists;
  bool _loadingProfile = true;
  ListService? _signingIn;
  ListService _service = ListService.anilist;
  AppUpdateInfo? _update;

  @override
  void initState() {
    super.initState();
    _load();
    _refreshProfile();
    _checkUpdate();
  }

  Future<void> _load() async {
    final size = await widget.cacheSize();
    if (mounted) setState(() => _size = size);
  }

  Future<void> _refreshProfile() async {
    final profile = await widget.loadProfile();
    // Only signed-in profiles need the provider/list lookups.
    final service = profile == null ? _service : await widget.loadService();
    final lists = profile == null ? null : await widget.loadLists();
    if (!mounted) return;
    setState(() {
      _service = service;
      _profile = profile;
      _lists = lists;
      _loadingProfile = false;
    });
    _checkUpdate();
  }

  Future<void> _checkUpdate() async {
    final update = await widget.checkUpdate();
    if (!mounted || update == null) return;
    setState(() => _update = update);
  }

  Future<void> _openUpdatePage() async {
    await Navigator.of(context)
        .push(MaterialPageRoute<void>(builder: (_) => const UpdateScreen()));
    _checkUpdate();
  }

  Future<void> _login(ListService service) async {
    setState(() => _signingIn = service);
    try {
      await (service == ListService.myanimelist
          ? widget.signInMal()
          : widget.signIn());
      await setActiveListService(service);
      await _refreshProfile();
    } catch (_) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Login ${_serviceLabel(service)} gagal')),
        );
      }
    } finally {
      if (mounted) setState(() => _signingIn = null);
    }
  }

  String _serviceLabel(ListService service) =>
      service == ListService.myanimelist ? 'MyAnimeList' : 'AniList';

  /// Solid, muted shadcn destructive button for destructive actions.
  ButtonStyle _destructiveStyle() => FilledButton.styleFrom(
    backgroundColor: const Color(0xFF7F1D1D),
    foregroundColor: const Color(0xFFFEF2F2),
    padding: const EdgeInsets.symmetric(horizontal: 12),
    minimumSize: const Size(0, 32),
    shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(3)),
  );

  Future<bool> _confirm({
    required String title,
    required String message,
    required String action,
  }) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text(title),
        content: Text(message),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('Batal'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            style: _destructiveStyle(),
            child: Text(action),
          ),
        ],
      ),
    );
    return confirmed ?? false;
  }

  Future<void> _confirmLogout() async {
    final label = _serviceLabel(_service);
    final confirmed = await _confirm(
      title: 'Keluar dari $label?',
      message: 'Profil dan daftar watching tidak akan ditampilkan lagi.',
      action: 'Keluar',
    );
    if (confirmed && mounted) await _logout();
  }

  Future<void> _logout() async {
    final label = _serviceLabel(_service);
    await widget.signOut();
    if (!mounted) return;
    setState(() {
      _profile = null;
      _lists = null;
    });
    ScaffoldMessenger.of(context)
        .showSnackBar(SnackBar(content: Text('Logout dari $label')));
  }

  Future<void> _confirmClear() async {
    final confirmed = await _confirm(
      title: 'Hapus cache aplikasi?',
      message: 'Gambar dan file cache akan diunduh ulang saat dibutuhkan.',
      action: 'Hapus',
    );
    if (confirmed && mounted) await _clear();
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
            if (_profile != null) ...[_statusGrid(), const Divider(height: 1)],
            _fontSetting(),
            const Divider(height: 1),
            _updateTile(),
            const Divider(height: 1),
            ListTile(
              contentPadding: const EdgeInsets.fromLTRB(16, 0, 16, 0),
              leading: const Icon(Icons.folder_outlined),
              title: const Text('Cache aplikasi'),
              subtitle: Text(
                size == null ? 'Menghitung...' : formatCacheSize(size),
              ),
              trailing: IconButton(
                onPressed: _clearing || size == null ? null : _confirmClear,
                tooltip: 'Hapus cache',
                iconSize: 24,
                icon: _clearing
                    ? const SizedBox(
                        width: 18,
                        height: 18,
                        child: CircularProgressIndicator(strokeWidth: 2),
                      )
                    : const Icon(Icons.delete_outline),
                style: IconButton.styleFrom(
                  backgroundColor: const Color(0xFF7F1D1D),
                  foregroundColor: const Color(0xFFFEF2F2),
                  disabledBackgroundColor: const Color(0x337F1D1D),
                  disabledForegroundColor: const Color(0x66FEF2F2),
                  fixedSize: const Size(32, 32),
                  padding: EdgeInsets.zero,
                  minimumSize: Size.zero,
                  tapTargetSize: MaterialTapTargetSize.shrinkWrap,
                  shape: RoundedRectangleBorder(
                    borderRadius: BorderRadius.circular(3),
                  ),
                ),
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
        title: Text('Memuat profil...'),
      );
    }

    final profile = _profile;
    if (profile == null) {
      final theme = Theme.of(context);
      return Padding(
        padding: const EdgeInsets.fromLTRB(16, 16, 16, 8),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('Hubungkan akun', style: theme.textTheme.titleMedium),
            const SizedBox(height: 4),
            Text(
              'Login untuk menyimpan status tontonan.',
              style: theme.textTheme.bodySmall,
            ),
            const SizedBox(height: 12),
            Row(
              children: [
                Expanded(
                  child: _loginButton(
                    service: ListService.anilist,
                    label: 'ani',
                    asset: 'assets/logos/anilist.svg',
                    color: const Color(0xFF02B9FF),
                  ),
                ),
                const SizedBox(width: 8),
                Expanded(
                  child: _loginButton(
                    service: ListService.myanimelist,
                    label: 'mal',
                    asset: 'assets/logos/myanimelist.svg',
                    color: const Color(0xFF2E51A2),
                  ),
                ),
              ],
            ),
          ],
        ),
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
                  if ((profile.bannerUrl ?? '').isEmpty)
                    Image.asset(
                      _bannerFallback,
                      fit: BoxFit.cover,
                      gaplessPlayback: true,
                    )
                  else
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
                        ClipOval(
                          child: SizedBox(
                            width: 52,
                            height: 52,
                            child: profile.avatarUrl.isEmpty
                                ? Image.asset(
                                    _profileFallback,
                                    fit: BoxFit.cover,
                                  )
                                : CachedNetworkImage(
                                    imageUrl: profile.avatarUrl,
                                    fit: BoxFit.cover,
                                    errorWidget: (_, _, _) => Image.asset(
                                      _profileFallback,
                                      fit: BoxFit.cover,
                                    ),
                                  ),
                          ),
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
          // Same 16 px gutter as the banner, so the button lines up with it.
          padding: const EdgeInsets.fromLTRB(16, 0, 16, 4),
          child: Row(
            children: [
              Expanded(
                child: Text(
                  'ID ${profile.id} · ${profile.animeCount} anime · '
                  '${profile.episodesWatched} episode',
                  style: Theme.of(context).textTheme.bodySmall,
                ),
              ),
              FilledButton(
                onPressed: _confirmLogout,
                style: _destructiveStyle(),
                child: const Text('Keluar'),
              ),
            ],
          ),
        ),
      ],
    );
  }

  Widget _loginButton({
    required ListService service,
    required String label,
    required String asset,
    required Color color,
  }) {
    final busy = _signingIn == service;
    return FilledButton.icon(
      onPressed: _signingIn == null ? () => _login(service) : null,
      style: FilledButton.styleFrom(
        backgroundColor: color,
        foregroundColor: Colors.white,
        disabledBackgroundColor: color.withValues(alpha: 0.4),
        minimumSize: const Size(0, 44),
        padding: const EdgeInsets.symmetric(horizontal: 12),
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(3)),
      ),
      icon: busy
          ? const SizedBox(
              width: 18,
              height: 18,
              child: CircularProgressIndicator(
                strokeWidth: 2,
                color: Colors.white,
              ),
            )
          : SvgPicture.asset(asset, width: 18, height: 18),
      label: Text(label),
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
          // Compact height for the icon+count row and the label below.
          mainAxisExtent: 72,
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
            : () async {
                await Navigator.of(context).push(
                  MaterialPageRoute<void>(
                    builder: (_) => AniListListScreen(
                      title: label,
                      status: status,
                      entries: entries,
                      resolver: resolveMiruroAnimeIdForActive,
                    ),
                  ),
                );
                // Counts may have changed after a pull-to-refresh in the list.
                if (mounted) await _refreshProfile();
              },
        child: Padding(
          padding: const EdgeInsets.all(8),
          // Fixed 72 px tiles: clamp the text scale so large system fonts
          // cannot overflow the icon+count row and the label below.
          child: MediaQuery.withClampedTextScaling(
            maxScaleFactor: 1.3,
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              mainAxisAlignment: MainAxisAlignment.spaceBetween,
              children: [
                Row(
                  children: [
                    Icon(_statusIcons[status], size: 16, color: accent),
                    const SizedBox(width: 6),
                    Text(
                      entries == null ? '–' : '${entries.length}',
                      style: Theme.of(context).textTheme.titleMedium
                          ?.copyWith(fontWeight: FontWeight.bold),
                    ),
                  ],
                ),
                Text(
                  label,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: Theme.of(context).textTheme.labelSmall?.copyWith(
                    fontSize: 10,
                    color: Theme.of(context).colorScheme.onSurfaceVariant,
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }

  Widget _updateTile() {
    final update = _update;
    final available = update?.available == true;
    return ListTile(
      key: const ValueKey('update-tile'),
      contentPadding: const EdgeInsets.fromLTRB(16, 0, 16, 0),
      leading: Icon(
        available ? Icons.system_update : Icons.system_update_outlined,
      ),
      title: const Text('Cek update'),
      subtitle: Text(
        available
            ? 'Versi ${update!.release.version} tersedia'
            : 'Bandingkan dengan release GitHub',
      ),
      trailing: IconButton(
        onPressed: _openUpdatePage,
        tooltip: 'Cek update',
        iconSize: 20,
        icon: const Icon(Icons.chevron_right),
        style: IconButton.styleFrom(
          backgroundColor: _zinc100,
          foregroundColor: const Color(0xFF09090B),
          fixedSize: const Size(32, 32),
          padding: EdgeInsets.zero,
          minimumSize: Size.zero,
          tapTargetSize: MaterialTapTargetSize.shrinkWrap,
          shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(3)),
        ),
      ),
      // Everything happens inside the app now: download and install here.
      onTap: _openUpdatePage,
    );
  }

  Widget _fontSetting() {
    return ValueListenableBuilder<double>(
      valueListenable: subtitlePx,
      builder: (context, value, _) {
        final theme = Theme.of(context);
        // Same 72 px row height as the other two-line settings tiles.
        return SizedBox(
          key: const ValueKey('subtitle-tile'),
          height: 72,
          child: MediaQuery.withClampedTextScaling(
            maxScaleFactor: 1.3,
            child: Padding(
              padding: const EdgeInsets.symmetric(horizontal: 16),
              child: Row(
                children: [
                  const Icon(Icons.subtitles_outlined),
                  const SizedBox(width: 16),
                  Expanded(
                    child: Column(
                      mainAxisAlignment: MainAxisAlignment.center,
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Row(
                          children: [
                            Expanded(
                              child: Text(
                                'Ukuran subtitle',
                                style: theme.textTheme.bodyLarge,
                              ),
                            ),
                            Text(
                              '${value.round()} px',
                              style: theme.textTheme.bodySmall,
                            ),
                          ],
                        ),
                        SizedBox(
                          height: 32,
                          child: SliderTheme(
                            data: SliderTheme.of(context).copyWith(
                              trackHeight: 2,
                              thumbShape: const RoundSliderThumbShape(
                                enabledThumbRadius: 6,
                              ),
                              overlayShape: const RoundSliderOverlayShape(
                                overlayRadius: 12,
                              ),
                              tickMarkShape: SliderTickMarkShape.noTickMark,
                            ),
                            child: Slider(
                              min: minSubtitlePx,
                              max: maxSubtitlePx,
                              divisions: (maxSubtitlePx - minSubtitlePx)
                                  .round(),
                              value: value.clamp(minSubtitlePx, maxSubtitlePx),
                              label: '${value.round()} px',
                              onChanged: setSubtitlePx,
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
        );
      },
    );
  }
}
