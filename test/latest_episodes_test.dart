import 'package:flutter_test/flutter_test.dart';
import 'package:material_ui/material_ui.dart';
import 'package:test1/screens/home/widgets/latest_episodes.dart';
import 'package:test1/screens/detail/watch_screen.dart';
import 'package:test1/models/models.dart';

LatestEpisode _episode(int i) => LatestEpisode(
  animeId: 'id$i',
  title: 'Anime $i',
  romaji: 'Anime $i',
  imageUrl: 'https://example.com/$i.jpg',
  episode: i,
  airedAt: DateTime.now().subtract(Duration(hours: i)),
  format: 'TV',
  year: 2026,
  subCount: i,
  dubCount: i - 1,
);

void main() {
  testWidgets('Latest Episodes shows every episode without pagination', (
    tester,
  ) async {
    final items = [for (var i = 1; i <= 20; i++) _episode(i)];

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: ListView(
            children: [LatestEpisodes(future: Future.value(items))],
          ),
        ),
      ),
    );
    await tester.pump();

    expect(find.text('Latest Episodes'), findsOneWidget);
    expect(find.text('Anime 1'), findsOneWidget);
    expect(find.textContaining('/'), findsNothing, reason: 'no page counter');
    expect(find.byType(CircularProgressIndicator), findsNothing);
  });

  testWidgets('Latest Episodes opens the watch page when a card is tapped', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(800, 1600);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: ListView(
            children: [
              LatestEpisodes(future: Future.value([_episode(1)])),
            ],
          ),
        ),
      ),
    );
    await tester.pump();

    await tester.tap(find.text('Anime 1'));
    await tester.pump();
    await tester.pump();

    expect(find.byType(WatchScreen), findsOneWidget);
  });

  testWidgets('Latest Episodes shows a skeleton while loading', (tester) async {
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: ListView(
            children: [
              LatestEpisodes(
                future: Future.delayed(
                  const Duration(seconds: 1),
                  () => [_episode(1)],
                ),
              ),
            ],
          ),
        ),
      ),
    );

    expect(find.text('Anime 1'), findsNothing);
    await tester.pump(const Duration(seconds: 1));
    expect(find.text('Anime 1'), findsOneWidget);
  });
}
