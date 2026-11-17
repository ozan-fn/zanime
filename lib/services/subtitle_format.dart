/// Subtitle payload normalisation.
///
/// ASS/SSA are handed to libmpv as-is: libass renders them natively, which is
/// what preserves `\an` top/bottom positioning and styling. Flattening them to
/// WebVTT made every line render at the bottom (looked like duplicate lines).
///
/// VTT/SRT go through the Flutter subtitle layer; providers often ship the
/// same line twice, so identical overlapping cues and repeated lines are
/// collapsed there.
String normalizeSubtitle(String content) {
  final text = content.startsWith('\uFEFF') ? content.substring(1) : content;
  if (text.contains('[Events]')) return text;
  return _dropEmptyCues(text);
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
