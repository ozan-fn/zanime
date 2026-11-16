import 'package:flutter_test/flutter_test.dart';
import 'package:test1/models/models.dart';

void main() {
  test('WatchData parses the server JSON contract', () {
    final data = WatchData.fromJson({
      'anime': {
        'title': 'Dungeon Meshi',
        'imageUrl': 'https://img/cover.jpg',
        'description': 'Desc',
        'format': 'TV',
        'status': 'FINISHED',
        'season': 'WINTER',
        'genres': ['Adventure'],
        'studios': ['TRIGGER'],
        'year': 2024,
        'episodes': 24,
        'duration': 25,
        'score': 86,
      },
      'episodes': [
        {
          'number': 1,
          'title': 'Hot Pot',
          'imageUrl': null,
          'airDate': '2024-01-04',
          'description': null,
          'duration': 25,
        },
      ],
      'servers': [
        {
          'provider': 'sun',
          'url': 'https://cdn/1.m3u8',
          'headers': {'Referer': 'https://ultracloud.cc'},
        },
      ],
      'subtitles': [
        {
          'label': 'English',
          'url': 'https://sub/en.vtt',
          'isDefault': false,
        },
      ],
      'currentEpisode': 1,
    });

    expect(data.anime.title, 'Dungeon Meshi');
    expect(data.anime.studios, ['TRIGGER']);
    expect(data.episode?.title, 'Hot Pot');
    expect(data.servers.single.headers['Referer'], 'https://ultracloud.cc');
    expect(data.subtitles.single.label, 'English');
  });

  test('LatestEpisode parses the server JSON contract', () {
    final episode = LatestEpisode.fromJson({
      'animeId': 'id1',
      'title': 'Frieren',
      'romaji': 'Sousou no Frieren',
      'imageUrl': 'https://img/1.jpg',
      'episode': 5,
      'airedAt': '2024-01-04T00:00:00Z',
      'format': 'TV',
      'year': 2024,
      'subCount': 12,
      'dubCount': null,
    });

    expect(episode.airedAt, DateTime.utc(2024, 1, 4));
    expect(episode.url, 'https://miruro.cx/watch/id1?ep=5');
  });
}
