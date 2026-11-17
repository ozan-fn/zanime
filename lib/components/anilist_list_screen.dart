import 'package:cached_network_image/cached_network_image.dart';
import 'package:material_ui/material_ui.dart';

import '../services/anilist_auth.dart';
import '../services/api.dart';
import 'watch_screen.dart';

/// One AniList status list (watching, completed, planning, and so on).
class AniListListScreen extends StatelessWidget {
  const AniListListScreen({
    super.key,
    required this.title,
    required this.entries,
    this.resolver = resolveMiruroAnimeId,
  });

  final String title;
  final List<WatchingAnime> entries;

  /// Maps an AniList entry to a Miruro id; injected in tests.
  final Future<String?> Function(String title, int aniListId) resolver;

  Future<void> _open(BuildContext context, WatchingAnime anime) async {
    String? animeId;
    try {
      animeId = await resolver(anime.title, anime.mediaId);
    } catch (_) {
      animeId = null;
    }
    if (!context.mounted) return;
    final id = animeId;
    if (id == null) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('Anime tidak tersedia di Miruro')),
      );
      return;
    }
    Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (_) => WatchScreen(
          animeId: id,
          episode: anime.nextWatchEpisode,
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Scaffold(
      appBar: AppBar(title: Text('$title · ${entries.length}')),
      body: entries.isEmpty
          ? const Center(child: Text('Daftar masih kosong'))
          : ListView.separated(
              itemCount: entries.length,
              separatorBuilder: (_, _) => const Divider(height: 1),
              itemBuilder: (context, index) {
                final anime = entries[index];
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
                  onTap: () => _open(context, anime),
                );
              },
            ),
    );
  }
}
