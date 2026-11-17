import 'dart:convert';
import 'dart:io';

import 'package:flutter_web_auth_2/flutter_web_auth_2.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'http_client.dart';

/// AniList application settings must register [aniListRedirectUrl] exactly.
/// The implicit grant needs no client secret (and a secret must never ship in
/// an APK).
const aniListClientId = '18615';
const aniListRedirectUrl = 'zanime://auth';
const _callbackScheme = 'zanime';
const _tokenKey = 'anilist_token';

class AniListProfile {
  const AniListProfile({
    required this.id,
    required this.name,
    required this.avatarUrl,
    this.bannerUrl,
    this.animeCount = 0,
    this.episodesWatched = 0,
    this.meanScore,
  });

  final int id;
  final String name;
  final String avatarUrl;
  final String? bannerUrl;
  final int animeCount;
  final int episodesWatched;
  final int? meanScore;
}

/// Extracts the JWT from the fragment (implicit grant) or query fallback.
String? parseAniListToken(String resultUrl) {
  final uri = Uri.tryParse(resultUrl);
  if (uri == null) return null;
  final params = uri.fragment.isNotEmpty
      ? Uri.splitQueryString(uri.fragment)
      : uri.queryParameters;
  final token = params['access_token']?.trim();
  return token == null || token.isEmpty ? null : token;
}

AniListProfile? parseAniListProfile(String body) {
  final root = jsonDecode(body) as Map<String, dynamic>;
  final viewer = (root['data'] as Map<String, dynamic>?)?['Viewer']
      as Map<String, dynamic>?;
  if (viewer == null) return null;
  final avatar = viewer['avatar'] as Map<String, dynamic>?;
  final stats =
      ((viewer['statistics'] as Map<String, dynamic>?)?['anime']
              as Map<String, dynamic>?) ??
          const {};
  return AniListProfile(
    id: (viewer['id'] as num?)?.toInt() ?? 0,
    name: (viewer['name'] as String?) ?? '',
    avatarUrl: (avatar?['large'] as String?) ?? '',
    bannerUrl: viewer['bannerImage'] as String?,
    animeCount: (stats['count'] as num?)?.toInt() ?? 0,
    episodesWatched: (stats['episodesWatched'] as num?)?.toInt() ?? 0,
    meanScore: (stats['meanScore'] as num?)?.toInt(),
  );
}

/// One anime on the user's CURRENT/REPEATING list.
class WatchingAnime {
  const WatchingAnime({
    required this.mediaId,
    required this.title,
    required this.imageUrl,
    required this.progress,
    this.totalEpisodes,
    this.nextEpisode,
    this.format,
    this.year,
  });

  final int mediaId;
  final String title;
  final String imageUrl;
  final int progress;
  final int? totalEpisodes;
  final int? nextEpisode;
  final String? format;
  final int? year;

  int get nextWatchEpisode => nextEpisode ?? (progress + 1);
}

/// Extracts entries from a MediaListCollection payload, newest update first.
List<WatchingAnime> parseAniListWatching(String body) {
  final root = jsonDecode(body) as Map<String, dynamic>;
  final collection = (root['data'] as Map<String, dynamic>?)?[
      'MediaListCollection'] as Map<String, dynamic>?;
  final lists = (collection?['lists'] as List? ?? const [])
      .cast<Map<String, dynamic>>();
  final seen = <int>{};
  final entries = <(int, WatchingAnime)>[];
  for (final list in lists) {
    for (final entry in (list['entries'] as List? ?? const [])
        .cast<Map<String, dynamic>>()) {
      final status = entry['status'] as String?;
      if (status != 'CURRENT' && status != 'REPEATING') continue;
      final media = entry['media'] as Map<String, dynamic>?;
      if (media == null) continue;
      final mediaId = (media['id'] as num?)?.toInt() ?? 0;
      if (mediaId == 0 || !seen.add(mediaId)) continue;
      final titles = media['title'] as Map<String, dynamic>? ?? const {};
      final cover = media['coverImage'] as Map<String, dynamic>? ?? const {};
      final next =
          (media['nextAiringEpisode'] as Map<String, dynamic>?)?['episode'];
      entries.add((
        (entry['updatedAt'] as num?)?.toInt() ?? 0,
        WatchingAnime(
          mediaId: mediaId,
          title: ((titles['english'] ?? titles['romaji']) as String?) ?? '',
          imageUrl: (cover['large'] as String?) ?? '',
          progress: (entry['progress'] as num?)?.toInt() ?? 0,
          totalEpisodes: (media['episodes'] as num?)?.toInt(),
          nextEpisode: (next as num?)?.toInt(),
          format: media['format'] as String?,
          year: (media['seasonYear'] as num?)?.toInt(),
        ),
      ));
    }
  }
  entries.sort((a, b) => b.$1.compareTo(a.$1));
  return [for (final entry in entries) entry.$2];
}

