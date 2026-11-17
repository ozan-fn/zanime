import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:test1/models/models.dart';
import 'package:test1/services/sokuja.dart';

String _rsc(Map<String, dynamic> data) {
  final payload = base64Encode(utf8.encode(jsonEncode(data)));
  return '<script>self.__next_f.push([1,"$payload"])</script>';
}

WatchAnime _anime({
  String title = 'A-Rank Party wo Ridatsu shita Ore wa, Moto Oshiego-tachi to Meikyuu Shinbu wo Mezasu',
  int? year = 2025,
  List<String> studios = const ['Bandai Namco Pictures'],
  List<String> genres = const ['Action', 'Adventure', 'Fantasy'],
  int? episodes = 3,
  String description = 'Yuke leaves his A-Rank party and starts over.',
}) {
  return WatchAnime(
    title: title,
    imageUrl: '',
    description: description,
    format: 'TV',
    status: 'FINISHED',
    season: 'WINTER',
    genres: genres,
    studios: studios,
    year: year,
    episodes: episodes,
  );
}

String _detailHtml({
  String title = 'A-Rank Party wo Ridatsu shita Ore wa, Moto Oshiego-tachi to Meikyuu Shinbu wo Mezasu',
  String year = '2025',
  String studio = 'Bandai Namco Pictures',
  List<String> genres = const ['Action', 'Adventure', 'Fantasy'],
}) {
  return '''
<h1 class="x">$title<!-- --> Subtitle Indonesia</h1>
<dt class="x">Tipe</dt><dd class="text-gray-200">TV</dd>
<dt class="x">Tahun</dt><dd class="text-gray-200">$year</dd>
<dt class="x">Studio</dt><dd class="text-gray-200"><span><a href="/studio/x/">$studio</a></span></dd>
${genres.map((g) => '<a class="rounded-full bg-gray-800" href="/genre/${g.toLowerCase()}/">$g</a>').join()}
<h2>Sinopsis $title</h2><div class="prose"><p>Yuke keluar dari party A-Rank.</p></div>
${_rsc({
    'episodes': [
      {'id': 1, 'slug': 'ep-1', 'title': 'Ep 1', 'episodeNumber': 3},
      {'id': 2, 'slug': 'ep-2', 'title': 'Ep 2', 'episodeNumber': 1},
      {'id': 3, 'slug': 'ep-3', 'title': 'Ep 3', 'episodeNumber': 2},
    ],
  })}
''';
}

void main() {
  test('parseSokujaSearch reads results', () {
    final results = parseSokujaSearch(
      jsonEncode({
        'results': [
          {
            'slug': 'a-rank-party',
            'title': 'A-Rank Party',
            'year': 2025,
            'type': 'TV',
          },
        ],
      }),
    );
    expect(results, hasLength(1));
    expect(results.single.slug, 'a-rank-party');
    expect(results.single.year, 2025);
  });

  test('parseSokujaDetail reads metadata and RSC episodes', () {
    final detail = parseSokujaDetail(_detailHtml())!;
    expect(detail.title, startsWith('A-Rank Party'));
    expect(detail.year, 2025);
    expect(detail.studios, ['Bandai Namco Pictures']);
    expect(detail.genres, ['Action', 'Adventure', 'Fantasy']);
    expect(detail.synopsis, contains('Yuke'));
    expect(detail.episodes.map((e) => e.number), [3, 1, 2]);
    expect(detail.episodes.first.id, 1);
    expect(detail.episodes.first.slug, 'ep-1');
  });

  test('parseSokujaDetail returns null without an episode list', () {
    expect(parseSokujaDetail('<h1>Only title</h1>'), isNull);
  });

  test('parseSokujaMirrors sorts qualities and ignores empty urls', () {
    final streams = parseSokujaMirrors(
      jsonEncode({
        'mirrors': [
          {'quality': '480p', 'embedUrl': 'https://x/480.mp4'},
          {'quality': '1080p', 'embedUrl': 'https://x/1080.mp4'},
          {'quality': '720p', 'embedUrl': ''},
        ],
      }),
    );
    expect(streams.map((s) => s.label), ['1080p', '480p']);
  });

  test('sokujaMatchScore accepts the same anime and rejects a stranger', () {
    final detail = parseSokujaDetail(_detailHtml())!;
    final match = sokujaMatchScore(detail: detail, anime: _anime());
    expect(match, greaterThanOrEqualTo(0.55));

    final wrong = sokujaMatchScore(
      detail: detail,
      anime: _anime(
        title: 'One Piece',
        year: 1999,
        studios: const ['Toei Animation'],
        genres: const ['Comedy'],
        episodes: 1000,
      ),
    );
    expect(wrong, lessThan(0.55));
  });

  test('same title from another season scores lower than the exact year', () {
    final exact = parseSokujaDetail(_detailHtml())!;
    final otherSeason = parseSokujaDetail(_detailHtml(year: '2026'))!;
    final anime = _anime();
    expect(
      sokujaMatchScore(detail: exact, anime: anime),
      greaterThan(sokujaMatchScore(detail: otherSeason, anime: anime)),
    );
  });
}
