import 'dart:math';

import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:test1/services/list_service.dart';
import 'package:test1/services/mal_auth.dart';

const _profileJson =
    '{"id":42,"name":"maluser","picture":"https://example.com/a.jpg",'
    '"anime_statistics":{"num_items":7,"num_episodes_watched":99}}';

const _listsJson =
    '{"data":['
    '{"node":{"id":1,"title":"Frieren","main_picture":{"medium":"https://a"},'
    '"media_type":"tv","num_episodes":28,"start_season":{"year":2023}},'
    '"list_status":{"status":"watching","num_episodes_watched":3,'
    '"updated_at":"2024-01-02T00:00:00+00:00"}},'
    '{"node":{"id":2,"title":"Bocchi","main_picture":{"medium":"https://b"},'
    '"media_type":"tv","num_episodes":12,"start_season":{"year":2022}},'
    '"list_status":{"status":"completed","num_episodes_watched":12,'
    '"updated_at":"2024-02-02T00:00:00+00:00"}},'
    '{"node":{"id":3,"title":"Dungeon Meshi","main_picture":{"medium":"https://c"},'
    '"media_type":"tv","num_episodes":24,"start_season":{"year":2024}},'
    '"list_status":{"status":"plan_to_watch","num_episodes_watched":0,'
    '"updated_at":"2024-03-02T00:00:00+00:00"}},'
    '{"node":{"id":4,"title":"Rewatch","main_picture":{"medium":"https://d"},'
    '"media_type":"tv","num_episodes":12,"start_season":{"year":2020}},'
    '"list_status":{"status":"watching","is_rewatching":true,'
    '"num_episodes_watched":5,"updated_at":"2024-04-02T00:00:00+00:00"}}'
    ']}';

