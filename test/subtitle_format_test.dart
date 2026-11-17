import 'package:flutter_test/flutter_test.dart';
import 'package:test1/services/subtitle_format.dart';

void main() {
  test('ASS dialogue becomes plain WebVTT cues', () {
    const ass = '''
[Script Info]
Title: Test

[V4+ Styles]
Format: Name, Fontname
Style: Default,Arial

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Dialogue: 0,0:00:01.23,0:00:03.45,Default,,0,0,0,,{\\i1}Hello, world{\\i0}
Dialogue: 0,0:00:04.00,0:00:06.50,Default,,0,0,0,,Line one\\NLine two
''';

    final vtt = normalizeSubtitle(ass);

    expect(vtt, startsWith('WEBVTT'));
    expect(vtt, contains('00:00:01.230 --> 00:00:03.450'));
    expect(vtt, contains('Hello, world'));
    expect(vtt, isNot(contains('{\\i1}')));
    expect(vtt, contains('Line one\nLine two'));
    expect(vtt, isNot(contains('[Events]')));
  });

  test('SSA timestamps and a missing Format line still resolve', () {
    const ssa = '''
[Script Info]

[Events]
Format: Marked, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Dialogue: Marked=0,0:00:10.5,0:00:12.9,Default,,0,0,0,,Short cue
''';

    final vtt = normalizeSubtitle(ssa);

    expect(vtt, contains('00:00:10.500 --> 00:00:12.900'));
    expect(vtt, contains('Short cue'));
  });

  test('WebVTT and SRT pass through untouched', () {
    const vtt = 'WEBVTT\n\n00:00:01.000 --> 00:00:02.000\nHi\n';
    const srt = '1\n00:00:01,000 --> 00:00:02,000\nHalo\n';

    expect(normalizeSubtitle(vtt), vtt);
    expect(normalizeSubtitle(srt), srt);
  });

  test('byte order mark is stripped', () {
    expect(normalizeSubtitle('\uFEFFWEBVTT\n'), 'WEBVTT\n');
  });

  test('malformed ASS falls back to empty cues instead of throwing', () {
    const broken = '''
[Events]
Format: Layer, Start, End, Text
Dialogue: 0,not-a-time,also-not,text
''';

    expect(() => normalizeSubtitle(broken), returnsNormally);
    expect(normalizeSubtitle(broken), startsWith('WEBVTT'));
  });
}
