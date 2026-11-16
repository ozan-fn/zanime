import 'package:material_ui/material_ui.dart';

import '../models/models.dart';
import 'watch_screen.dart';

const _pageSize = 18;

class LatestEpisodes extends StatefulWidget {
  const LatestEpisodes({
    super.key,
    required this.future,
    this.scrollController,
  });

  final Future<List<LatestEpisode>> future;
  final ScrollController? scrollController;

  @override
  State<LatestEpisodes> createState() => _LatestEpisodesState();
}

class _LatestEpisodesState extends State<LatestEpisodes> {
  List<LatestEpisode> _all = const [];
  int _visible = 0;
  bool _loading = true;
  bool _loadingMore = false;
  String? _error;

  int get _pages => (_all.length / _pageSize).ceil();

  @override
  void initState() {
    super.initState();
    widget.scrollController?.addListener(_onScroll);
    _load(widget.future);
  }

  @override
  void didUpdateWidget(LatestEpisodes oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.future != widget.future) _load(widget.future);
    if (oldWidget.scrollController != widget.scrollController) {
      oldWidget.scrollController?.removeListener(_onScroll);
      widget.scrollController?.addListener(_onScroll);
    }
  }

  @override
  void dispose() {
    widget.scrollController?.removeListener(_onScroll);
    super.dispose();
  }

  Future<void> _load(Future<List<LatestEpisode>> future) async {
    setState(() {
      _all = const [];
      _visible = 0;
      _loading = true;
      _error = null;
    });
    try {
      final items = await future;
      if (!mounted) return;
      setState(() {
        _all = items;
        _visible = items.length < _pageSize ? items.length : _pageSize;
      });
    } catch (e) {
      if (mounted) setState(() => _error = 'Gagal memuat episode');
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  void _onScroll() {
    final controller = widget.scrollController;
    if (controller == null || !controller.hasClients) return;
    if (controller.position.extentAfter < 400) _loadMore();
  }

  Future<void> _loadMore() async {
    if (_loadingMore || _visible >= _all.length) return;
    setState(() => _loadingMore = true);
    await Future<void>.delayed(const Duration(milliseconds: 300));
    if (!mounted) return;
    setState(() {
      _visible = (_visible + _pageSize).clamp(0, _all.length);
      _loadingMore = false;
    });
  }

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(16, 16, 16, 8),
          child: Row(
            children: [
              Expanded(
                child: Text(
                  'Latest Episodes',
                  style: Theme.of(context).textTheme.titleLarge
                      ?.copyWith(fontWeight: FontWeight.bold),
                ),
              ),
              if (_all.isNotEmpty)
                Text(
                  '${(_visible / _pageSize).ceil().clamp(1, _pages)}/$_pages',
                  style: Theme.of(context).textTheme.labelMedium,
                ),
            ],
          ),
        ),
        if (_error != null)
          Padding(padding: const EdgeInsets.all(16), child: Text(_error!))
        else if (_loading)
          const _SkeletonGrid()
        else
          for (var i = 0; i < _visible; i += 2)
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 6),
              child: Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Expanded(child: _EpisodeCard(item: _all[i])),
                  const SizedBox(width: 12),
                  Expanded(
                    child: i + 1 < _visible
                        ? _EpisodeCard(item: _all[i + 1])
                        : const SizedBox.shrink(),
                  ),
                ],
              ),
            ),
        if (_loadingMore)
          const Padding(
            padding: EdgeInsets.all(16),
            child: Center(child: CircularProgressIndicator()),
          ),
      ],
    );
  }
}

class _EpisodeCard extends StatelessWidget {
  const _EpisodeCard({required this.item});

  final LatestEpisode item;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return InkWell(
      onTap: () => Navigator.of(context).push(
        MaterialPageRoute<void>(
          builder: (_) =>
              WatchScreen(animeId: item.animeId, episode: item.episode),
        ),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          ClipRRect(
            borderRadius: BorderRadius.circular(3),
            child: AspectRatio(
              aspectRatio: 2 / 3,
              child: Stack(
                fit: StackFit.expand,
                children: [
                  Image.network(
                    item.imageUrl,
                    fit: BoxFit.cover,
                    errorBuilder: (_, _, _) =>
                        const ColoredBox(color: Colors.black),
                  ),
                  Positioned(
                    left: 6,
                    top: 6,
                    child: Container(
                      padding: const EdgeInsets.symmetric(
                        horizontal: 6,
                        vertical: 2,
                      ),
                      decoration: BoxDecoration(
                        color: Colors.black54,
                        borderRadius: BorderRadius.circular(3),
                      ),
                      child: Text(
                        'EP ${item.episode}',
                        style: const TextStyle(
                          color: Colors.white,
                          fontSize: 10,
                          fontWeight: FontWeight.bold,
                        ),
                      ),
                    ),
                  ),
                ],
              ),
            ),
          ),
          const SizedBox(height: 6),
          Text(
            item.title,
            maxLines: 2,
            overflow: TextOverflow.ellipsis,
            style: theme.textTheme.bodySmall,
          ),
          Text(
            'EP ${item.episode} · ${_ago(item.airedAt)}',
            style: theme.textTheme.labelSmall?.copyWith(
              color: theme.colorScheme.primary,
            ),
          ),
          Text(
            [
              if (item.subCount != null) '${item.subCount} sub',
              if (item.dubCount != null) '${item.dubCount} dub',
              item.format,
              '${item.year}',
            ].join(' · '),
            style: theme.textTheme.labelSmall,
          ),
        ],
      ),
    );
  }

  String _ago(DateTime time) {
    final diff = DateTime.now().difference(time);
    if (diff.inMinutes < 60) return '${diff.inMinutes.clamp(1, 59)}m ago';
    if (diff.inHours < 24) return '${diff.inHours}h ago';
    return '${diff.inDays}d ago';
  }
}

class _SkeletonGrid extends StatelessWidget {
  const _SkeletonGrid();

  @override
  Widget build(BuildContext context) {
    final color = Theme.of(context).colorScheme.surfaceContainerHighest;
    Widget card() => Expanded(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          AspectRatio(
            aspectRatio: 2 / 3,
            child: DecoratedBox(
              decoration: BoxDecoration(
                color: color,
                borderRadius: BorderRadius.circular(3),
              ),
            ),
          ),
          const SizedBox(height: 8),
          Container(height: 12, color: color),
          const SizedBox(height: 6),
          Container(height: 10, width: 80, color: color),
        ],
      ),
    );

    return Column(
      children: [
        for (var i = 0; i < 6; i += 2)
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 6),
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [card(), const SizedBox(width: 12), card()],
            ),
          ),
      ],
    );
  }
}
