import 'package:cached_network_image/cached_network_image.dart';
import 'package:material_ui/material_ui.dart';

import '../../models/models.dart';
import '../../services/anilist_auth.dart';
import '../../services/list_service.dart';
import '../detail/watch_screen.dart';

/// Anime on the active service's CURRENT/REPEATING list.
class WatchingScreen extends StatefulWidget {
  const WatchingScreen({
    super.key,
    this.loader = fetchActiveWatching,
    this.resolver = resolveMiruroAnimeIdForActive,
    this.onLogin,
    this.refreshSignal = 0,
  });

  /// Injected in tests.
  final Future<List<WatchingAnime>?> Function() loader;

  /// Maps an AniList entry to a Miruro id; injected in tests.
  final Future<String?> Function(String title, int aniListId) resolver;
  final VoidCallback? onLogin;

  /// Changes whenever the tab is opened, so the list reloads after login.
  final int refreshSignal;

  @override
  State<WatchingScreen> createState() => _WatchingScreenState();
}

class _WatchingScreenState extends State<WatchingScreen> {
  late Future<List<WatchingAnime>?> _future = _load();

  /// True when the last load found no token; only then does opening the tab
  /// reload. Logged-in lists stay cached until pull-to-refresh.
  bool _loggedOut = false;

  Future<List<WatchingAnime>?> _load() {
    final future = widget.loader();
    future.then<void>((data) => _loggedOut = data == null, onError: (_) {});
    return future;
  }

  @override
  void didUpdateWidget(WatchingScreen oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.refreshSignal != widget.refreshSignal && _loggedOut) {
      _reload();
    }
  }

  void _reload() {
    setState(() => _future = _load());
  }

  Future<void> _refresh() async {
    final future = _load();
    setState(() => _future = future);
    await future.catchError((_) => <WatchingAnime>[]);
  }

  Future<void> _open(WatchingAnime anime) async {
    String? animeId;
    try {
      animeId = await widget.resolver(anime.title, anime.mediaId);
    } catch (_) {
      animeId = null;
    }
    if (!mounted) return;
    final id = animeId;
    if (id == null) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('Anime tidak tersedia di Miruro')),
      );
      return;
    }
    Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (_) =>
            WatchScreen(animeId: id, episode: anime.nextWatchEpisode),
      ),
    );
  }

  /// Pull-to-refresh works even when the state has no list to scroll.
  Widget _refreshable(Widget child) {
    return RefreshIndicator(
      onRefresh: _refresh,
      child: CustomScrollView(
        physics: const AlwaysScrollableScrollPhysics(),
        slivers: [SliverFillRemaining(hasScrollBody: false, child: child)],
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Watching')),
      body: FutureBuilder<List<WatchingAnime>?>(
        future: _future,
        builder: (context, snapshot) {
          if (snapshot.hasError) {
            return _refreshable(
              _Message(
                text: 'Gagal memuat daftar watching',
                action: TextButton(
                  onPressed: _reload,
                  child: const Text('Coba lagi'),
                ),
              ),
            );
          }
          if (snapshot.connectionState != ConnectionState.done) {
            return const Center(child: CircularProgressIndicator());
          }
          final anime = snapshot.data;
          if (anime == null) {
            return _refreshable(
              _Message(
                text:
                    'Login AniList atau MyAnimeList untuk melihat anime yang '
                    'sedang ditonton',
                action: widget.onLogin == null
                    ? null
                    : FilledButton(
                        onPressed: widget.onLogin,
                        child: const Text('Buka Setting'),
                      ),
              ),
            );
          }
          if (anime.isEmpty) {
            return _refreshable(
              _Message(
                text: 'Belum ada anime berstatus watching',
                action: TextButton(
                  onPressed: _reload,
                  child: const Text('Muat ulang'),
                ),
              ),
            );
          }
          return RefreshIndicator(
            onRefresh: _refresh,
            child: ListView.separated(
              physics: const AlwaysScrollableScrollPhysics(),
              itemCount: anime.length,
              separatorBuilder: (_, _) => const Divider(height: 1),
              itemBuilder: (context, index) => _tile(anime[index]),
            ),
          );
        },
      ),
    );
  }

  Widget _tile(WatchingAnime anime) {
    final meta = [
      if (anime.score != null) '★ ${formatAnimeScore(anime.score)}',
      anime.format,
      if (anime.year != null) '${anime.year}',
      if (anime.totalEpisodes != null)
        '${anime.progress}/${anime.totalEpisodes}',
    ].whereType<String>().where((part) => part.isNotEmpty).join(' · ');
    return ListTile(
      leading: ClipRRect(
        borderRadius: BorderRadius.circular(3),
        child: SizedBox(
          width: 44,
          height: 62,
          child: CachedNetworkImage(
            imageUrl: anime.imageUrl,
            fit: BoxFit.cover,
            errorWidget: (_, _, _) => const ColoredBox(color: Colors.black),
          ),
        ),
      ),
      title: Text(anime.title, maxLines: 2, overflow: TextOverflow.ellipsis),
      subtitle: Text(meta),
      trailing: Text(
        'Eps ${anime.nextWatchEpisode}',
        style: Theme.of(context).textTheme.labelMedium,
      ),
      onTap: () => _open(anime),
    );
  }
}

class _Message extends StatelessWidget {
  const _Message({required this.text, this.action});

  final String text;
  final Widget? action;

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Text(text, textAlign: TextAlign.center),
            if (action != null) ...[const SizedBox(height: 12), action!],
          ],
        ),
      ),
    );
  }
}
