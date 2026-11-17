import 'package:cached_network_image/cached_network_image.dart';
import 'package:material_ui/material_ui.dart';

import '../models/models.dart';
import '../services/anilist_auth.dart';
import '../services/api.dart';
import '../services/sokuja.dart';
import 'upcoming_countdown.dart';
import 'video_player_widget.dart';

/// Provider picked by the user most recently, shared by all watch screens
/// for the rest of the session. Falls back to the first server.
String? _lastProvider;

/// AniList status icons shown in the add/update button.
const _aniListStatusIcons = {
  'CURRENT': Icons.play_arrow,
  'PLANNING': Icons.bookmark_outline,
  'COMPLETED': Icons.check,
  'REPEATING': Icons.replay,
  'PAUSED': Icons.pause,
  'DROPPED': Icons.close,
};

class WatchScreen extends StatefulWidget {
  const WatchScreen({
    super.key,
    required this.animeId,
    this.episode = 1,
    this.loader,
    this.saver,
    this.statusLoader,
  });

  final String animeId;
  final int episode;

  /// Injected in tests; defaults to [fetchWatchData].
  final Future<WatchData> Function(String animeId, int episode)? loader;

  /// Injected in tests; defaults to [saveAniListEntry].
  final Future<bool> Function(int mediaId, String status)? saver;

  /// Injected in tests; defaults to [fetchAniListStatus].
  final Future<String?> Function(int mediaId)? statusLoader;

  @override
  State<WatchScreen> createState() => _WatchScreenState();
}

class _WatchScreenState extends State<WatchScreen> {
  late int _episode = widget.episode;
  late Future<WatchData> _future = _load();
  String? _provider;
  String? _listStatus;
  bool _expanded = false;

  Future<WatchData> _load() async {
    final data = await (widget.loader != null
        ? widget.loader!(widget.animeId, _episode)
        : fetchWatchData(widget.animeId, episode: _episode));
    _syncStatus(data.aniListId);
    return data;
  }

  Future<void> _syncStatus(int? mediaId) async {
    if (mediaId == null) return;
    final status = await (widget.statusLoader ?? fetchAniListStatus)(mediaId);
    if (!mounted || status == _listStatus) return;
    setState(() => _listStatus = status);
  }

  void _selectEpisode(int number) {
    if (number == _episode) return;
    setState(() {
      _episode = number;
      _provider = null;
      _expanded = false;
      _future = _load();
    });
  }

  Future<void> _pickStatus(WatchData data) async {
    final status = await showDialog<String>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('AniList status'),
        contentPadding: const EdgeInsets.fromLTRB(16, 8, 16, 16),
        content: SizedBox(
          width: 280,
          child: GridView.count(
            shrinkWrap: true,
            physics: const NeverScrollableScrollPhysics(),
            crossAxisCount: 2,
            crossAxisSpacing: 8,
            mainAxisSpacing: 8,
            childAspectRatio: 2.8,
            children: [
              for (final entry in aniListStatusLabels.entries)
                _statusOption(context, entry.key, entry.value),
            ],
          ),
        ),
      ),
    );
    if (status == null || !mounted) return;
    final saved = await (widget.saver ?? saveAniListEntry)(
      data.aniListId!,
      status,
    );
    if (!mounted) return;
    if (saved) setState(() => _listStatus = status);
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        content: Text(
          saved
              ? 'Status: ${aniListStatusLabels[status]}'
              : 'Login AniList di Setting dulu',
        ),
      ),
    );
  }

  Widget _statusOption(BuildContext context, String status, String label) {
    final selected = status == _listStatus;
    final icon = Icon(_aniListStatusIcons[status], size: 16);
    final text = Text(label, maxLines: 1, overflow: TextOverflow.ellipsis);
    const style = ButtonStyle(
      padding: WidgetStatePropertyAll(EdgeInsets.symmetric(horizontal: 8)),
      minimumSize: WidgetStatePropertyAll(Size.zero),
      tapTargetSize: MaterialTapTargetSize.shrinkWrap,
    );
    void pressed() => Navigator.pop(context, status);
    return selected
        ? FilledButton.icon(
            onPressed: pressed,
            icon: icon,
            label: text,
            style: style,
          )
        : OutlinedButton.icon(
            onPressed: pressed,
            icon: icon,
            label: text,
            style: style,
          );
  }

  Widget _listButton(WatchData data) {
    final status = _listStatus;
    final theme = Theme.of(context);
    return IconButton(
      onPressed: () => _pickStatus(data),
      tooltip: status == null
          ? 'Add to AniList'
          : 'AniList: ${aniListStatusLabels[status]}',
      icon: Icon(
        status == null ? Icons.add : _aniListStatusIcons[status]!,
        size: 18,
      ),
      style: IconButton.styleFrom(
        fixedSize: const Size(40, 40),
        padding: EdgeInsets.zero,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(3),
          side: BorderSide(color: theme.colorScheme.outlineVariant),
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: FutureBuilder<WatchData>(
          future: _future,
          builder: (context, snapshot) =>
              Text(snapshot.data?.anime.title ?? ''),
        ),
      ),
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
                  (item) => item.provider == (_provider ?? _lastProvider),
                  orElse: () => data.servers.first,
                );
          final upcoming = _upcoming(data);
          return ListView(
            children: [
              if (upcoming != null)
                UpcomingCountdown(
                  episode: upcoming.number,
                  airDate: DateTime.parse(upcoming.airDate!),
                ),
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

  /// Next episode that has not aired yet, if the schedule lists one.
  WatchEpisode? _upcoming(WatchData data) {
    final now = DateTime.now();
    for (final episode in data.episodes) {
      final date = DateTime.tryParse(episode.airDate ?? '');
      if (date != null && date.isAfter(now)) return episode;
    }
    return null;
  }

  Widget _player(WatchServer? server, WatchData data) {
    if (server == null) {
      return const AspectRatio(
        aspectRatio: 16 / 9,
        child: Center(child: Text('Server tidak tersedia')),
      );
    }
    return VideoPlayerWidget(
      key: ValueKey(server.url),
      streams: server.streams,
      headers: server.headers,
      // Sokuja mirrors are self-contained mp4s without track metadata.
      subtitles: server.provider == sokujaProvider ? const [] : data.subtitles,
      subtitleHeaders: server.headers,
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
                            child: Text(
                              item.provider == sokujaProvider
                                  ? 'skj'
                                  : item.provider,
                            ),
                          ),
                      ],
                      onChanged: (value) => setState(() {
                        _provider = value;
                        _lastProvider = value;
                      }),
                    ),
                  ],
                ),
              ],
            ],
          ),
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
                  child: CachedNetworkImage(
                    imageUrl: anime.imageUrl,
                    fit: BoxFit.cover,
                    errorWidget: (_, _, _) =>
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
              if (data.aniListId != null) ...[
                const SizedBox(width: 8),
                _listButton(data),
              ],
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
