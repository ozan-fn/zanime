import 'dart:async';

import 'package:cached_network_image/cached_network_image.dart';
import 'package:flutter/foundation.dart';
import 'package:material_ui/material_ui.dart';

import '../../models/models.dart';
import '../../services/anilist_auth.dart';
import '../../services/api.dart';
import '../../services/list_service.dart';
import '../../services/mini_player.dart';
import '../../services/playback_session.dart';
import '../../services/sokuja.dart';
import '../../widgets/mini_player.dart';
import '../../widgets/upcoming_countdown.dart';
import '../player/video_player_widget.dart';

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
    this.remover,
    this.session,
    this.initialData,
    this.recommendationLoader,
    this.relatedLoader,
  });

  final String animeId;
  final int episode;

  /// Player taken back from the floating player; keeps playback running.
  final PlaybackSession? session;

  /// Already-loaded data from PiP, so reopening shows no loading state.
  final WatchData? initialData;

  /// Injected in tests; defaults to [fetchWatchData].
  final Future<WatchData> Function(String animeId, int episode)? loader;

  /// Injected in tests; defaults to [saveAniListEntry].
  final Future<bool> Function(int mediaId, String status)? saver;

  /// Injected in tests; defaults to [fetchAniListStatus].
  final Future<String?> Function(int mediaId)? statusLoader;

  /// Injected in tests; defaults to [removeAniListEntry].
  final Future<bool> Function(int mediaId)? remover;

  /// Injected in tests; defaults to [fetchRecommendations].
  final Future<List<AnimeRecommendation>> Function(int aniListId)?
  recommendationLoader;

  /// Injected in tests; defaults to [fetchRelated].
  final Future<List<RelatedAnime>> Function(int aniListId)? relatedLoader;

  @override
  State<WatchScreen> createState() => _WatchScreenState();
}

class _WatchScreenState extends State<WatchScreen> {
  late int _episode = widget.episode;
  late Future<WatchData> _future = widget.initialData != null
      ? SynchronousFuture<WatchData>(widget.initialData!)
      : _load();
  String? _provider;
  String? _listStatus;
  ListService _service = ListService.anilist;
  bool _expanded = false;
  Future<List<AnimeRecommendation>>? _recommendations;
  Future<List<RelatedAnime>>? _related;

  /// Set while resolving a tapped card, so a double tap cannot open two pages.
  bool _openingAnime = false;

  /// Latest loaded data/server, captured for the floating player handoff.
  WatchData? _data;
  WatchServer? _server;

  /// The running player, owned here unless handed off to PiP.
  PlaybackSession? _session;
  bool _handedOff = false;

  @override
  void initState() {
    super.initState();
    _session = widget.session;
    _data = widget.initialData;
    final initial = widget.initialData;
    if (initial != null) _syncStatus(initial);
    unawaited(_loadService());
    // Opening the watch page always takes over from the floating player.
    hideMiniPlayer();
  }

  /// The list button must stay available for MAL even when Miruro exposes no
  /// AniList id, because MAL entries are resolved from the title.
  Future<void> _loadService() async {
    final service = await loadActiveListService();
    if (mounted && service != _service) setState(() => _service = service);
  }

  String get _serviceLabel =>
      _service == ListService.myanimelist ? 'MyAnimeList' : 'AniList';

  @override
  void dispose() {
    if (!_handedOff && _session != null) unawaited(_session!.dispose());
    super.dispose();
  }

  Future<WatchData> _load() async {
    WatchData data;
    try {
      data = await _fetchEpisode(_episode);
      // Requested episodes that have not aired yet are missing from the
      // schedule; show episode 1 rather than an empty watch page.
      if (_episode != 1 && !data.episodes.any((e) => e.number == _episode)) {
        _episode = 1;
        data = await _fetchEpisode(1);
      }
    } catch (_) {
      // The provider may reject the requested episode outright.
      if (_episode == 1) rethrow;
      _episode = 1;
      data = await _fetchEpisode(1);
    }
    _data = data;
    _session ??= PlaybackSession.create();
    _syncStatus(data);
    return data;
  }

  Future<WatchData> _fetchEpisode(int episode) => widget.loader != null
      ? widget.loader!(widget.animeId, episode)
      : fetchWatchData(widget.animeId, episode: episode);

