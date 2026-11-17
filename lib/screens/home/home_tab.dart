import 'package:material_ui/material_ui.dart';

import '../../models/models.dart';
import '../../services/api.dart';
import 'widgets/anime_banner.dart';
import 'widgets/latest_episodes.dart';

const _zinc900 = Color(0xFF18181B);

/// Marks a future's error as observed without changing what it yields.
void _ignoreErrors(Future<Object?> future) {
  future.then<void>((_) {}, onError: (_, _) {});
}

/// Home tab: trending banner plus the latest episode grid.
class HomeTab extends StatefulWidget {
  const HomeTab({super.key});

  @override
  State<HomeTab> createState() => _HomeTabState();
}

class _HomeTabState extends State<HomeTab> {
  // Started after the first frame: building the Cronet engine is heavy enough
  // to stall the first paint (visible as skipped frames/Davey in logcat).
  Future<List<AnimeBannerData>>? _banners;
  Future<List<LatestEpisode>>? _episodes;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) _load();
    });
  }

  void _load() {
    final banners = fetchAnimeBanners();
    final episodes = fetchLatestEpisodes();
    // The widgets subscribe a frame later; keep the instant failures
    // (offline, test HTTP stub) from surfacing as unhandled async errors.
    _ignoreErrors(banners);
    _ignoreErrors(episodes);
    setState(() {
      _banners = banners;
      _episodes = episodes;
    });
  }

  Future<void> _refresh() async {
    final banners = fetchAnimeBanners();
    final episodes = fetchLatestEpisodes();
    setState(() {
      _banners = banners;
      _episodes = episodes;
    });
    await Future.wait([
      banners.catchError((_) => <AnimeBannerData>[]),
      episodes.catchError((_) => <LatestEpisode>[]),
    ]);
  }

  @override
  Widget build(BuildContext context) {
    final banners = _banners;
    final episodes = _episodes;
    return Scaffold(
      appBar: AppBar(title: const Text('Zanime')),
      body: RefreshIndicator(
        onRefresh: _refresh,
        child: ListView(
          physics: const AlwaysScrollableScrollPhysics(),
          children: [
            if (banners == null)
              const AspectRatio(
                aspectRatio: 16 / 9,
                child: ColoredBox(color: _zinc900),
              )
            else
              AnimeBanner(future: banners),
            if (episodes != null) LatestEpisodes(future: episodes),
          ],
        ),
      ),
    );
  }
}
