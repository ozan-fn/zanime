import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:test1/services/anilist_auth.dart';

void main() {
  test('parseAniListToken reads the implicit grant fragment', () {
    expect(
      parseAniListToken(
        'zanime://auth#access_token=abc.def.ghi&token_type=Bearer&expires_in=31536000',
      ),
      'abc.def.ghi',
    );
  });

  test('parseAniListToken falls back to query parameters', () {
    expect(parseAniListToken('zanime://auth?access_token=xyz'), 'xyz');
  });

  test('parseAniListToken rejects a result without a token', () {
    expect(parseAniListToken('zanime://auth?error=denied'), isNull);
    expect(parseAniListToken('not a url'), isNull);
  });

  test('parseAniListProfile reads viewer and anime statistics', () {
    final profile = parseAniListProfile(
      jsonEncode({
        'data': {
          'Viewer': {
            'id': 18615,
            'name': 'zanime',
            'avatar': {'large': 'https://example.com/avatar.jpg'},
            'bannerImage': 'https://example.com/banner.jpg',
            'statistics': {
              'anime': {
                'count': 42,
                'episodesWatched': 510,
                'meanScore': 78,
              },
            },
          },
        },
      }),
    )!;
    expect(profile.id, 18615);
    expect(profile.name, 'zanime');
    expect(profile.avatarUrl, 'https://example.com/avatar.jpg');
    expect(profile.animeCount, 42);
    expect(profile.episodesWatched, 510);
    expect(profile.meanScore, 78);
  });

  test('parseAniListProfile tolerates missing statistics', () {
    final profile = parseAniListProfile(
      jsonEncode({
        'data': {
          'Viewer': {'id': 1, 'name': 'a', 'avatar': {'large': 'x'}},
        },
      }),
    )!;
    expect(profile.animeCount, 0);
    expect(profile.bannerUrl, isNull);
  });
}