Uri aniListAuthorizeUrl() => Uri.https(
  'anilist.co',
  '/api/v2/oauth/authorize',
  {'client_id': aniListClientId, 'response_type': 'token'},
);

/// Opens the AniList consent page and stores the returned token.
Future<void> signInWithAniList() async {
  final result = await FlutterWebAuth2.authenticate(
    url: aniListAuthorizeUrl().toString(),
    callbackUrlScheme: _callbackScheme,
  );
  final token = parseAniListToken(result);
  if (token == null) {
    throw const FormatException('AniList tidak mengembalikan access token');
  }
  final prefs = await SharedPreferences.getInstance();
  await prefs.setString(_tokenKey, token);
}

Future<void> signOutAniList() async {
  final prefs = await SharedPreferences.getInstance();
  await prefs.remove(_tokenKey);
}

/// Profile of the stored token, or null when logged out / token expired.
Future<AniListProfile?> loadAniListProfile() async {
  try {
    final prefs = await SharedPreferences.getInstance();
    final token = prefs.getString(_tokenKey);
    if (token == null || token.isEmpty) return null;

    final response = await sharedHttpClient.post(
      Uri.parse('https://graphql.anilist.co'),
      headers: {
        'content-type': 'application/json',
        'authorization': 'Bearer $token',
      },
      body: jsonEncode({
        'query':
            'query { Viewer { id name avatar { large } bannerImage '
            'statistics { anime { count episodesWatched meanScore } } } }',
      }),
    );
    if (response.statusCode == 401) {
      await signOutAniList();
      return null;
    }
    if (response.statusCode != 200) return null;
    return parseAniListProfile(response.body);
  } catch (_) {
    return null;
  }
}

/// The user's CURRENT/REPEATING anime list, or null when not logged in.
Future<List<WatchingAnime>?> fetchAniListWatching() async {
  final prefs = await SharedPreferences.getInstance();
  final token = prefs.getString(_tokenKey);
  if (token == null || token.isEmpty) return null;

  final viewer = await _aniListQuery(token, 'query { Viewer { id } }');
  if (viewer == null) return null;
  final userId =
      (((viewer['data'] as Map<String, dynamic>?)?['Viewer']
                  as Map<String, dynamic>?)?['id']
              as num?)
          ?.toInt();
  if (userId == null) return const [];

  final collection = await _aniListQuery(
    token,
    'query (\$userId: Int!) { MediaListCollection(userId: \$userId, '
    'type: ANIME) { lists { entries { status progress updatedAt media { '
    'id title { romaji english } coverImage { large } episodes format '
    'seasonYear nextAiringEpisode { episode } } } } } }',
    {'userId': userId},
  );
  if (collection == null) return const [];
  return parseAniListWatching(jsonEncode(collection));
}

/// Returns the decoded GraphQL body; null when the token was rejected.
Future<Map<String, dynamic>?> _aniListQuery(
  String token,
  String query, [
  Map<String, dynamic>? variables,
]) async {
  final response = await sharedHttpClient.post(
    Uri.parse('https://graphql.anilist.co'),
    headers: {
      'content-type': 'application/json',
      'authorization': 'Bearer $token',
    },
    body: jsonEncode({'query': query, 'variables': ?variables}),
  );
  if (response.statusCode == 401) {
    await signOutAniList();
    return null;
  }
  if (response.statusCode != 200) {
    throw HttpException('AniList HTTP ${response.statusCode}');
  }
  return jsonDecode(response.body) as Map<String, dynamic>;
}