void main() {
  setUp(() {
    SharedPreferences.setMockInitialValues({});
  });

  test('parseMalAuthCode reads the code from the callback URL', () {
    expect(parseMalAuthCode('zanime://auth?code=abc123&state=x'), 'abc123');
    expect(parseMalAuthCode('zanime://auth?error=access_denied'), isNull);
    expect(parseMalAuthCode('not a url'), isNull);
  });

  test('malAuthorizeUrl carries credentials, redirect and PKCE', () {
    final url = malAuthorizeUrl(codeVerifier: 'verifier123');
    expect(url.host, 'myanimelist.net');
    expect(url.path, '/v1/oauth2/authorize');
    expect(url.queryParameters['client_id'], malClientId);
    expect(url.queryParameters['redirect_uri'], malRedirectUrl);
    expect(url.queryParameters['response_type'], 'code');
    expect(url.queryParameters['code_challenge'], 'verifier123');
    expect(url.queryParameters['code_challenge_method'], 'plain');
  });

  test('createMalCodeVerifier is URL-safe and stable with a seeded RNG', () {
    final first = createMalCodeVerifier();
    expect(first, isNotEmpty);
    expect(first, isNot(contains('=')));
    expect(first, isNot(contains('+')));

    final seededA = createMalCodeVerifier(_SeededRandom());
    final seededB = createMalCodeVerifier(_SeededRandom());
    expect(seededA, seededB);
  });

  test('parseMalProfile normalizes the MAL user payload', () {
    final profile = parseMalProfile(_profileJson);
    expect(profile, isNotNull);
    expect(profile!.id, 42);
    expect(profile.name, 'maluser');
    expect(profile.avatarUrl, 'https://example.com/a.jpg');
    expect(profile.animeCount, 7);
    expect(profile.episodesWatched, 99);
  });

  test('parseMalLists maps MAL statuses to the shared codes', () {
    final lists = parseMalLists(_listsJson);

    expect(lists['CURRENT']!.single.title, 'Frieren');
    expect(lists['CURRENT']!.single.progress, 3);
    expect(lists['CURRENT']!.single.nextWatchEpisode, 4);
    expect(lists['CURRENT']!.single.totalEpisodes, 28);
    expect(lists['CURRENT']!.single.format, 'TV');
    expect(lists['CURRENT']!.single.year, 2023);
    expect(lists['COMPLETED']!.single.title, 'Bocchi');
    expect(lists['PLANNING']!.single.title, 'Dungeon Meshi');
    expect(lists['REPEATING']!.single.title, 'Rewatch');
    expect(lists['REPEATING']!.single.totalEpisodes, 12);
  });

  test('malListFields requests anime fields at the top level', () {
    // `node{...}` is ignored by the list endpoint, which used to leave the
    // episode count, format and season empty.
    expect(malListFields, contains('num_episodes'));
    expect(malListFields, contains('media_type'));
    expect(malListFields, contains('start_season'));
    expect(malListFields, isNot(contains('node{')));
  });

  test('parseMalLists treats a zero episode count as unknown', () {
    final lists = parseMalLists(
      '{"data":[{"node":{"id":9,"title":"Airing","main_picture":{},'
      '"media_type":"tv","num_episodes":0,"start_season":{"year":2026}},'
      '"list_status":{"status":"completed","num_episodes_watched":5,'
      '"updated_at":"2024-01-02T00:00:00+00:00"}}]}',
    );

    final anime = lists['COMPLETED']!.single;
    expect(anime.totalEpisodes, isNull);
    expect(anime.format, 'TV');
    expect(anime.year, 2026);
  });

  test('MAL and AniList status codes round-trip', () {
    for (final entry in malStatusToAniListStatus.entries) {
      expect(aniListStatusToMalStatus[entry.value], entry.key);
    }
  });

  test('MAL status saves and deletes use the my_list_status endpoint', () {
    final uri = malStatusUri(30230);
    expect(uri.host, 'api.myanimelist.net');
    expect(uri.path, '/v2/anime/30230/my_list_status');
  });

  test('MAL status body clears rewatch unless REPEATING', () {
    expect(malStatusBody('CURRENT'), {
      'status': 'watching',
      'is_rewatching': 'false',
    });
    expect(malStatusBody('COMPLETED'), {
      'status': 'completed',
      'is_rewatching': 'false',
    });
    expect(malStatusBody('REPEATING'), {
      'status': 'watching',
      'is_rewatching': 'true',
    });
  });

  test('MAL title scoring normalizes punctuation, case and spaces', () {
    expect(malTitleMatchScore('Frieren!', 'frieren'), 1);
    expect(
      malTitleMatchScore('Delicious in Dungeon', 'Dungeon Meshi'),
      lessThan(malTitleMatchThreshold),
    );
    expect(
      malTitleMatchScore('Delicious in Dungeon', 'Delicious in Dungeon'),
      1,
    );
    expect(malTitleMatchScore('One Piece', 'Naruto'), 0);
  });

  test('pickMalAnimeId accepts a verified title or synonym match', () {
    const candidates = [
      {
        'node': {
          'id': 1,
          'title': 'Dungeon Meshi',
          'alternative_titles': {'en': 'Delicious in Dungeon'},
        },
      },
      {
        'node': {'id': 2, 'title': 'Totally Different Anime'},
      },
    ];

    expect(pickMalAnimeId('Delicious in Dungeon', candidates), 1);
    expect(pickMalAnimeId('dungeon   meshi!', candidates), 1);
  });

  test('pickMalAnimeId falls back to the closest similar title', () {
    const candidates = [
      {
        'node': {'id': 7, 'title': 'Overlord II'},
      },
      {
        'node': {'id': 9, 'title': 'Overlord'},
      },
    ];

    // "Overlord II" shares most words but is a different entry; the exact
    // "Overlord" must win.
    expect(pickMalAnimeId('Overlord', candidates), 9);
  });

  test('pickMalAnimeId returns null instead of guessing an unrelated hit', () {
    const candidates = [
      {
        'node': {'id': 3, 'title': 'Totally Different Anime'},
      },
      {
        'node': {'id': 4, 'title': 'Another Unrelated Show'},
      },
    ];

    expect(
      pickMalAnimeId('Frieren: Beyond Journey\'s End', candidates),
      isNull,
    );
  });

  test('resolveMalAnimeId prefers the exact AniList idMal mapping', () async {
    SharedPreferences.setMockInitialValues({});
    var calls = 0;
    Future<int?> lookup(int aniListId) async {
      calls++;
      return aniListId == 21 ? 313 : null;
    }

    expect(
      await resolveMalAnimeId('Some Anime', aniListId: 21, malIdLookup: lookup),
      313,
    );
    // The mapping is cached, so the lookup runs only once.
    expect(
      await resolveMalAnimeId('Some Anime', aniListId: 21, malIdLookup: lookup),
      313,
    );
    expect(calls, 1);
  });

  test('resolveMalAnimeId falls back to titles without an idMal', () async {
    SharedPreferences.setMockInitialValues({});

    // No mapping + no network in tests → null instead of a guessed id.
    expect(
      await resolveMalAnimeId(
        'Unknown Anime',
        aniListId: 22,
        malIdLookup: (_) async => null,
      ),
      isNull,
    );
  });

  test('the active list service is restored from storage', () async {
    expect(await loadActiveListService(), ListService.anilist);

    await setActiveListService(ListService.myanimelist);
    expect(await loadActiveListService(), ListService.myanimelist);

    await setActiveListService(ListService.anilist);
    expect(await loadActiveListService(), ListService.anilist);
  });
}

/// Deterministic Random for the verifier test.
class _SeededRandom implements Random {
  int _value = 1;

  @override
  bool nextBool() => nextInt(2) == 1;

  @override
  double nextDouble() => nextInt(1 << 32) / (1 << 32);

  @override
  int nextInt(int max) {
    _value = (_value * 1103515245 + 12345) & 0x7fffffff;
    return _value % max;
  }
}
