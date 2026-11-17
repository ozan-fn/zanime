import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:test1/models/models.dart';
import 'package:test1/services/api.dart';

void main() {
  group('parseAnimeBanners', () {
    test('maps AniList media, falling back to romaji and cover image', () {
      final body = jsonEncode({
        'data': {
          'Page': {
            'media': [
              {
                'title': {'romaji': 'Sousou no Frieren'},
                'bannerImage': 'https://img/banner.jpg',
                'coverImage': {'extraLarge': 'https://img/cover.jpg'},
                'genres': ['Adventure', 'Drama'],
                'seasonYear': 2023,
                'episodes': 28,
                'nextAiringEpisode': {'episode': 8},
              },
              {
                'title': {
                  'english': 'Dungeon Meshi',
                  'romaji': 'Dungeon Meshi',
                },
                'bannerImage': null,
                'coverImage': {'extraLarge': 'https://img/cover2.jpg'},
                'genres': ['Comedy'],
                'seasonYear': 2024,
                'episodes': 24,
                'nextAiringEpisode': null,
              },
            ],
          },
        },
      });

      final banners = parseAnimeBanners(body);

      expect(banners, hasLength(2));
      expect(banners.first.title, 'Sousou no Frieren');
      expect(banners.first.imageUrl, 'https://img/banner.jpg');
      expect(banners.first.aired, 7);
      expect(banners.first.meta, 'Adventure · Drama · 2023 · 7/28 eps');
      expect(banners.last.title, 'Dungeon Meshi');
      expect(banners.last.imageUrl, 'https://img/cover2.jpg');
      expect(banners.last.aired, 24, reason: 'no next airing episode');
    });
  });

  group('parseSearchResults', () {
    test('keeps the SvelteKit field names and falls back to romaji', () {
      final results = parseSearchResults({
        'items': [
          {
            'id': 'id1',
            'title': {'english': 'Frieren', 'romaji': 'Sousou'},
            'cover_url': 'https://img/1.jpg',
            'format': 'TV',
            'season_year': 2023,
            'episode_count': 28,
            'average_score': 91,
            'external_ids': {'anilist': 154587},
          },
          {
            'id': 'id2',
            'title': {'romaji': 'Only Romaji'},
            'cover_url': 'https://img/2.jpg',
            'format': null,
            'season_year': null,
            'episode_count': null,
            'average_score': null,
          },
        ],
      });

      expect(results.first.title, 'Frieren');
      expect(results.first.animeId, 'id1');
      expect(results.first.aniListId, 154587);
      expect(results.first.subtitle, 'TV · 2023 · 28 eps · ★ 91');
      expect(results.first.url, 'https://miruro.cx/watch/id1');
      expect(results.last.title, 'Only Romaji');
      expect(results.last.subtitle, isEmpty);
    });

    test('returns an empty list when items are missing', () {
      expect(parseSearchResults(const {}), isEmpty);
    });

    test('reads list-form AniList ids and tolerates missing fields', () {
      final results = parseSearchResults({
        'items': [
          {
            'id': 'id3',
            'title': {'english': null, 'romaji': 'Romaji only'},
            'cover_url': null,
            'external_ids': {
              'anilist': ['7'],
            },
          },
        ],
      });

      expect(results.single.title, 'Romaji only');
      expect(results.single.imageUrl, isEmpty);
      expect(results.single.aniListId, 7);
    });
  });

  group('pickMiruroAnimeId', () {
    const results = [
      SearchResult(
        animeId: 'other',
        title: 'Frieren',
        imageUrl: '',
        aniListId: 1,
      ),
      SearchResult(
        animeId: 'frieren-id',
        title: 'Frieren',
        imageUrl: '',
        aniListId: 154587,
      ),
    ];

    test('matches the AniList id, not the first result', () {
      expect(pickMiruroAnimeId(results, 154587), 'frieren-id');
    });

    test('returns null when Miruro has no matching AniList id', () {
      expect(pickMiruroAnimeId(results, 99), isNull);
      expect(pickMiruroAnimeId(const [], 1), isNull);
    });
  });

  group('parseWatchData', () {
    Map<String, dynamic> payload({bool includeCurrentEpisode = true}) => {
      'anilistId': 154587,
      'media': {
        'title': {'romaji': 'Sousou no Frieren'},
        'coverImage': {'extraLarge': 'https://img/cover.jpg'},
        'description': 'A mage and her party.',
        'format': 'TV',
        'status': 'FINISHED',
        'season': 'FALL',
        'genres': ['Adventure', 'Fantasy'],
        'studios': {
          'edges': [
            {
              'isMain': true,
              'node': {'name': 'Madhouse'},
            },
            {
              'isMain': false,
              'node': {'name': 'Not Main'},
            },
          ],
        },
        'seasonYear': 2023,
        'episodes': 28,
        'duration': 24,
        'averageScore': 91,
      },
      'episodesData': {
        'episodes': [
          {'number': 1, 'title': 'The Journey’s End', 'airDate': '2023-09-29'},
          {'number': 2, 'title': null},
        ],
        if (includeCurrentEpisode) 'currentEpisode': {'number': 2},
      },
      'sourcesData': {
        'tracks': [
          {
            'track': 'ssub',
            'providers': [
              {
                'provider': 'sun',
                'servers': [
                  {
                    'streams': [
                      {'url': 'https://mirror/a.m3u8', 'quality': '1080p'},
                      {'url': 'https://mirror/b.m3u8', 'quality': '720p'},
                    ],
                    'headers': {'Referer': 'https://miruro.cx/'},
                  },
                ],
                'subtitles': [
                  {'label': 'English', 'file': 'en.vtt', 'default': true},
                  {'label': 'Indonesian', 'file': 'id.vtt'},
                  {'label': 'Indonesia', 'file': 'id-alt.vtt'},
                  {'label': 'Arabic', 'file': 'ar.vtt'},
                  {'label': '', 'file': 'blank.vtt'},
                ],
              },
            ],
          },
        ],
      },
    };

    test('maps anime, episodes, servers and subtitles', () {
      final data = parseWatchData(payload(), 1);

      expect(data.anime.title, 'Sousou no Frieren');
      expect(data.anime.studios, ['Madhouse'], reason: 'only main studios');
      expect(data.anime.meta, 'TV · 28 eps · 24 min');
      expect(data.anime.statusLine, 'Finished · Madhouse');
      expect(data.anime.seasonLabel, 'Fall 2023');
      expect(data.episodes, hasLength(2));
      expect(data.episodes.last.title, '', reason: 'null title becomes empty');
      expect(data.currentEpisode, 2);
      expect(data.aniListId, 154587);

      expect(data.servers, hasLength(1));
      expect(data.servers.single.provider, 'sun');
      expect(data.servers.single.url, 'https://mirror/a.m3u8');
      expect(data.servers.single.streams.map((stream) => stream.label), [
        '1080p',
        '720p',
      ], reason: 'every stream becomes a quality option');
      expect(data.servers.single.headers['Referer'], 'https://miruro.cx/');

      expect(data.subtitles.map((subtitle) => subtitle.label), [
        'English · sun',
        'Indonesian · sun',
        'Indonesia · sun',
        'Arabic · sun',
        'Subtitle 1 · sun',
      ], reason: 'every track is listed, tagged with its provider, none blank');
      expect(data.subtitles.first.isDefault, isTrue);
    });

    test('subtitles from other providers dedupe by file', () {
      final payload2 = payload();
      (((payload2['sourcesData']['tracks'] as List)[0]
                  as Map<String, Object>)['providers']
              as List)
          .add(<String, Object>{
            'provider': 'bee',
            'servers': [
              {
                'streams': [
                  {'url': 'https://bee/a.m3u8'},
                ],
              },
            ],
            'subtitles': [
              // Same file as sun's English track: no duplicate row.
              {'label': 'English', 'file': 'en.vtt', 'default': true},
              {'label': 'Spanish', 'file': 'es.vtt'},
            ],
          });
      // Round-trip through JSON so every nested map is Map<String, dynamic>.
      final normalized =
          jsonDecode(jsonEncode(payload2)) as Map<String, dynamic>;
      final data = parseWatchData(normalized, 1);

      expect(data.subtitles.map((subtitle) => subtitle.label), [
        'English · sun',
        'Indonesian · sun',
        'Indonesia · sun',
        'Arabic · sun',
        'Subtitle 1 · sun',
        'Spanish · bee',
      ]);
      expect(data.subtitles.first.isDefault, isTrue);
    });

    test('falls back to the requested episode when unspecified', () {
      final data = parseWatchData(payload(includeCurrentEpisode: false), 7);

      expect(data.currentEpisode, 7);
      expect(data.episode, isNull);
    });
  });

  group('parseMiruroRoot', () {
    test('resolves node references and Promise chunks', () {
      final body = [
        jsonEncode({
          'type': 'data',
          'nodes': [
            null,
            {
              'data': [
                {
                  'media': ['Promise', 3],
                },
                'unused',
                'unused',
                3,
              ],
            },
          ],
        }),
        jsonEncode({
          'type': 'chunk',
          'id': 3,
          'data': [
            {
              'title': {'english': 'Frieren'},
            },
          ],
        }),
      ].join('\n');

      final root = parseMiruroRoot(body);

      expect((root['media'] as Map)['title'], {'english': 'Frieren'});
    });

    test('throws when the data line is missing', () {
      expect(
        () => parseMiruroRoot('{"type":"chunk","id":1,"data":[]}'),
        throwsFormatException,
      );
    });
  });

  group('parseRecommendations', () {
    test('maps AniList recommendations and drops empty slots', () {
      final body = jsonEncode({
        'data': {
          'Media': {
            'recommendations': {
              'nodes': [
                {
                  'mediaRecommendation': {
                    'id': 5114,
                    'title': {'english': 'Fullmetal Alchemist', 'romaji': ''},
                    'coverImage': {'extraLarge': 'https://img/fmab.jpg'},
                    'format': 'TV',
                    'episodes': 64,
                    'seasonYear': 2009,
                  },
                },
                {'mediaRecommendation': null},
                {
                  'mediaRecommendation': {
                    'id': 1,
                    'title': {'romaji': 'Cowboy Bebop'},
                    'coverImage': {'extraLarge': 'https://img/bebop.jpg'},
                  },
                },
              ],
            },
          },
        },
      });

      final items = parseRecommendations(body);

      expect(items, hasLength(2));
      expect(items.first.title, 'Fullmetal Alchemist');
      expect(items.first.aniListId, 5114);
      expect(items.first.episodes, 64);
      expect(items.last.title, 'Cowboy Bebop');
      expect(items.last.format, isNull);
    });

    test('returns empty when Media or nodes are missing', () {
      expect(parseRecommendations('{"data":null}'), isEmpty);
      expect(parseRecommendations('{"data":{"Media":null}}'), isEmpty);
    });
  });
}
