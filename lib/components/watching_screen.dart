import 'package:cached_network_image/cached_network_image.dart';
import 'package:material_ui/material_ui.dart';

import '../services/anilist_auth.dart';
import 'watch_screen.dart';

/// Anime on the user's AniList CURRENT/REPEATING list.
class WatchingScreen extends StatefulWidget {
  const WatchingScreen({
    super.key,
    this.loader = fetchAniListWatching,
    this.onLogin,
    this.refreshSignal = 0,
  });

  /// Injected in tests.
  final Future<List<WatchingAnime>?> Function() loader;
  final VoidCallback? onLogin;

  /// Changes whenever the tab is opened, so the list reloads after login.
  final int refreshSignal;

  @override
  State<WatchingScreen> createState() => _WatchingScreenState();
}

class _WatchingScreenState extends State<WatchingScreen> {
  late Future<List<WatchingAnime>?> _future = widget.loader();

  @override
  void didUpdateWidget(WatchingScreen oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.refreshSignal != widget.refreshSignal) _reload();
  }

  void _reload() {
    setState(() => _future = widget.loader());
  }

  Future<void> _refresh() async {
    final future = widget.loader();
    setState(() => _future = future);
    await future.catchError((_) => <WatchingAnime>[]);
  }

  void _open(WatchingAnime anime) {
    Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (_) => WatchScreen(
          animeId: '${anime.mediaId}',
          episode: anime.nextWatchEpisode,
        ),
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
            return _Message(
              text: 'Gagal memuat daftar watching',
              action: TextButton(
                onPressed: _reload,
                child: const Text('Coba lagi'),
              ),
            );
          }
          if (snapshot.connectionState != ConnectionState.done) {
            return const Center(child: CircularProgressIndicator());
          }
          final anime = snapshot.data;
          if (anime == null) {
            return _Message(
              text: 'Login AniList untuk melihat anime yang sedang ditonton',
              action: widget.onLogin == null
                  ? null
                  : FilledButton(
                      onPressed: widget.onLogin,
                      child: const Text('Buka Setting'),
                    ),
            );
          }
          if (anime.isEmpty) {
            return _Message(
              text: 'Belum ada anime berstatus watching',
              action: TextButton(onPressed: _reload, child: const Text('Muat ulang')),
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
