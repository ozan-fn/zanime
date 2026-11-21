import 'package:flutter_test/flutter_test.dart';
import 'package:test1/services/translate.dart';

const _vtt =
    'WEBVTT\n\n'
    '00:00:01.000 --> 00:00:02.000\nHello\n\n'
    '00:00:03.000 --> 00:00:04.000\nSame\n\n'
    '00:00:05.000 --> 00:00:06.000\nSame\n';

void main() {
  test('translateSubtitle keeps timings and translates every cue', () async {
    final progress = <int>[];
    final srt = await translateSubtitle(
      _vtt,
      translate: (texts) async => [for (final text in texts) 'ID:$text'],
      onProgress: (done, total) => progress.add(done),
    );

    expect(srt, contains('00:00:01,000 --> 00:00:02,000\nID:Hello'));
    expect('ID:Same'.allMatches(srt), hasLength(2));
    expect(srt.trimRight(), endsWith('ID:Same'));
    expect(progress.last, 3, reason: 'progress counts cues, not requests');
  });

  test('cues are batched instead of one request per line', () async {
    final sizes = <int>[];
    final body = [
      for (var i = 0; i < 45; i++)
        '00:00:01.000 --> 00:00:02.000\nLine number $i',
    ].join('\n\n');
    await translateSubtitle(
      'WEBVTT\n\n$body\n',
      translate: (texts) async {
        sizes.add(texts.length);
        return texts;
      },
    );

    expect(sizes.reduce((a, b) => a + b), 45);
    expect(sizes.every((size) => size <= 20), isTrue);
    expect(sizes.length, greaterThan(1));
  });

  test('a failed batch keeps the original lines', () async {
    final srt = await translateSubtitle(
      _vtt,
      translate: (texts) async => throw Exception('429'),
    );

    expect(srt, contains('Hello'));
    expect(srt, contains('Same'));
  });

  test('an empty payload yields no captions', () async {
    expect(await translateSubtitle('WEBVTT\n'), '');
  });
}
