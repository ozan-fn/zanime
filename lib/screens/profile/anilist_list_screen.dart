import 'package:cached_network_image/cached_network_image.dart';
import 'package:material_ui/material_ui.dart';

import '../../services/anilist_auth.dart';
import '../../services/list_service.dart';
import '../detail/watch_screen.dart';

/// One AniList status list (watching, completed, planning, and so on).
/// Pull down — or tap the refresh action — to fetch the list again.
class AniListListScreen extends StatefulWidget {
  const AniListListScreen({
    super.key,
    required this.title,
    required this.status,
    required this.entries,
    this.resolver = resolveMiruroAnimeIdForActive,
    this.loader = fetchActiveLists,
  });

  final String title;

  /// AniList/MAL status code, e.g. `COMPLETED`.
  final String status;
  final List<WatchingAnime> entries;

  /// Maps an entry to a Miruro id; injected in tests.
  final Future<String?> Function(String title, int aniListId) resolver;

  /// Loads every status list from the active service; injected in tests.
  final Future<Map<String, List<WatchingAnime>>?> Function() loader;

  @override
  State<AniListListScreen> createState() => _AniListListScreenState();
}

class _AniListListScreenState extends State<AniListListScreen> {
  late List<WatchingAnime> _entries = widget.entries;
  bool _refreshing = false;

  Future<void> _refresh() async {
    if (_refreshing) return;
    setState(() => _refreshing = true);
    try {
      final lists = await widget.loader();
      final entries = lists?[widget.status];
      if (!mounted) return;
      if (lists == null) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(content: Text('Login untuk memperbarui daftar')),
        );
        return;
      }
      setState(() => _entries = entries ?? const []);
    } catch (_) {
      if (!mounted) return;
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('Gagal memperbarui daftar')));
    } finally {
      if (mounted) setState(() => _refreshing = false);
    }
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

  /// Pull-to-refresh works even when the list is empty.
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
    final theme = Theme.of(context);
    return Scaffold(
      appBar: AppBar(
        title: Text('${widget.title} · ${_entries.length}'),
        actions: [
          if (_refreshing)
            const Padding(
              padding: EdgeInsets.all(16),
              child: SizedBox.square(
                dimension: 20,
                child: CircularProgressIndicator(strokeWidth: 2),
              ),
            )
          else
            IconButton(
              onPressed: _refresh,
              tooltip: 'Muat ulang',
              icon: const Icon(Icons.refresh),
            ),
        ],
      ),
      body: _entries.isEmpty
          ? _refreshable(const Center(child: Text('Daftar masih kosong')))
          : RefreshIndicator(
              onRefresh: _refresh,
              child: ListView.separated(
                physics: const AlwaysScrollableScrollPhysics(),
                itemCount: _entries.length,
                separatorBuilder: (_, _) => const Divider(height: 1),
                itemBuilder: (context, index) {
                  final anime = _entries[index];
                  final meta = [
                    ?anime.format,
                    if (anime.year != null) '${anime.year}',
                    '${anime.progress}/${anime.totalEpisodes ?? '?'}',
                  ].join(' · ');
                  return ListTile(
                    leading: ClipRRect(
                      borderRadius: BorderRadius.circular(3),
                      child: SizedBox(
                        width: 44,
                        height: 62,
                        child: CachedNetworkImage(
                          imageUrl: anime.imageUrl,
                          fit: BoxFit.cover,
                          errorWidget: (_, _, _) =>
                              const ColoredBox(color: Colors.black),
                        ),
                      ),
                    ),
                    title: Text(
                      anime.title,
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                    ),
                    subtitle: Text(meta),
                    trailing: Text(
                      'Eps ${anime.nextWatchEpisode}',
                      style: theme.textTheme.labelMedium,
                    ),
                    onTap: () => _open(anime),
                  );
                },
              ),
            ),
    );
  }
}
