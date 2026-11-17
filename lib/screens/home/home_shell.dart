import 'package:material_ui/material_ui.dart';

import '../../models/models.dart';
import '../profile/settings_screen.dart';
import '../search/search_screen.dart';
import '../watching/watching_screen.dart';
import 'home_tab.dart';

/// Bottom navigation: Home, Search, Watching, Profile. Tabs are mounted on
/// first visit and kept afterwards so their state survives switching.
class HomeShell extends StatefulWidget {
  const HomeShell({super.key, this.search});

  /// Injected in tests; defaults to [searchAnime] inside [SearchScreen].
  final Future<List<SearchResult>> Function(String query)? search;

  @override
  State<HomeShell> createState() => _HomeShellState();
}

class _HomeShellState extends State<HomeShell> {
  final _visited = <int>{0};
  int _index = 0;
  int _watchingRefresh = 0;

  List<Widget> get _pages => [
    const HomeTab(),
    SearchScreen(search: widget.search),
    WatchingScreen(
      refreshSignal: _watchingRefresh,
      onLogin: () => setState(() {
        _index = 3;
        _visited.add(3);
      }),
    ),
    const SettingsScreen(),
  ];

  @override
  Widget build(BuildContext context) {
    final pages = _pages;
    return Scaffold(
      body: IndexedStack(
        index: _index,
        children: [
          for (var i = 0; i < pages.length; i++)
            _visited.contains(i) ? pages[i] : const SizedBox.shrink(),
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
            if (index == 2) _watchingRefresh++;
          }),
          destinations: const [
            NavigationDestination(
              icon: Icon(Icons.home_outlined),
              selectedIcon: Icon(Icons.home),
              label: 'Home',
            ),
            NavigationDestination(icon: Icon(Icons.search), label: 'Search'),
            NavigationDestination(
              icon: Icon(Icons.live_tv_outlined),
              selectedIcon: Icon(Icons.live_tv),
              label: 'Watching',
            ),
            NavigationDestination(
              icon: Icon(Icons.person_outline),
              selectedIcon: Icon(Icons.person),
              label: 'Profile',
            ),
          ],
        ),
      ),
    );
  }
}
