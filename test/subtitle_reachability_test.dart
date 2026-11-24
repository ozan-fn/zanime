import 'package:test1/models/models.dart';
import 'package:test1/services/api.dart';
import 'package:test1/services/http_client.dart';
import 'package:test1/services/subtitle_format.dart';
import 'package:test1/screens/player/video_player_widget.dart'
    show subtitleOrder;
import 'package:flutter_test/flutter_test.dart';

/// The picker shows only tracks that are reachable and carry real cues
/// (the same check `_validSubtitleOrder` does in the player widget).
Future<List<int>> validSubtitleOrder(List<WatchSubtitle> subtitles) async {
  final order = subtitleOrder(subtitles);
  final results = await Future.wait([
    for (final i in order)
      () async {
        try {
          final response = await sharedHttpClient.get(
            Uri.parse(subtitles[i].url),
            headers: {
              'accept': '*/*',
              'referer': 'https://miruro.cx/',
              ...subtitles[i].headers,
            },
          );
          if (response.statusCode != 200) return null;
          if (normalizeSubtitle(response.body.trim()).isEmpty) return null;
          return i;
        } catch (_) {
          return null;
        }
      }(),
  ]);
  return [for (final r in results) if (r != null) r];
}

void main() {
  test(
    'Miruro anime subtitles are reachable and non-empty '
    '(Or3DRjOfPJpCM3slS9i_wiguw5sXRHTj ep 7)',
    () async {
      final data = await fetchWatchData(
        'Or3DRjOfPJpCM3slS9i_wiguw5sXRHTj',
        episode: 7,
      );
      expect(data.subtitles, isNotEmpty, reason: 'provider ships tracks');

      final shown = subtitleOrder(data.subtitles);
      expect(shown, isNotEmpty, reason: 'English/Indonesian tracks exist');

      final valid = await validSubtitleOrder(data.subtitles);
      // Every track the picker would list must pass the reachability + content
      // check; an empty result means all are broken/fetch-failing.
      expect(valid, isNotEmpty, reason: 'at least one usable track');
      expect(valid.length, shown.length, reason: 'no track is dead/empty');

      expect(
        data.subtitles.map((s) => s.label),
        contains(contains(RegExp(r'indonesian', caseSensitive: false))),
        reason: 'an Indonesian track is present',
      );
    },
    timeout: const Timeout(Duration(minutes: 2)),
  );
}
