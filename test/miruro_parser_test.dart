import 'package:flutter_test/flutter_test.dart';
import 'package:test1/services/api.dart';

void main() {
  test('parseLatestEpisodes extracts the miruro raw array', () {
    const html = '<html><script>\n'
        'const a={}, b={};\n'
        'a.english="Frieren"; a.romaji="Sousou no Frieren"; b.sub=12; b.dub=3;\n'
        'raw:{latest:[{animeId:"id1",episode:5,coverUrl:"https://img/1.jpg",'
        'airingAt:1700000000,format:"TV",seasonYear:2024,title:a,episodeCounts:b},'
        '{animeId:"id2",episode:2,coverUrl:"https://img/2.jpg",'
        'airingAt:1700000100,format:"ONA",seasonYear:2023,'
        'title:{romaji:"Only Romaji"},episodeCounts:{sub:7}}],upcoming:[]}\n'
        '</script></html>';

    final episodes = parseLatestEpisodes(html);

    expect(episodes, hasLength(2));
    expect(episodes.first.animeId, 'id1');
    expect(episodes.first.title, 'Frieren');
    expect(episodes.first.romaji, 'Sousou no Frieren');
    expect(episodes.first.episode, 5);
    expect(episodes.first.subCount, 12);
    expect(episodes.first.dubCount, 3);
    expect(
      episodes.first.airedAt,
      DateTime.fromMillisecondsSinceEpoch(1700000000 * 1000, isUtc: true),
    );
    expect(episodes.last.title, 'Only Romaji');
    expect(episodes.last.subCount, 7);
    expect(episodes.last.dubCount, isNull);
  });
}
