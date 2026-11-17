import 'package:flutter_test/flutter_test.dart';
import 'package:material_ui/material_ui.dart';
import 'package:test1/widgets/upcoming_countdown.dart';
import 'package:test1/screens/detail/watch_screen.dart';
import 'package:test1/models/models.dart';

void main() {
  test('formatCountdown picks the right unit', () {
    expect(formatCountdown(const Duration(seconds: 30)), '<1m');
    expect(formatCountdown(const Duration(minutes: 12)), '12m');
    expect(formatCountdown(const Duration(hours: 3, minutes: 12)), '3j 12m');
    expect(formatCountdown(const Duration(days: 2, hours: 3)), '2d 3j');
  });

  testWidgets('countdown hides once the episode has aired', (tester) async {
    final airDate = DateTime(2026, 1, 1, 12);

    await tester.pumpWidget(
      MaterialApp(
        home: UpcomingCountdown(
          episode: 5,
          airDate: airDate,
          now: () => airDate.add(const Duration(minutes: 1)),
        ),
      ),
    );

    expect(find.textContaining('tayang dalam'), findsNothing);
  });

  testWidgets('WatchScreen shows the upcoming episode above the player', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(800, 1600);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    final airDate = DateTime.now().add(const Duration(days: 2, hours: 3));
    final data = WatchData(
      anime: const WatchAnime(
        title: 'Psyren',
        imageUrl: 'https://example.com/c.jpg',
        description: '',
        format: 'TV',
        status: 'RELEASING',
        season: 'FALL',
        genres: [],
        studios: [],
      ),
      episodes: [
        const WatchEpisode(
          number: 1,
          title: 'Episode 1',
          airDate: '2020-01-01',
        ),
        WatchEpisode(
          number: 2,
          title: 'Episode 2',
          airDate: airDate.toIso8601String(),
        ),
      ],
      servers: const [],
      currentEpisode: 1,
    );

    await tester.pumpWidget(
      MaterialApp(
        home: WatchScreen(animeId: 'x', loader: (_, _) async => data),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.textContaining('Episode 2 tayang dalam'), findsOneWidget);
  });
}
