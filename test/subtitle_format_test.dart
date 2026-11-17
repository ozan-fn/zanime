import 'package:flutter_test/flutter_test.dart';
import 'package:test1/services/subtitle_format.dart';

void main() {
  test('ASS/SSA payloads pass through so libass keeps their positioning', () {
    const ass = '''
[Script Info]
Title: Test

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Dialogue: 0,0:00:01.23,0:00:03.45,Top,,0,0,0,,{\\an8}Top line
Dialogue: 0,0:00:04.00,0:00:06.50,Default,,0,0,0,,Bottom line\\NSecond line
''';

    final result = normalizeSubtitle(ass);

    expect(result, ass);
    expect(result, contains('{\\an8}'));
  });

  test('WebVTT and SRT pass through untouched', () {
    const vtt = 'WEBVTT\n\n00:00:01.000 --> 00:00:02.000\nHi\n';
    const srt = '1\n00:00:01,000 --> 00:00:02,000\nHalo\n';

    expect(normalizeSubtitle(vtt), vtt);
    expect(normalizeSubtitle(srt), srt);
  });

  test('blank and markup-only cues are dropped', () {
    const vtt =
        'WEBVTT\n\n'
        '00:00:01.000 --> 00:00:02.000\n\n'
        '00:00:03.000 --> 00:00:04.000\n<i></i>\n\n'
        '00:00:05.000 --> 00:00:06.000\nHi\n';

    final result = normalizeSubtitle(vtt);

    expect(result, isNot(contains('00:00:01.000')));
    expect(result, isNot(contains('00:00:03.000')));
    expect(result, contains('00:00:05.000 --> 00:00:06.000\nHi'));
  });

  test('duplicate WebVTT cues and repeated lines are collapsed', () {
    const vtt =
        'WEBVTT\n\n'
        '00:00:01.000 --> 00:00:03.000\nSame line\n\n'
        '00:00:01.000 --> 00:00:03.000\nSame line\n\n'
        '00:00:05.000 --> 00:00:07.000\nOther\nOther\n';

    final result = normalizeSubtitle(vtt);

    expect('Same line'.allMatches(result), hasLength(1));
    expect('Other'.allMatches(result), hasLength(1));
    expect(result, contains('00:00:05.000 --> 00:00:07.000\nOther'));
  });

  test('byte order mark is stripped', () {
    expect(normalizeSubtitle('\uFEFFWEBVTT\n'), 'WEBVTT\n');
  });

  test('HTML entities and ASS escapes become plain characters', () {
    const vtt =
        'WEBVTT\n\n'
        '00:00:01.000 --> 00:00:02.000\n'
        r'A &amp; B &#39;quoted&#39; &#x266A; C\Nnext line'
        '\n';

    final result = normalizeSubtitle(vtt);

    expect(result, contains("A & B 'quoted' \u266A C\nnext line"));
    expect(result, isNot(contains('&amp;')));
    expect(result, isNot(contains('&#39;')));
    expect(result, isNot(contains(r'\N')));
  });

  test('malformed ASS does not throw', () {
    const broken = '''
[Events]
Format: Layer, Start, End, Text
Dialogue: 0,not-a-time,also-not,text
''';

    expect(() => normalizeSubtitle(broken), returnsNormally);
  });
}
