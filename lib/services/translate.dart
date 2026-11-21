import 'dart:convert';

import 'package:video_player/video_player.dart';

import 'http_client.dart';
import 'subtitle_format.dart';

/// One request's worth of lines in, the same count out (order preserved).
typedef Translator = Future<List<String>> Function(List<String> texts);

/// Cues translated so far, out of the whole track.
typedef TranslateProgress = void Function(int done, int total);

/// Google's dictionary endpoint (the one Chrome's translate extension calls):
/// no API key, repeated `q` params, one answer per line. Batching keeps a
/// 300-cue track at ~15 requests instead of 300.
const _maxCuesPerRequest = 20;
const _maxCharsPerRequest = 1500;

Future<List<String>> googleTranslate(List<String> texts) async {
  if (texts.isEmpty) return const [];
  final query = [
    'client=dict-chrome-ex',
    'sl=auto',
    'tl=id',
    for (final text in texts) 'q=${Uri.encodeQueryComponent(text)}',
  ].join('&');
  final response = await sharedHttpClient.get(
    Uri.parse('https://clients5.google.com/translate_a/t?$query'),
    headers: const {'accept': 'application/json'},
  );
  if (response.statusCode != 200) {
    throw ApiException('Terjemahan HTTP ${response.statusCode}');
  }
  final decoded = jsonDecode(response.body) as List;
  return [
    for (final row in decoded)
      if (row is List && row.isNotEmpty) '${row.first}' else '$row',
  ];
}

/// Rewrites an SRT/WebVTT payload into Indonesian SRT, keeping the timings.
/// [workers] batches are in flight at once; a failed batch keeps its source
/// lines rather than losing the whole track.
Future<String> translateSubtitle(
  String body, {
  Translator translate = googleTranslate,
  int workers = 2,
  TranslateProgress? onProgress,
}) async {
  final cues = _parseCues(normalizeSubtitle(body));
  if (cues.isEmpty) return '';
  final translated = await _translateAll(
    [for (final cue in cues) cue.text],
    translate: translate,
    workers: workers,
    onProgress: onProgress,
  );
  final buffer = StringBuffer();
  for (var i = 0; i < cues.length; i++) {
    buffer
      ..writeln('${i + 1}')
      ..writeln(
        '${srtTimestamp(cues[i].start.inMilliseconds)} --> '
        '${srtTimestamp(cues[i].end.inMilliseconds)}',
      )
      ..writeln(translated[i])
      ..writeln();
  }
  return buffer.toString();
}

class _Cue {
  const _Cue(this.start, this.end, this.text);

  final Duration start;
  final Duration end;
  final String text;
}

List<_Cue> _parseCues(String body) {
  final trimmed = body.trimLeft();
  if (trimmed.isEmpty) return const [];
  final file = trimmed.startsWith('WEBVTT')
      ? WebVTTCaptionFile(body)
      : SubRipCaptionFile(body);
  return [
    for (final caption in file.captions)
      _Cue(caption.start, caption.end, caption.text.trim()),
  ];
}

Future<List<String>> _translateAll(
  List<String> texts, {
  required Translator translate,
  required int workers,
  TranslateProgress? onProgress,
}) async {
  final results = List<String>.from(texts);
  final batches = <List<int>>[];
  var batch = <int>[];
  var chars = 0;
  for (var i = 0; i < texts.length; i++) {
    if (batch.isNotEmpty &&
        (batch.length >= _maxCuesPerRequest ||
            chars + texts[i].length > _maxCharsPerRequest)) {
      batches.add(batch);
      batch = <int>[];
      chars = 0;
    }
    batch.add(i);
    chars += texts[i].length;
  }
  if (batch.isNotEmpty) batches.add(batch);

  var done = 0;
  var next = 0;
  onProgress?.call(0, texts.length);
  Future<void> worker() async {
    while (next < batches.length) {
      final batch = batches[next++];
      try {
        final lines = await translate([for (final i in batch) texts[i]]);
        for (var i = 0; i < batch.length && i < lines.length; i++) {
          if (lines[i].trim().isNotEmpty) results[batch[i]] = lines[i];
        }
      } catch (_) {
        // Keep the source lines; a throttled batch is not a failed track.
      }
      done += batch.length;
      onProgress?.call(done, texts.length);
    }
  }

  await Future.wait([for (var i = 0; i < workers; i++) worker()]);
  return results;
}
