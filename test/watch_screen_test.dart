import 'package:flutter_test/flutter_test.dart';
import 'package:material_ui/material_ui.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:test1/widgets/mini_player.dart';
import 'package:test1/screens/detail/watch_screen.dart';
import 'package:test1/screens/player/video_player_widget.dart'
    show VideoPlayerWidget;
import 'package:test1/models/models.dart';
import 'package:test1/services/anilist_auth.dart';
import 'package:test1/services/mini_player.dart';

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
  aniListId: 21,
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
  aniListId: 21,
);

void main() {
  testWidgets('WatchScreen falls back to episode 1 when requested is missing', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(800, 1600);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final calls = <int>[];
    final base = _data(1);
    await tester.pumpWidget(
      MaterialApp(
        home: WatchScreen(
          animeId: 'x',
          episode: 99,
          loader: (_, episode) async {
            calls.add(episode);
            return WatchData(
              anime: base.anime,
              episodes: base.episodes.take(3).toList(),
              servers: const [],
              currentEpisode: episode,
              aniListId: 21,
            );
          },
          statusLoader: (_) async => null,
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(calls, [99, 1]);
    expect(find.text('Gagal memuat anime'), findsNothing);
    expect(find.text('Episode 1'), findsWidgets);
  });

  testWidgets('WatchScreen retries with episode 1 when the episode fails', (
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
          episode: 5,
          loader: (_, episode) async {
            calls.add(episode);
            if (episode != 1) throw Exception('episode not found');
            return _data(1);
          },
          statusLoader: (_) async => null,
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(calls, [5, 1]);
    expect(find.text('Gagal memuat anime'), findsNothing);
    expect(find.text('Episode 1'), findsWidgets);
  });

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
          statusLoader: (_) async => null,
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(
      find.text('Delicious in Dungeon'),
      findsNWidgets(2),
      reason: 'AppBar title plus the synopsis header',
    );
    expect(find.text('Winter 2024 · TV · 1200 eps · 25 min'), findsOneWidget);
    expect(find.text('Finished · TRIGGER'), findsOneWidget);
    expect(find.text('Server tidak tersedia'), findsOneWidget);
    expect(find.text('1200'), findsNothing);

    await tester.tap(find.text('2'));
    await tester.pumpAndSettle();

    expect(calls, [1, 2]);
    expect(find.textContaining('Episode 2 · Jan 4, 2024'), findsNWidgets(2));
  });

  testWidgets(
    'WatchScreen keeps the server picker and passes subtitles to the player',
    (tester) async {
      tester.view.physicalSize = const Size(800, 1600);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      await tester.pumpWidget(
        MaterialApp(
          home: WatchScreen(
            animeId: 'x',
            loader: (_, episode) async => _dataWithServers(episode),
            statusLoader: (_) async => null,
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
      expect(player.subtitles.map((subtitle) => subtitle.label), [
        'English',
        'Indonesian',
      ]);
    },
  );

  testWidgets('WatchScreen remembers the last picked server', (tester) async {
    tester.view.physicalSize = const Size(800, 1600);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    Widget screen() => MaterialApp(
      home: WatchScreen(
        animeId: 'x',
        loader: (_, episode) async => _dataWithServers(episode),
        statusLoader: (_) async => null,
      ),
    );

    await tester.pumpWidget(screen());
    await tester.pumpAndSettle();
    await tester.tap(find.text('sun'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('bee').last);
    await tester.pumpAndSettle();

    expect(
      tester
          .widget<VideoPlayerWidget>(find.byType(VideoPlayerWidget))
          .streams
          .single
          .url,
      'https://example.com/b.m3u8',
    );

    // A fresh watch screen (e.g. another anime) keeps the same provider.
    await tester.pumpWidget(const SizedBox());
    await tester.pumpWidget(screen());
    await tester.pumpAndSettle();

    expect(find.text('bee'), findsOneWidget);
    expect(
      tester
          .widget<VideoPlayerWidget>(find.byType(VideoPlayerWidget))
          .streams
          .single
          .url,
      'https://example.com/b.m3u8',
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
          statusLoader: (_) async => null,
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Show more'), findsOneWidget);
    await tester.tap(find.text('Show more'));
    await tester.pumpAndSettle();
    expect(find.text('Show less'), findsOneWidget);
  });

  testWidgets('WatchScreen saves an AniList status from the add dialog', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(800, 1600);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final saved = <(int, String)>[];
    await tester.pumpWidget(
      MaterialApp(
        home: WatchScreen(
          animeId: 'x',
          loader: (_, episode) async => _data(episode),
          saver: (mediaId, status) async {
            saved.add((mediaId, status));
            return true;
          },
          statusLoader: (_) async => null,
        ),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.byIcon(Icons.add));
    await tester.pumpAndSettle();
    expect(find.text('AniList status'), findsOneWidget);
    expect(find.text('Watching'), findsOneWidget);
    expect(find.text('Completed'), findsOneWidget);
    expect(
      find.descendant(
        of: find.byType(AlertDialog),
        matching: find.byType(OutlinedButton),
      ),
      findsNothing,
      reason: 'all status options are solid buttons',
    );
    expect(
      find.descendant(
        of: find.byType(AlertDialog),
        matching: find.byType(FilledButton),
      ),
      findsNWidgets(aniListStatusLabels.length),
    );

    await tester.tap(find.text('Completed'));
    await tester.pumpAndSettle();

    expect(saved, [(21, 'COMPLETED')]);
    expect(find.byIcon(Icons.check), findsOneWidget, reason: 'status icon');
    expect(find.text('Status: Completed'), findsOneWidget);
    expect(find.text('Add'), findsNothing, reason: 'icon-only button');
  });

  testWidgets('WatchScreen removes the status when the active one is tapped', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(800, 1600);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final removed = <int>[];
    final saved = <(int, String)>[];
    await tester.pumpWidget(
      MaterialApp(
        home: WatchScreen(
          animeId: 'x',
          loader: (_, episode) async => _data(episode),
          statusLoader: (_) async => 'CURRENT',
          saver: (mediaId, status) async {
            saved.add((mediaId, status));
            return true;
          },
          remover: (mediaId) async {
            removed.add(mediaId);
            return true;
          },
        ),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.byIcon(Icons.play_arrow));
    await tester.pumpAndSettle();
    expect(
      find.text('Tap status yang sedang aktif untuk menghapus dari list.'),
      findsOneWidget,
    );

    await tester.tap(find.text('Watching'));
    await tester.pumpAndSettle();

    expect(removed, [21]);
    expect(saved, isEmpty);
    expect(find.byIcon(Icons.add), findsOneWidget);
    expect(find.text('Dihapus dari list'), findsOneWidget);
  });

  testWidgets('WatchScreen shows the saved list status icon', (tester) async {
    tester.view.physicalSize = const Size(800, 1600);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    await tester.pumpWidget(
      MaterialApp(
        home: WatchScreen(
          animeId: 'x',
          loader: (_, episode) async => _data(episode),
          statusLoader: (_) async => 'CURRENT',
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.byIcon(Icons.play_arrow), findsOneWidget);
    expect(find.byIcon(Icons.add), findsNothing);
  });

  testWidgets('Back keeps playback in the floating player', (tester) async {
    addTearDown(hideMiniPlayer);
    tester.view.physicalSize = const Size(800, 1600);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    await tester.pumpWidget(
      MaterialApp(
        navigatorKey: navigatorKey,
        home: Builder(
          builder: (context) => TextButton(
            onPressed: () => Navigator.of(context).push(
              MaterialPageRoute<void>(
                builder: (_) => WatchScreen(
                  animeId: 'x',
                  loader: (_, episode) async => _dataWithServers(episode),
                  statusLoader: (_) async => null,
                ),
              ),
            ),
            child: const Text('open'),
          ),
        ),
      ),
    );
    await tester.tap(find.text('open'));
    await tester.pumpAndSettle();
    expect(find.byType(VideoPlayerWidget), findsOneWidget);
    final playingUrl = tester
        .widget<VideoPlayerWidget>(find.byType(VideoPlayerWidget))
        .streams
        .single
        .url;

    await tester.binding.handlePopRoute();
    await tester.pumpAndSettle();

    expect(find.text('open'), findsOneWidget);
    final playback = miniPlayback.value;
    expect(playback, isNotNull);
    expect(playback!.animeId, 'x');
    expect(playback.episode, 1);
    expect(playback.streams.single.url, playingUrl);
    expect(
      tester
          .widgetList<VideoPlayerWidget>(find.byType(VideoPlayerWidget))
          .single
          .showControls,
      isFalse,
      reason: 'floating player has no controls',
    );

    // Tapping the floating card hides it and reopens the watch page with the
    // already-loaded data, so no loading indicator appears.
    await tester.tap(find.byType(VideoPlayerWidget), warnIfMissed: false);
    await tester.pump();
    expect(find.byType(CircularProgressIndicator), findsNothing);
    await tester.pump();
    expect(find.byType(WatchScreen), findsOneWidget);
    await tester.pumpAndSettle();
    expect(miniPlayback.value, isNull);
  });

  testWidgets('Floating player can be dragged', (tester) async {
    addTearDown(hideMiniPlayer);
    tester.view.physicalSize = const Size(800, 1600);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    await tester.pumpWidget(
      MaterialApp(
        navigatorKey: navigatorKey,
        home: const Scaffold(body: Text('home')),
      ),
    );
    final data = _dataWithServers(1);
    final server = data.servers.first;
    showMiniPlayer(
      MiniPlayback(
        animeId: 'x',
        episode: 1,
        streams: server.streams,
        headers: server.headers,
        subtitles: data.subtitles,
        subtitleHeaders: server.headers,
      ),
    );
    await tester.pump();

    final before = tester.getTopLeft(find.byType(VideoPlayerWidget));
    await tester.drag(find.byType(VideoPlayerWidget), const Offset(-200, -300));
    await tester.pump();
    final after = tester.getTopLeft(find.byType(VideoPlayerWidget));

    expect(after.dx, lessThan(before.dx));
    expect(after.dy, lessThan(before.dy));
  });

  testWidgets('Floating player can be closed', (tester) async {
    addTearDown(hideMiniPlayer);
    await tester.pumpWidget(
      MaterialApp(
        navigatorKey: navigatorKey,
        home: const Scaffold(body: Text('home')),
      ),
    );
    final data = _dataWithServers(1);
    final server = data.servers.first;
    showMiniPlayer(
      MiniPlayback(
        animeId: 'x',
        episode: 1,
        streams: server.streams,
        headers: server.headers,
        subtitles: data.subtitles,
        subtitleHeaders: server.headers,
      ),
    );
    await tester.pump();

    expect(find.byType(VideoPlayerWidget), findsOneWidget);
    await tester.tap(find.byTooltip('Tutup'));
    await tester.pump();

    expect(miniPlayback.value, isNull);
    expect(find.byType(VideoPlayerWidget), findsNothing);
  });

  testWidgets('Watch status dialog follows the active MAL service', (
    tester,
  ) async {
    SharedPreferences.setMockInitialValues({'list_service': 'myanimelist'});
    tester.view.physicalSize = const Size(800, 1600);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    final base = _data(1);
    final data = WatchData(
      anime: base.anime,
      episodes: base.episodes.take(3).toList(),
      servers: const [],
      currentEpisode: 1,
      // MAL resolves entries from the title, so no AniList id is required.
      aniListId: null,
    );
    await tester.pumpWidget(
      MaterialApp(
        home: WatchScreen(
          animeId: 'x',
          initialData: data,
          loader: (_, _) async => data,
          statusLoader: (_) async => null,
          saver: (_, _) async => true,
          remover: (_) async => true,
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.byTooltip('Add to MyAnimeList'), findsOneWidget);
    await tester.tap(find.byTooltip('Add to MyAnimeList'));
    await tester.pumpAndSettle();

    expect(find.text('MyAnimeList status'), findsOneWidget);
    expect(find.text('AniList status'), findsNothing);
  });

  testWidgets('Watch screen reports an anime missing from the list service', (
    tester,
  ) async {
    SharedPreferences.setMockInitialValues({'list_service': 'myanimelist'});
    tester.view.physicalSize = const Size(800, 1600);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    final base = _data(1);
    final data = WatchData(
      anime: base.anime,
      episodes: base.episodes.take(3).toList(),
      servers: const [],
      currentEpisode: 1,
      aniListId: null,
    );
    await tester.pumpWidget(
      MaterialApp(
        home: WatchScreen(
          animeId: 'x',
          initialData: data,
          loader: (_, _) async => data,
          statusLoader: (_) async => null,
          saver: (_, _) async => false,
          remover: (_) async => false,
        ),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.byTooltip('Add to MyAnimeList'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Watching'));
    await tester.pumpAndSettle();

    expect(find.text('Anime tidak tersedia di MyAnimeList'), findsOneWidget);
  });
}