  /// Reloads the current episode (pull-to-refresh / app-bar action). The
  /// future swap keeps the old data on screen, so the player is not torn down
  /// while the new payload is in flight.
  Future<void> _refresh() async {
    final future = _load();
    setState(() {
      _future = future;
      _recommendations = null;
      _related = null;
    });
    try {
      await future;
    } catch (_) {
      if (!mounted) return;
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('Gagal memperbarui anime')));
    }
  }

  /// Drops the current player after the frame so its widget unmounts first.
  void _replaceSession(PlaybackSession? next) {
    final old = _session;
    _session = next;
    if (old != null) {
      WidgetsBinding.instance.addPostFrameCallback((_) => old.dispose());
    }
  }

  Future<void> _syncStatus(WatchData data) async {
    final loader = widget.statusLoader;
    final status = loader != null
        ? (data.aniListId == null ? null : await loader(data.aniListId!))
        : await fetchActiveStatus(
            title: data.anime.title,
            aniListId: data.aniListId,
          );
    if (!mounted || status == _listStatus) return;
    setState(() => _listStatus = status);
  }

  void _selectEpisode(int number) {
    if (number == _episode) return;
    setState(() {
      _episode = number;
      _provider = null;
      _expanded = false;
      _replaceSession(null);
      _future = _load();
    });
  }

  void _selectProvider(String? value) {
    setState(() {
      _provider = value;
      _lastProvider = value;
      _replaceSession(PlaybackSession.create());
    });
  }

  /// Leaves the page but keeps playback going in the floating player.
  void _minimize() {
    final data = _data;
    final server = _server;
    if (data == null || server == null) {
      Navigator.of(context).pop();
      return;
    }
    _handedOff = true;
    final playback = MiniPlayback(
      animeId: widget.animeId,
      episode: _episode,
      streams: server.streams,
      headers: server.headers,
      subtitles: server.provider == sokujaProvider ? const [] : data.subtitles,
      subtitleHeaders: server.headers,
      session: _session,
      data: data,
    );
    // Show PiP once this page is gone, so only one Video uses the controller.
    final animation = ModalRoute.of(context)?.animation;
    if (animation == null) {
      showMiniPlayer(playback);
    } else {
      void onStatus(AnimationStatus status) {
        if (status != AnimationStatus.dismissed) return;
        animation.removeStatusListener(onStatus);
        showMiniPlayer(playback);
      }

      animation.addStatusListener(onStatus);
    }
    Navigator.of(context).pop();
  }

  Future<void> _pickStatus(WatchData data) async {
    final status = await showDialog<String>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text('$_serviceLabel status'),
        contentPadding: const EdgeInsets.fromLTRB(16, 8, 16, 16),
        content: SizedBox(
          width: 280,
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                'Tap status yang sedang aktif untuk menghapus dari list.',
                style: Theme.of(context).textTheme.bodySmall,
              ),
              const SizedBox(height: 12),
              GridView.count(
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
            ],
          ),
        ),
      ),
    );
    if (status == null || !mounted) return;
    // Tapping the active status again removes the entry from the list.
    final unmark = status == _listStatus;
    final remover = widget.remover;
    final saver = widget.saver;
    final saved = unmark
        ? remover != null
              ? await remover(data.aniListId ?? 0)
              : await removeActiveEntry(
                  title: data.anime.title,
                  aniListId: data.aniListId,
                )
        : saver != null
        ? await saver(data.aniListId ?? 0, status)
        : await saveActiveEntry(
            title: data.anime.title,
            status: status,
            aniListId: data.aniListId,
          );
    if (!mounted) return;
    if (saved) setState(() => _listStatus = unmark ? null : status);
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        content: Text(
          !saved
              ? 'Anime tidak tersedia di $_serviceLabel'
              : unmark
              ? 'Dihapus dari list'
              : 'Status: ${aniListStatusLabels[status]}',
        ),
      ),
    );
  }

  Widget _statusOption(BuildContext context, String status, String label) {
    final theme = Theme.of(context);
    final selected = status == _listStatus;
    final icon = Icon(_aniListStatusIcons[status], size: 16);
    final text = Text(label, maxLines: 1, overflow: TextOverflow.ellipsis);
    // Solid buttons: the active status uses the primary fill, the others a
    // solid zinc fill so none of them look outlined.
    final style = FilledButton.styleFrom(
      backgroundColor: selected
          ? theme.colorScheme.primary
          : const Color(0xFF3F3F46),
      foregroundColor: selected
          ? theme.colorScheme.onPrimary
          : const Color(0xFFE4E4E7),
      side: BorderSide.none,
      padding: const EdgeInsets.symmetric(horizontal: 8),
      minimumSize: Size.zero,
      tapTargetSize: MaterialTapTargetSize.shrinkWrap,
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(3)),
    );
    return FilledButton.icon(
      onPressed: () => Navigator.pop(context, status),
      icon: icon,
      label: text,
      style: style,
    );
  }

  Widget _listButton(WatchData data) {
    final status = _listStatus;
    return IconButton(
      onPressed: () => _pickStatus(data),
      tooltip: status == null
          ? 'Add to $_serviceLabel'
          : '$_serviceLabel: ${aniListStatusLabels[status]}',
      icon: Icon(
        status == null ? Icons.add : _aniListStatusIcons[status]!,
        size: 18,
      ),
      style: IconButton.styleFrom(
        // Solid like the other action buttons in the app.
        backgroundColor: const Color(0xFFF4F4F5),
        foregroundColor: const Color(0xFF09090B),
        fixedSize: const Size(32, 32),
        padding: EdgeInsets.zero,
        minimumSize: Size.zero,
        tapTargetSize: MaterialTapTargetSize.shrinkWrap,
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(3)),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    return PopScope(
      // Back keeps the stream alive in the floating player instead of
      // tearing the player down.
      canPop: false,
      onPopInvokedWithResult: (didPop, _) {
        if (!didPop) _minimize();
      },
      child: Scaffold(
        appBar: AppBar(
          title: FutureBuilder<WatchData>(
            future: _future,
            builder: (context, snapshot) =>
                Text(snapshot.data?.anime.title ?? ''),
          ),
          actions: [
            IconButton(
              onPressed: _refresh,
              tooltip: 'Muat ulang',
              icon: const Icon(Icons.refresh),
            ),
          ],
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
            return RefreshIndicator(
              onRefresh: _refresh,
              child: ListView(
                physics: const AlwaysScrollableScrollPhysics(),
                children: [
                  if (upcoming != null)
                    UpcomingCountdown(
                      episode: upcoming.number,
                      airDate: DateTime.parse(upcoming.airDate!),
                    ),
                  _player(server, data),
                  _episodeBar(data, server),
                  _episodeGrid(data),
                  _relatedSection(data),
                  _recommendationsSection(data),
                  _about(data),
                ],
              ),
            );
          },
        ),
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
    _server = server;
    if (server == null) {
      return const AspectRatio(
        aspectRatio: 16 / 9,
        child: Center(child: Text('Server tidak tersedia')),
      );
    }
    return VideoPlayerWidget(
      key: ValueKey(server.url),
      session: _session,
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
                      onChanged: _selectProvider,
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
                    if (anime.japaneseTitle.isNotEmpty &&
                        anime.japaneseTitle != anime.title)
                      Text(
                        anime.japaneseTitle,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: theme.textTheme.bodySmall?.copyWith(
                          color: theme.colorScheme.onSurfaceVariant,
                        ),
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
              if (data.aniListId != null ||
                  _service == ListService.myanimelist) ...[
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

  /// Related anime (prequel, sequel, ...) above the recommendations.
  Widget _relatedSection(WatchData data) {
    final aniListId = data.aniListId;
    if (aniListId == null) return const SizedBox.shrink();
    final loader = widget.relatedLoader ?? fetchRelated;
    final future = _related ??= loader(aniListId);
    return _horizontalSection<RelatedAnime>(
      title: 'Terkait',
      icon: Icons.account_tree_outlined,
      future: future,
      emptyText: 'Belum ada prequel, sequel, atau cerita sampingan di AniList.',
      card: (item) => _animeCard(
        title: item.title,
        imageUrl: item.imageUrl,
        meta: [
          item.relationLabel,
          if (item.score != null) '★ ${formatAnimeScore(item.score)}',
          ?item.format,
          if (item.year != null) '${item.year}',
        ].join(' · '),
        onTap: () => _openAnime(item.title, item.aniListId),
      ),
    );
  }

  /// Horizontal AniList recommendations; hidden while loading, on error, or
  /// when the anime has no AniList id.
  Widget _recommendationsSection(WatchData data) {
    final aniListId = data.aniListId;
    if (aniListId == null) return const SizedBox.shrink();
    final loader = widget.recommendationLoader ?? fetchRecommendations;
    final future = _recommendations ??= loader(aniListId);
    return _horizontalSection<AnimeRecommendation>(
      title: 'Rekomendasi',
      icon: Icons.recommend_outlined,
      future: future,
      card: (item) => _animeCard(
        title: item.title,
        imageUrl: item.imageUrl,
        meta: [
          if (item.score != null) '★ ${formatAnimeScore(item.score)}',
          ?item.format,
          if (item.year != null) '${item.year}',
          if (item.episodes != null) '${item.episodes} eps',
        ].join(' · '),
        onTap: () => _openAnime(item.title, item.aniListId),
      ),
    );
  }

  Widget _horizontalSection<T>({
    required String title,
    required IconData icon,
    required Future<List<T>> future,
    required Widget Function(T item) card,
    String? emptyText,
  }) {
    return FutureBuilder<List<T>>(
      future: future,
      builder: (context, snapshot) {
        final items = snapshot.data;
        if (items == null || (items.isEmpty && emptyText == null)) {
          return const SizedBox.shrink();
        }
        final theme = Theme.of(context);
        return Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 0, 16, 8),
              child: Row(
                children: [
                  Icon(icon, size: 18),
                  const SizedBox(width: 8),
                  Text(title, style: theme.textTheme.titleMedium),
                ],
              ),
            ),
            if (items.isEmpty)
              Padding(
                padding: const EdgeInsets.fromLTRB(16, 0, 16, 16),
                child: Text(
                  emptyText!,
                  style: theme.textTheme.bodySmall?.copyWith(
                    color: theme.colorScheme.onSurfaceVariant,
                  ),
                ),
              )
            else ...[
              SizedBox(
                height: 224,
                child: ListView.separated(
                  scrollDirection: Axis.horizontal,
                  padding: const EdgeInsets.symmetric(horizontal: 16),
                  itemCount: items.length,
                  separatorBuilder: (_, _) => const SizedBox(width: 12),
                  itemBuilder: (context, index) => card(items[index]),
                ),
              ),
              const SizedBox(height: 16),
            ],
          ],
        );
      },
    );
  }

  Widget _animeCard({
    required String title,
    required String imageUrl,
    required String meta,
    required VoidCallback onTap,
  }) {
    final theme = Theme.of(context);
    return SizedBox(
      width: 108,
      child: InkWell(
        borderRadius: BorderRadius.circular(3),
        onTap: onTap,
        // Clamp the scale so a huge system font cannot overflow the fixed card.
        child: MediaQuery.withClampedTextScaling(
          maxScaleFactor: 1.2,
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              ClipRRect(
                borderRadius: BorderRadius.circular(3),
                child: SizedBox(
                  width: 108,
                  height: 152,
                  child: CachedNetworkImage(
                    imageUrl: imageUrl,
                    fit: BoxFit.cover,
                    errorWidget: (_, _, _) =>
                        const ColoredBox(color: Colors.black),
                  ),
                ),
              ),
              const SizedBox(height: 6),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      title,
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                      style: theme.textTheme.titleSmall,
                    ),
                    if (meta.isNotEmpty)
                      Text(
                        meta,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: theme.textTheme.labelSmall?.copyWith(
                          color: theme.colorScheme.onSurfaceVariant,
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
  }

  Future<void> _openAnime(String title, int aniListId) async {
    if (_openingAnime) return;
    _openingAnime = true;
    String? animeId;
    try {
      animeId = await resolveMiruroAnimeId(title, aniListId);
    } catch (_) {
      animeId = null;
    }
    if (!mounted) return;
    final id = animeId;
    if (id == null) {
      _openingAnime = false;
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('Anime tidak tersedia di Miruro')),
      );
      return;
    }
    // Pause before the transition: the old page is still mounted while the
    // new route animates, so both players would otherwise play at once.
    final session = _session;
    if (session != null) unawaited(session.player.pause());
    // Replace instead of push. Stacking watch pages keeps one libmpv player
    // and one video controller alive per page: overlapping audio and a steady
    // memory leak. Replacing disposes the current page and its session.
    Navigator.of(context).pushReplacement(
      MaterialPageRoute<void>(builder: (_) => WatchScreen(animeId: id)),
    );
  }
}
