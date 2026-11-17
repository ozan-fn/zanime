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

  test('subtitle picker shows only Indonesian and English', () {
    const subtitles = [
      WatchSubtitle(label: 'English · bee', url: 'en.vtt', isDefault: true),
      WatchSubtitle(label: 'Indonesian · sum', url: 'id.vtt'),
      WatchSubtitle(label: 'Arabic · sun', url: 'ar.vtt'),
    ];

    expect(subtitleOrder(subtitles), [1, 0], reason: 'Arabic is hidden');
    expect(defaultSubtitleIndex(subtitles), 1);
    expect(
      defaultSubtitleIndex(const [
        WatchSubtitle(label: 'English', url: 'en.vtt', isDefault: true),
        WatchSubtitle(label: 'Arabic', url: 'ar.vtt'),
      ]),
      0,
      reason: 'falls back to the provider default when no Indonesian track',
    );
    expect(
      defaultSubtitleIndex(const [
        WatchSubtitle(label: 'Arabic', url: 'ar.vtt', isDefault: true),
      ]),
      isNull,
      reason: 'a default track in a hidden language does not auto-select',
    );
    expect(defaultSubtitleIndex(const []), isNull);
    expect(
      subtitleOrder(const [
        WatchSubtitle(label: 'Bahasa Indonesia · bee', url: 'id.vtt'),
        WatchSubtitle(label: 'Subtitle 1 · sun', url: 'x.vtt'),
      ]),
      [0],
      reason:
          'language names inside a longer label still count; blanks stay out',
    );
  });
}
