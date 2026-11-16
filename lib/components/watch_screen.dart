import 'package:material_ui/material_ui.dart';

import '../models/models.dart';
import '../services/api.dart';
import 'video_player_widget.dart';

class WatchScreen extends StatefulWidget {
  const WatchScreen({
    super.key,
    required this.animeId,
    this.episode = 1,
    this.loader,
  });

  final String animeId;
  final int episode;

  /// Injected in tests; defaults to [fetchWatchData].
  final Future<WatchData> Function(String animeId, int episode)? loader;

  @override
  State<WatchScreen> createState() => _WatchScreenState();
}

class _WatchScreenState extends State<WatchScreen> {
  late int _episode = widget.episode;
  late Future<WatchData> _future = _load();
  String? _provider;
  String? _subtitleUrl;
  bool _expanded = false;

  Future<WatchData> _load() => widget.loader != null
      ? widget.loader!(widget.animeId, _episode)
      : fetchWatchData(widget.animeId, episode: _episode);

  void _selectEpisode(int number) {
    if (number == _episode) return;
    setState(() {
      _episode = number;
      _provider = null;
      _subtitleUrl = null;
      _expanded = false;
      _future = _load();
    });
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Watch')),
      body: FutureBuilder<WatchData>(
        future: _future,
        builder: (context, snapshot) {
          if (snapshot.hasError) {
            return const Center(child: Text('Gagal memuat anime'));
          }
          final data = snapshot.data;
          if (data == null) {
            return const Center(child: CircularProgressIndicator());
          }
          final server = data.servers.isEmpty
              ? null
              : data.servers.firstWhere(
                  (item) => item.provider == _provider,
                  orElse: () => data.servers.first,
                );
          return ListView(
            children: [
              _player(server, data),
              _episodeBar(data, server),
              _episodeGrid(data),
              _about(data),
            ],
          );
        },
      ),
    );
  }

  Widget _player(WatchServer? server, WatchData data) {
    if (server == null) {
      return const AspectRatio(
        aspectRatio: 16 / 9,
        child: Center(child: Text('Server tidak tersedia')),
      );
    }
    final subtitle = _subtitleFor(data);
    return VideoPlayerWidget(
      key: ValueKey(server.url),
      url: server.url,
      headers: server.headers,
      subtitleUrl: subtitle,
      subtitleHeaders: server.headers,
    );
  }

  /// null = auto (default subtitle), '' = explicitly off.
  String? _subtitleFor(WatchData data) {
    if (_subtitleUrl != null) {
      return _subtitleUrl!.isEmpty ? null : _subtitleUrl;
    }
    for (final subtitle in data.subtitles) {
      if (subtitle.isDefault) return subtitle.url;
    }
    for (final subtitle in data.subtitles) {
      if (subtitle.label.toLowerCase().startsWith('english')) {
        return subtitle.url;
      }
    }
    return null;
  }

  Widget _subtitlePicker(WatchData data) {
    if (data.subtitles.isEmpty) return const SizedBox.shrink();
    return Padding(
      padding: const EdgeInsets.only(top: 8),
      child: Row(
        children: [
          const Icon(Icons.subtitles_outlined, size: 18),
          const SizedBox(width: 6),
          DropdownButton<String>(
            value: _subtitleFor(data) ?? '',
            isDense: true,
            underline: const SizedBox.shrink(),
            items: [
              const DropdownMenuItem(value: '', child: Text('Off')),
              for (final subtitle in data.subtitles)
                DropdownMenuItem(
                  value: subtitle.url,
                  child: Text(subtitle.label),
                ),
            ],
            onChanged: (value) => setState(() => _subtitleUrl = value ?? ''),
          ),
        ],
      ),
    );
  }

  Widget _episodeBar(WatchData data, WatchServer? server) {
    final theme = Theme.of(context);
    final episode = data.episode;
    final date = WatchData.formatDate(episode?.airDate);
    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 12, 16, 0),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      [
                        'Episode ${data.currentEpisode}',
                        if (date.isNotEmpty) date,
                      ].join(' · '),
                      style: theme.textTheme.labelMedium,
                    ),
                    if (episode != null && episode.title.isNotEmpty)
                      Text(
                        episode.title,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: theme.textTheme.titleMedium,
                      ),
                  ],
                ),
              ),
              if (data.servers.isNotEmpty) ...[
                const SizedBox(width: 12),
                Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    const Icon(Icons.dns_outlined, size: 18),
                    const SizedBox(width: 6),
                    DropdownButton<String>(
                      value: server?.provider,
                      isDense: true,
                      underline: const SizedBox.shrink(),
                      items: [
                        for (final item in data.servers)
                          DropdownMenuItem(
                            value: item.provider,
                            child: Text(item.provider),
                          ),
                      ],
                      onChanged: (value) => setState(() {
                        _provider = value;
                        _subtitleUrl = null;
                      }),
                    ),
                  ],
                ),
              ],
            ],
          ),
          _subtitlePicker(data),
        ],
      ),
    );
  }

  Widget _episodeGrid(WatchData data) {
    final rows = ((data.episodes.length + 5) ~/ 6).clamp(1, 3);
    // Capped at three rows: long series scroll inside the box instead of
    // pushing the info section thousands of rows down.
    final height = rows * 38.0 + (rows - 1) * 6.0;
    final theme = Theme.of(context);
    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 16, 16, 0),
      child: SizedBox(
        height: height,
        child: GridView.builder(
          primary: false,
          padding: EdgeInsets.zero,
          gridDelegate: const SliverGridDelegateWithFixedCrossAxisCount(
            crossAxisCount: 6,
            mainAxisExtent: 38,
            crossAxisSpacing: 6,
            mainAxisSpacing: 6,
          ),
          itemCount: data.episodes.length,
          itemBuilder: (context, index) {
            final episode = data.episodes[index];
            final current = episode.number == data.currentEpisode;
            return OutlinedButton(
              onPressed: () => _selectEpisode(episode.number),
              style: OutlinedButton.styleFrom(
                padding: EdgeInsets.zero,
                minimumSize: Size.zero,
                shape: RoundedRectangleBorder(
                  borderRadius: BorderRadius.circular(3),
                ),
                backgroundColor: current ? theme.colorScheme.primary : null,
                foregroundColor: current ? theme.colorScheme.onPrimary : null,
                side: BorderSide(color: theme.colorScheme.outlineVariant),
              ),
              child: Text('${episode.number}'),
            );
          },
        ),
      ),
    );
  }

  Widget _about(WatchData data) {
    final theme = Theme.of(context);
    final anime = data.anime;
    final episode = data.episode;
    final date = WatchData.formatDate(episode?.airDate);
    final longDescription = anime.description.length > 240;
    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 24, 16, 32),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              ClipRRect(
                borderRadius: BorderRadius.circular(3),
                child: SizedBox(
                  width: 56,
                  height: 80,
                  child: Image.network(
                    anime.imageUrl,
                    fit: BoxFit.cover,
                    errorBuilder: (_, _, _) =>
                        const ColoredBox(color: Colors.black),
                  ),
                ),
              ),
              const SizedBox(width: 12),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      anime.title,
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                      style: theme.textTheme.titleLarge,
                    ),
                    const SizedBox(height: 4),
                    Text(
                      [
                        anime.seasonLabel,
                        anime.meta,
                      ].where((part) => part.isNotEmpty).join(' · '),
                      style: theme.textTheme.bodySmall,
                    ),
                    if (anime.statusLine.isNotEmpty)
                      Text(anime.statusLine, style: theme.textTheme.bodySmall),
                  ],
                ),
              ),
            ],
          ),
          const SizedBox(height: 16),
          Wrap(
            spacing: 6,
            runSpacing: 6,
            children: [
              for (final genre in anime.genres)
                DecoratedBox(
                  decoration: BoxDecoration(
                    border: Border.all(color: theme.colorScheme.outlineVariant),
                    borderRadius: BorderRadius.circular(3),
                  ),
                  child: Padding(
                    padding: const EdgeInsets.symmetric(
                      horizontal: 8,
                      vertical: 2,
                    ),
                    child: Text(genre, style: theme.textTheme.labelSmall),
                  ),
                ),
            ],
          ),
          const SizedBox(height: 16),
          Text('About this anime', style: theme.textTheme.titleMedium),
          const SizedBox(height: 6),
          Text(
            anime.description,
            maxLines: _expanded ? null : 4,
            overflow: _expanded ? null : TextOverflow.ellipsis,
            style: theme.textTheme.bodyMedium,
          ),
          if (longDescription)
            TextButton(
              onPressed: () => setState(() => _expanded = !_expanded),
              child: Text(_expanded ? 'Show less' : 'Show more'),
            ),
          if (episode != null) ...[
            const Divider(height: 32),
            Text(
              [
                'Episode ${episode.number}',
                if (date.isNotEmpty) date,
              ].join(' · '),
              style: theme.textTheme.labelMedium,
            ),
            if (episode.title.isNotEmpty)
              Text(episode.title, style: theme.textTheme.titleSmall),
            if (episode.description != null)
              Padding(
                padding: const EdgeInsets.only(top: 6),
                child: Text(
                  episode.description!,
                  style: theme.textTheme.bodyMedium,
                ),
              ),
          ],
        ],
      ),
    );
  }
}
