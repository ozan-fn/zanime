/// Subtitle payload normalisation.
///
/// media_kit/libmpv handles VTT and SRT text tracks and exposes their lines to
/// the Flutter subtitle layer, which is where our font/outline styling applies.
/// ASS/SSA are rendered by libass inside libmpv instead, ignore that styling
/// and need system fonts, so they are flattened to plain WebVTT cues.
String normalizeSubtitle(String content) {
  final text = content.startsWith('\uFEFF') ? content.substring(1) : content;
  if (!text.contains('[Events]')) return text;
  return _assToVtt(text);
}

String _assToVtt(String content) {
  final lines = content.split(RegExp(r'\r?\n'));
  final cues = StringBuffer('WEBVTT\n');
  var inEvents = false;
  var order = const <String>[];

  for (final line in lines) {
    final trimmed = line.trim();
    if (trimmed.startsWith('[')) {
      inEvents = trimmed.toLowerCase() == '[events]';
      continue;
    }
    if (!inEvents) continue;
    final lower = trimmed.toLowerCase();
    if (lower.startsWith('format:')) {
      order = trimmed
          .substring('format:'.length)
          .split(',')
          .map((field) => field.trim().toLowerCase())
          .toList();
      continue;
    }
    if (!lower.startsWith('dialogue:')) continue;
    if (order.isEmpty) continue;
    final fields = _splitFields(
      trimmed.substring('dialogue:'.length).trim(),
      order.length,
    );
    if (fields.length < order.length) continue;

    final start = fields[order.indexOf('start')];
    final end = fields[order.indexOf('end')];
    final body = fields[order.indexOf('text')]
        .replaceAll(RegExp(r'\{[^}]*\}'), '')
        .replaceAll(RegExp(r'\\[Nn]'), '\n')
        .trim();
    if (body.isEmpty) continue;

    cues.write('\n${_vttTime(start)} --> ${_vttTime(end)}\n$body\n');
  }
  return cues.toString();
}

/// ASS keeps commas inside its last field (the text), so only split `count-1`.
List<String> _splitFields(String value, int count) {
  if (count <= 0) return const [];
  final parts = value.split(',');
  if (parts.length <= count) return parts;
  return [...parts.take(count - 1), parts.skip(count - 1).join(',')];
}

/// `H:MM:SS.cc` (ASS) or `H:MM:SS,mmm` (SRT-ish) to `HH:MM:SS.mmm`.
String _vttTime(String value) {
  final match = RegExp(
    r'^\s*(\d+):(\d{1,2}):(\d{1,2})[.,](\d{1,3})',
  ).firstMatch(value);
  if (match == null) return '00:00:00.000';
  final hours = int.parse(match[1]!).toString().padLeft(2, '0');
  final minutes = int.parse(match[2]!).toString().padLeft(2, '0');
  final seconds = int.parse(match[3]!).toString().padLeft(2, '0');
  final millis = int.parse(match[4]!.padRight(3, '0').substring(0, 3));
  return '$hours:$minutes:$seconds.${millis.toString().padLeft(3, '0')}';
}
