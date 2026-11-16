import 'package:material_ui/material_ui.dart';
import 'package:media_kit/media_kit.dart';

import 'components/anime_banner.dart';
import 'components/latest_episodes.dart';
import 'components/search_screen.dart';
import 'models/models.dart';
import 'services/api.dart';

const _zinc950 = Color(0xFF09090B);
const _zinc900 = Color(0xFF18181B);
const _zinc800 = Color(0xFF27272A);
const _zinc700 = Color(0xFF3F3F46);
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
      title: 'Flutter Demo',
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
      ),
      home: const MyHomePage(title: 'Flutter Demo Home Page'),
    );
  }
}

class MyHomePage extends StatefulWidget {
  const MyHomePage({super.key, required this.title});

  // This widget is the home page of your application. It is stateful, meaning
  // that it has a State object (defined below) that contains fields that affect
  // how it looks.

  // This class is the configuration for the state. It holds the values (in this
  // case the title) provided by the parent (in this case the App widget) and
  // used by the build method of the State. Fields in a Widget subclass are
  // always marked "final".

  final String title;

  @override
  State<MyHomePage> createState() => _MyHomePageState();
}

class _MyHomePageState extends State<MyHomePage> {
  final _scrollController = ScrollController();
  late Future<List<AnimeBannerData>> _banners = fetchAnimeBanners();
  late Future<List<LatestEpisode>> _episodes = fetchLatestEpisodes();

  @override
  void dispose() {
    _scrollController.dispose();
    super.dispose();
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
    return Scaffold(
      appBar: AppBar(
        title: Text(widget.title),
        actions: [
          IconButton(
            icon: const Icon(Icons.search),
            tooltip: 'Cari',
            onPressed: () => Navigator.of(context).push(
              MaterialPageRoute<void>(builder: (_) => const SearchScreen()),
            ),
          ),
        ],
      ),
      body: RefreshIndicator(
        onRefresh: _refresh,
        child: ListView(
          controller: _scrollController,
          physics: const AlwaysScrollableScrollPhysics(),
          children: [
            AnimeBanner(future: _banners),
            LatestEpisodes(
              future: _episodes,
              scrollController: _scrollController,
            ),
          ],
        ),
      ),
    );
  }
}
