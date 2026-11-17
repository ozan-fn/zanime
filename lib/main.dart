import 'package:material_ui/material_ui.dart';
import 'package:media_kit/media_kit.dart';

import 'components/anime_banner.dart';
import 'components/latest_episodes.dart';
import 'components/search_screen.dart';
import 'components/settings_screen.dart';
import 'models/models.dart';
import 'services/api.dart';

const _zinc950 = Color(0xFF09090B);
const _zinc900 = Color(0xFF18181B);
const _zinc800 = Color(0xFF27272A);
const _zinc700 = Color(0xFF3F3F46);
const _zinc400 = Color(0xFFA1A1AA);
const _zinc100 = Color(0xFFF4F4F5);

void main() {
  WidgetsFlutterBinding.ensureInitialized();
  MediaKit.ensureInitialized();
  runApp(const MyApp());
}

class MyApp extends StatelessWidget {
  const MyApp({super.key});

  // This widget is the root of your application.
  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'Zanime',
      theme: ThemeData(
        brightness: Brightness.dark,
        scaffoldBackgroundColor: _zinc950,
        colorScheme: const ColorScheme.dark(
          primary: _zinc100,
          onPrimary: _zinc950,
          surface: _zinc900,
          onSurface: _zinc100,
          outline: _zinc700,
          outlineVariant: _zinc800,
        ),
        dividerTheme: const DividerThemeData(color: _zinc800),
        appBarTheme: const AppBarTheme(
          backgroundColor: _zinc950,
          foregroundColor: _zinc100,
        ),
        // Flat zinc bar: selected tab is just a brighter icon/label, no box.
        navigationBarTheme: NavigationBarThemeData(
          backgroundColor: _zinc950,
          indicatorColor: Colors.transparent,
          indicatorShape: const RoundedRectangleBorder(
            borderRadius: BorderRadius.zero,
          ),
          surfaceTintColor: Colors.transparent,
          // Kills the pressed overlay box M3 draws behind the icon.
          overlayColor: const WidgetStatePropertyAll(Colors.transparent),
          elevation: 0,
          // shadcn-like compact bar.
          height: 48,
          iconTheme: WidgetStateProperty.resolveWith(
            (states) => IconThemeData(
              size: 22,
              color: states.contains(WidgetState.selected)
                  ? _zinc100
                  : _zinc400,
            ),
          ),
          labelTextStyle: WidgetStateProperty.resolveWith(
            (states) => TextStyle(
              fontSize: 12,
              color: states.contains(WidgetState.selected)
                  ? _zinc100
                  : _zinc400,
            ),
          ),
        ),
      ),
      home: const HomeShell(),
    );
  }
}

/// Bottom navigation: Home, Search, History, Setting. Tabs are mounted on
/// first visit and kept afterwards so their state survives switching.
class HomeShell extends StatefulWidget {
  const HomeShell({super.key, this.search});

  /// Injected in tests; defaults to [searchAnime] inside [SearchScreen].
  final Future<List<SearchResult>> Function(String query)? search;

  @override
  State<HomeShell> createState() => _HomeShellState();
}

class _HomeShellState extends State<HomeShell> {
  late final List<Widget> _pages = [
    const HomeTab(),
    SearchScreen(search: widget.search),
    const _EmptyTab(title: 'History'),
    const SettingsScreen(),
  ];
  final _visited = <int>{0};
  int _index = 0;

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: IndexedStack(
        index: _index,
        children: [
          for (var i = 0; i < _pages.length; i++)
            _visited.contains(i) ? _pages[i] : const SizedBox.shrink(),
        ],
      ),
      bottomNavigationBar: Theme(
        // No ripple/highlight box on tap: only the icon colour changes.
        data: Theme.of(context).copyWith(
          splashFactory: NoSplash.splashFactory,
          splashColor: Colors.transparent,
          highlightColor: Colors.transparent,
        ),
        child: NavigationBar(
          selectedIndex: _index,
          // No labels and no transition: the tab switch is a plain swap.
          labelBehavior: NavigationDestinationLabelBehavior.alwaysHide,
          animationDuration: Duration.zero,
          onDestinationSelected: (index) => setState(() {
            _index = index;
            _visited.add(index);
          }),
          destinations: const [
            NavigationDestination(
              icon: Icon(Icons.home_outlined),
              selectedIcon: Icon(Icons.home),
              label: 'Home',
            ),
            NavigationDestination(icon: Icon(Icons.search), label: 'Search'),
            NavigationDestination(icon: Icon(Icons.history), label: 'History'),
            NavigationDestination(
              icon: Icon(Icons.settings_outlined),
              selectedIcon: Icon(Icons.settings),
              label: 'Setting',
            ),
          ],
        ),
      ),
    );
  }
}

/// Marks a future's error as observed without changing what it yields.
void _ignoreErrors(Future<Object?> future) {
  future.then<void>((_) {}, onError: (_, _) {});
}

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

/// History and Setting are placeholders for now.
class _EmptyTab extends StatelessWidget {
  const _EmptyTab({required this.title});

  final String title;

  @override
  Widget build(BuildContext context) {
    return Scaffold(appBar: AppBar(title: Text(title)));
  }
}
