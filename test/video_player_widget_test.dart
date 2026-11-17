import 'package:flutter_test/flutter_test.dart';
import 'package:material_ui/material_ui.dart';
import 'package:test1/screens/player/video_player_widget.dart';
import 'package:test1/models/models.dart';

void main() {
  testWidgets('player falls back to an error card when libmpv is unavailable', (
    tester,
  ) async {
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: VideoPlayerWidget(
            streams: [
              WatchStream(label: 'auto', url: 'https://example.com/v.m3u8'),
            ],
          ),
        ),
      ),
    );
    await tester.pump();

    expect(find.text('Video gagal dimuat'), findsOneWidget);
    expect(
      tester.getSize(find.byType(VideoPlayerWidget)),
      const Size(800, 450),
      reason: 'keeps the 16:9 box even when playback fails',
    );
  });

  testWidgets('player accepts stream headers and subtitle tracks', (
    tester,
  ) async {
    const subtitles = [
      WatchSubtitle(label: 'English', url: 'https://example.com/en.vtt'),
      WatchSubtitle(
        label: 'Indonesian',
        url: 'https://example.com/id.vtt',
        isDefault: true,
      ),
    ];

    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: VideoPlayerWidget(
            streams: [
              WatchStream(label: '1080p', url: 'https://example.com/v.m3u8'),
              WatchStream(label: '720p', url: 'https://example.com/v720.m3u8'),
            ],
            headers: {'Referer': 'https://miruro.cx/'},
            subtitles: subtitles,
            subtitleHeaders: {'Referer': 'https://miruro.cx/'},
          ),
        ),
      ),
    );
    await tester.pump();

    final player = tester.widget<VideoPlayerWidget>(
      find.byType(VideoPlayerWidget),
    );
    expect(player.headers?['Referer'], 'https://miruro.cx/');
    expect(player.streams, hasLength(2));
    expect(player.subtitles, hasLength(2));
    expect(find.text('Video gagal dimuat'), findsOneWidget);
  });

  test('subtitle picker puts Indonesian first and defaults to it', () {
    const subtitles = [
      WatchSubtitle(label: 'English', url: 'en.vtt', isDefault: true),
      WatchSubtitle(label: 'Indonesian', url: 'id.vtt'),
    ];

    expect(subtitleOrder(subtitles), [1, 0]);
    expect(defaultSubtitleIndex(subtitles), 1);
    expect(
      defaultSubtitleIndex(const [
        WatchSubtitle(label: 'English', url: 'en.vtt', isDefault: true),
      ]),
      0,
      reason: 'falls back to the provider default when no Indonesian track',
    );
    expect(defaultSubtitleIndex(const []), isNull);
  });
}
