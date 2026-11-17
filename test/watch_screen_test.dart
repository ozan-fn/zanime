import 'package:flutter_test/flutter_test.dart';
import 'package:material_ui/material_ui.dart';
import 'package:test1/components/watch_screen.dart';
import 'package:test1/components/video_player_widget.dart' show VideoPlayerWidget;
import 'package:test1/models/models.dart';

WatchData _data(int episode) => WatchData(
  anime: const WatchAnime(
    title: 'Delicious in Dungeon',
    imageUrl: 'https://example.com/cover.jpg',
    description:
        'When young adventurer Laios and his company are attacked and soundly '
        'thrashed by a dragon deep in a dungeon, the party loses all its money '
        'and provisions and a member. They reenter the dungeon to save her, but '
        'this time they have a foolproof survival plan: eat the monsters.',
    format: 'TV',
    status: 'FINISHED',
    season: 'WINTER',
    genres: ['Adventure', 'Comedy', 'Fantasy'],
    studios: ['TRIGGER'],
    year: 2024,
    episodes: 1200,
    duration: 25,
  ),
  episodes: [
    for (var i = 1; i <= 1200; i++)
      WatchEpisode(number: i, title: 'Episode $i', airDate: '2024-01-04'),
  ],
  servers: const [],
  currentEpisode: episode,
);

WatchData _dataWithServers(int episode) => WatchData(
  anime: _data(episode).anime,
  episodes: _data(episode).episodes,
  servers: const [
    WatchServer(
      provider: 'sun',
      streams: [WatchStream(label: 'auto', url: 'https://example.com/v.m3u8')],
      headers: {},
    ),
    WatchServer(
      provider: 'bee',
      streams: [WatchStream(label: 'auto', url: 'https://example.com/b.m3u8')],
      headers: {},
    ),
  ],
  subtitles: const [
    WatchSubtitle(label: 'English', url: 'https://example.com/en.vtt'),
    WatchSubtitle(label: 'Indonesian', url: 'https://example.com/id.vtt'),
  ],
  currentEpisode: episode,
);

void main() {
  testWidgets('WatchScreen caps the episode grid and switches episodes', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(800, 1600);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final calls = <int>[];
    await tester.pumpWidget(
      MaterialApp(
        home: WatchScreen(
          animeId: 'x',
          loader: (_, episode) async {
            calls.add(episode);
            return _data(episode);
          },
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Delicious in Dungeon'), findsOneWidget);
    expect(find.text('Winter 2024 · TV · 1200 eps · 25 min'), findsOneWidget);
    expect(find.text('Finished · TRIGGER'), findsOneWidget);
    expect(find.text('Server tidak tersedia'), findsOneWidget);
    expect(find.text('1200'), findsNothing);

    await tester.tap(find.text('2'));
    await tester.pumpAndSettle();

    expect(calls, [1, 2]);
    expect(find.textContaining('Episode 2 · Jan 4, 2024'), findsNWidgets(2));
  });

  testWidgets('WatchScreen keeps the server picker and passes subtitles to the player', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(800, 1600);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    await tester.pumpWidget(
      MaterialApp(
        home: WatchScreen(
          animeId: 'x',
          loader: (_, episode) async => _dataWithServers(episode),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.byIcon(Icons.dns_outlined), findsOneWidget);
    expect(find.text('sun'), findsOneWidget);
    // The subtitle picker lives inside the player, not under the title.
    expect(find.text('English'), findsNothing);
    final player = tester.widget<VideoPlayerWidget>(
      find.byType(VideoPlayerWidget),
    );
    expect(
      player.subtitles.map((subtitle) => subtitle.label),
      ['English', 'Indonesian'],
    );
  });

  testWidgets('WatchScreen expands the synopsis', (tester) async {
    tester.view.physicalSize = const Size(800, 1600);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    await tester.pumpWidget(
      MaterialApp(
        home: WatchScreen(
          animeId: 'x',
          loader: (_, episode) async => _data(episode),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Show more'), findsOneWidget);
    await tester.tap(find.text('Show more'));
    await tester.pumpAndSettle();
    expect(find.text('Show less'), findsOneWidget);
  });
}
