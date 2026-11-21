/// Subtitle payload normalisation into a format the Flutter caption layer
/// parses (SRT). video_player has no libass, so ASS/SSA are flattened here:
/// override tags such as `{\an8}` are dropped, which means a top-positioned
/// sign renders at the bottom like a regular line.
///
/// Providers often ship the same line twice, so identical overlapping cues and
/// repeated lines are collapsed.
String normalizeSubtitle(String content) {
  final text = content.startsWith('\uFEFF') ? content.substring(1) : content;
  if (text.contains('[Events]')) return _assToSrt(text);
  return _dropEmptyCues(text);
}

/// Flattens ASS `Dialogue:` lines into numbered SubRip cues.
String _assToSrt(String ass) {
  final cues = <String>[];
  for (final line in ass.split(RegExp(r'\r?\n'))) {
    if (!line.startsWith('Dialogue:')) continue;
    // Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
    final fields = line.substring('Dialogue:'.length).split(',');
    if (fields.length < 10) continue;
    // Vector drawings flatten to junk (`m 0 0 l ...`), not text.
    if (RegExp(r'\{[^}]*\\p\d').hasMatch(fields[9])) continue;
    final start = _timeMs(fields[1]);
    final end = _timeMs(fields[2]);
    if (start == null || end == null || end <= start) continue;
    final body = _plainText(fields.skip(9).join(','));
    if (body.isEmpty) continue;
    cues.add(
      '${cues.length + 1}\n${srtTimestamp(start)} --> ${srtTimestamp(end)}\n$body',
    );
  }
  return cues.isEmpty ? '' : _dropEmptyCues(cues.join('\n\n'));
}

/// `HH:MM:SS,mmm`, the timestamp format SubRip parsers require.
String srtTimestamp(int milliseconds) {
  String pad(int value) => value.toString().padLeft(2, '0');
  final hours = milliseconds ~/ 3600000;
  final minutes = milliseconds ~/ 60000 % 60;
  final seconds = milliseconds ~/ 1000 % 60;
  return '${pad(hours)}:${pad(minutes)}:${pad(seconds)},'
      '${(milliseconds % 1000).toString().padLeft(3, '0')}';
}

class _Cue {
  const _Cue(this.start, this.end, this.body);

  final int start;
  final int end;
  final String body;
}

/// Drops cues whose text is blank or markup-only, and cues repeating a line
/// that is already on screen at the same time.
String _dropEmptyCues(String text) {
  final blocks = text.split(RegExp(r'\r?\n[ \t]*\r?\n'));
  final kept = <String>[];
  final emitted = <_Cue>[];
  for (final block in blocks) {
    final lines = block.split(RegExp(r'\r?\n'));
    final timeIndex = lines.indexWhere((line) => line.contains('-->'));
    if (timeIndex < 0) {
      kept.add(block);
      continue;
    }
    final body = _dedupeLines(
      lines.skip(timeIndex + 1).map(_plainText).toList(),
    );
    if (body.isEmpty) continue;

    final times = lines[timeIndex].split('-->');
    final startMs = _timeMs(times[0]);
    final endMs = times.length > 1 ? _timeMs(times[1]) : null;
    if (_isDuplicate(emitted, body, startMs, endMs)) continue;
    if (startMs != null && endMs != null) {
      emitted.add(_Cue(startMs, endMs, body));
    }

    final trailer = block.endsWith('\n') ? '\n' : '';
    kept.add([...lines.take(timeIndex + 1), body].join('\n') + trailer);
  }
  return kept.join('\n\n');
}

String _plainText(String line) {
  final stripped = line
      .replaceAll(RegExp(r'<[^>]*>'), '')
      .replaceAll(RegExp(r'\{[^}]*\}'), '');
  return _decodeEntities(
    stripped
        // ASS escapes that leaked into a plain text track.
        .replaceAll(r'\N', '\n')
        .replaceAll(r'\n', '\n')
        .replaceAll(r'\h', ' '),
  ).trim();
}

/// Providers often ship HTML entities; left raw they render as stray symbols.
String _decodeEntities(String text) {
  var result = text
      .replaceAll('&nbsp;', ' ')
      .replaceAll('&amp;', '&')
      .replaceAll('&lt;', '<')
      .replaceAll('&gt;', '>')
      .replaceAll('&quot;', '"')
      .replaceAll('&apos;', "'")
      .replaceAll('&#39;', "'");
  return result.replaceAllMapped(RegExp(r'&#(x?)([0-9a-fA-F]+);'), (match) {
    final code = match[1] == 'x'
        ? int.tryParse(match[2]!, radix: 16)
        : int.tryParse(match[2]!);
    if (code == null || code <= 0 || code > 0x10FFFF) return '';
    return String.fromCharCode(code);
  });
}

/// Keeps the first occurrence of each line, dropping repeats inside a cue.
String _dedupeLines(List<String> lines) {
  final result = <String>[];
  for (final raw in lines) {
    final line = _plainText(raw);
    if (line.isEmpty || result.contains(line)) continue;
    result.add(line);
  }
  return result.join('\n');
}

bool _isDuplicate(List<_Cue> cues, String body, int? start, int? end) {
  if (start == null || end == null) return false;
  return cues.any(
    (cue) => cue.body == body && start < cue.end && cue.start < end,
  );
}

int? _timeMs(String value) {
  final match = RegExp(r'(\d+):(\d{2}):(\d{2})[.,](\d{1,3})').firstMatch(value);
  if (match == null) return null;
  return Duration(
    hours: int.parse(match[1]!),
    minutes: int.parse(match[2]!),
    seconds: int.parse(match[3]!),
    milliseconds: int.parse(match[4]!.padRight(3, '0').substring(0, 3)),
  ).inMilliseconds;
}
