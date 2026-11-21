import 'dart:convert';
import 'dart:io';
import 'dart:math';

import 'package:flutter_web_auth_2/flutter_web_auth_2.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'anilist_auth.dart';
import 'http_client.dart';

/// MyAnimeList OAuth2 application credentials (hardcoded by request).
const malClientId = '9de8db2034d559e70de97b61528a9692';
const malClientSecret =
    '7d02ba71ee6e6c128d7067106cee0dae948223ce933cb08f09844bb544e59235';

/// MAL apps must register this redirect URI exactly.
const malRedirectUrl = 'zanime://auth';
const _callbackScheme = 'zanime';

const _tokenKey = 'mal_token';
const _refreshKey = 'mal_refresh_token';
const _expiryKey = 'mal_expiry_ms';
// v2: older caches also stored first-guess ids that were never verified.
const _idCachePrefix = 'mal_anime_id_v2_';

/// Minimum title similarity (Dice on word sets) to accept a MAL search hit.
const malTitleMatchThreshold = 0.55;

/// Dice coefficient of the normalized word sets of two titles.
double malTitleMatchScore(String a, String b) {
  Set<String> tokens(String value) =>
      _normalizeTitle(value)
          .split(RegExp(r'\s+'))
          .where((token) => token.isNotEmpty)
          .toSet();
  final left = tokens(a);
  final right = tokens(b);
  if (left.isEmpty || right.isEmpty) return 0;
  if (left.length == right.length && left.containsAll(right)) return 1;
  final overlap = left.intersection(right).length;
  return 2 * overlap / (left.length + right.length);
}

/// Extracts `code` from the OAuth callback URL.
String? parseMalAuthCode(String resultUrl) {
  final uri = Uri.tryParse(resultUrl);
  if (uri == null) return null;
  final code = uri.queryParameters['code']?.trim();
  return code == null || code.isEmpty ? null : code;
}

/// PKCE verifier (plain challenge) for the authorization request.
String createMalCodeVerifier([Random? random]) {
  final rng = random ?? Random.secure();
  final bytes = List<int>.generate(32, (_) => rng.nextInt(256));
  return base64Url.encode(bytes).replaceAll('=', '');
}

Uri malAuthorizeUrl({required String codeVerifier}) =>
    Uri.https('myanimelist.net', '/v1/oauth2/authorize', {
      'response_type': 'code',
      'client_id': malClientId,
      'redirect_uri': malRedirectUrl,
      'code_challenge': codeVerifier,
      'code_challenge_method': 'plain',
    });

/// Opens the MAL consent page and stores the resulting tokens.
Future<void> signInWithMal() async {
  final verifier = createMalCodeVerifier();
  final result = await FlutterWebAuth2.authenticate(
    url: malAuthorizeUrl(codeVerifier: verifier).toString(),
    callbackUrlScheme: _callbackScheme,
  );
  final code = parseMalAuthCode(result);
  if (code == null) {
    throw const FormatException('MyAnimeList tidak mengembalikan kode OAuth');
  }
  final response = await sharedHttpClient.post(
    Uri.parse('https://myanimelist.net/v1/oauth2/token'),
    headers: {'content-type': 'application/x-www-form-urlencoded'},
    body: {
      'client_id': malClientId,
      'client_secret': malClientSecret,
      'grant_type': 'authorization_code',
      'code': code,
      'redirect_uri': malRedirectUrl,
      'code_verifier': verifier,
    },
  );
  if (response.statusCode != 200) {
    throw HttpException('MAL token HTTP ${response.statusCode}');
  }
  await _storeToken(jsonDecode(response.body) as Map<String, dynamic>);
}

Future<void> signOutMal() async {
  final prefs = await SharedPreferences.getInstance();
  await prefs.remove(_tokenKey);
  await prefs.remove(_refreshKey);
  await prefs.remove(_expiryKey);
}

Future<void> _storeToken(Map<String, dynamic> body) async {
  final prefs = await SharedPreferences.getInstance();
  final access = body['access_token'] as String?;
  if (access == null || access.isEmpty) {
    throw const FormatException('MAL tidak mengembalikan access token');
  }
  await prefs.setString(_tokenKey, access);
  final refresh = body['refresh_token'] as String?;
  if (refresh != null && refresh.isNotEmpty) {
    await prefs.setString(_refreshKey, refresh);
  }
  final expiresIn = (body['expires_in'] as num?)?.toInt();
  if (expiresIn != null) {
    final expiry = DateTime.now().add(Duration(seconds: expiresIn));
    await prefs.setInt(_expiryKey, expiry.millisecondsSinceEpoch);
  }
}

/// Stored access token; refreshes it when it expired.
Future<String?> malAccessToken() async {
  final prefs = await SharedPreferences.getInstance();
  final token = prefs.getString(_tokenKey);
  final expiry = prefs.getInt(_expiryKey);
  final stillValid =
      token != null &&
      token.isNotEmpty &&
      (expiry == null ||
          DateTime.now().millisecondsSinceEpoch < expiry - 60000);
  if (stillValid) return token;

  final refresh = prefs.getString(_refreshKey);
  if (refresh == null || refresh.isEmpty) return token;
  try {
    final response = await sharedHttpClient.post(
      Uri.parse('https://myanimelist.net/v1/oauth2/token'),
      headers: {'content-type': 'application/x-www-form-urlencoded'},
      body: {
        'client_id': malClientId,
        'client_secret': malClientSecret,
        'grant_type': 'refresh_token',
        'refresh_token': refresh,
      },
    );
    if (response.statusCode != 200) return null;
    await _storeToken(jsonDecode(response.body) as Map<String, dynamic>);
    return (await SharedPreferences.getInstance()).getString(_tokenKey);
  } catch (_) {
    return null;
  }
}

/// Normalizes a MAL user payload into the shared profile model.
AniListProfile? parseMalProfile(String body) {
  final root = jsonDecode(body) as Map<String, dynamic>;
  final name = root['name'] as String?;
  if (name == null || name.isEmpty) return null;
  final stats = (root['anime_statistics'] as Map<String, dynamic>?) ?? const {};
  return AniListProfile(
    id: (root['id'] as num?)?.toInt() ?? 0,
    name: name,
    avatarUrl: (root['picture'] as String?) ?? '',
    animeCount: (stats['num_items'] as num?)?.toInt() ?? 0,
    episodesWatched: (stats['num_episodes_watched'] as num?)?.toInt() ?? 0,
  );
}

/// Profile of the stored MAL token, or null when logged out.
Future<AniListProfile?> loadMalProfile() async {
  try {
    final root = await _malGet('/v2/users/@me', {'fields': 'anime_statistics'});
    if (root == null) return null;
    return parseMalProfile(jsonEncode(root));
  } catch (_) {
    return null;
  }
}

/// MAL list status <-> the AniList-style codes used by the UI.
const malStatusToAniListStatus = {
  'watching': 'CURRENT',
  'completed': 'COMPLETED',
  'on_hold': 'PAUSED',
  'dropped': 'DROPPED',
  'plan_to_watch': 'PLANNING',
};

const aniListStatusToMalStatus = {
  'CURRENT': 'watching',
  'COMPLETED': 'completed',
  'PAUSED': 'on_hold',
  'DROPPED': 'dropped',
  'PLANNING': 'plan_to_watch',
  'REPEATING': 'watching',
};

/// MAL list fields. The list endpoint takes anime fields at the top level;
/// wrapping them in `node{...}` is ignored by the API and leaves episodes,
/// format and season empty.
const malListFields =
    'id,title,main_picture,media_type,start_season,num_episodes,mean,'
    'list_status{status,score,num_episodes_watched,updated_at,is_rewatching}';

/// MAL media types renamed to the AniList format codes used by the UI.
const _malFormatToAniListFormat = {
  'tv': 'TV',
  'movie': 'MOVIE',
  'ova': 'OVA',
  'ona': 'ONA',
  'special': 'SPECIAL',
  'tv_special': 'TV_SHORT',
  'music': 'MUSIC',
  'pv': 'PV',
  'cm': 'CM',
};

/// Parses one MAL animelist payload into entries grouped by status.
Map<String, List<WatchingAnime>> parseMalLists(String body) {
  final root = jsonDecode(body) as Map<String, dynamic>;
  final grouped = <String, List<WatchingAnime>>{};
  for (final item
      in ((root['data'] as List?) ?? const []).cast<Map<String, dynamic>>()) {
    final node = item['node'] as Map<String, dynamic>?;
    final listStatus = item['list_status'] as Map<String, dynamic>?;
    if (node == null || listStatus == null) continue;
    final rawStatus = listStatus['status'] as String?;
    final status = malStatusToAniListStatus[rawStatus];
    if (status == null) continue;
    final rewatching = listStatus['is_rewatching'] == true;
    final mapped = rewatching && status == 'CURRENT' ? 'REPEATING' : status;
    final picture = node['main_picture'] as Map<String, dynamic>?;
    final season = node['start_season'] as Map<String, dynamic>?;
    final episodes = (node['num_episodes'] as num?)?.toInt();
    // MAL `mean` is 0–10; the UI uses AniList's 0–100 scale everywhere.
    final mean = (node['mean'] as num?)?.toDouble();
    final updated =
        DateTime.tryParse((listStatus['updated_at'] as String?) ?? '')
            ?.millisecondsSinceEpoch ??
        0;
    grouped
        .putIfAbsent(mapped, () => [])
        .add(
          WatchingAnime(
            mediaId: (node['id'] as num?)?.toInt() ?? 0,
            title: (node['title'] as String?) ?? '',
            imageUrl:
                (picture?['large'] ?? picture?['medium']) as String? ?? '',
            progress:
                (listStatus['num_episodes_watched'] as num?)?.toInt() ?? 0,
            // MAL reports 0 when the episode count is unknown; AniList uses
            // null, so the UI shows "?" in both services.
            totalEpisodes: episodes == null || episodes <= 0 ? null : episodes,
            format:
                _malFormatToAniListFormat[node['media_type'] as String?] ??
                node['media_type'] as String?,
            year: (season?['year'] as num?)?.toInt(),
            status: mapped,
            updatedAt: updated,
            score: mean == null ? null : (mean * 10).round(),
          ),
        );
  }
  for (final list in grouped.values) {
    list.sort((a, b) => b.updatedAt.compareTo(a.updatedAt));
  }
  return grouped;
}

Future<Map<String, List<WatchingAnime>>?> fetchMalLists() async {
  try {
    final grouped = <String, List<WatchingAnime>>{};
    var next = Uri.https('api.myanimelist.net', '/v2/users/@me/animelist', {
      'fields': malListFields,
      'limit': '1000',
      'nsfw': 'true',
    });
    do {
      final root = await _malGetUri(next);
      if (root == null) return null;
      final page = parseMalLists(jsonEncode(root));
      for (final entry in page.entries) {
        grouped.putIfAbsent(entry.key, () => []).addAll(entry.value);
      }
      final nextUrl =
          ((root['paging'] as Map<String, dynamic>?)?['next'] as String?)
              ?.trim();
      next = nextUrl == null || nextUrl.isEmpty ? Uri() : Uri.parse(nextUrl);
    } while (next.hasScheme);
    for (final list in grouped.values) {
      list.sort((a, b) => b.updatedAt.compareTo(a.updatedAt));
    }
    return grouped;
  } catch (_) {
    return null;
  }
}

/// The user's watching/rewatching MAL entries.
Future<List<WatchingAnime>?> fetchMalWatching() async {
  final lists = await fetchMalLists();
  if (lists == null) return null;
  return [...?lists['CURRENT'], ...?lists['REPEATING']]
    ..sort((a, b) => b.updatedAt.compareTo(a.updatedAt));
}

String _normalizeTitle(String value) =>
    value.toLowerCase().replaceAll(RegExp('[^a-z0-9]+'), ' ').trim();

/// All title strings of a MAL search node (main + alternative titles).
List<String> malNodeTitles(Map<String, dynamic> node) {
  final titles = <String>[(node['title'] as String?) ?? ''];
  final alternatives = node['alternative_titles'] as Map?;
  if (alternatives != null) {
    for (final value in alternatives.values) {
      if (value is String) {
        titles.add(value);
      } else if (value is List) {
        titles.addAll(value.whereType<String>());
      }
    }
  }
  return titles;
}

/// Picks the MAL id whose title best matches [title], or null when no
/// candidate is close enough. Never guesses: an unrelated first hit returns
/// null instead of pointing the status at the wrong MAL entry.
int? pickMalAnimeId(String title, Iterable<Map<String, dynamic>> candidates) {
  int? best;
  var bestScore = 0.0;
  for (final item in candidates) {
    final node = item['node'] as Map<String, dynamic>?;
    final id = (node?['id'] as num?)?.toInt();
    if (node == null || id == null) continue;
    for (final candidate in malNodeTitles(node)) {
      final score = malTitleMatchScore(title, candidate);
      // An exact normalized match wins immediately.
      if (score >= 1) return id;
      if (score > bestScore) {
        best = id;
        bestScore = score;
      }
    }
  }
  if (best == null || bestScore < malTitleMatchThreshold) return null;
  return best;
}

/// Looks up the MAL id from the AniList id; injectable in tests.
typedef MalIdLookup = Future<int?> Function(int aniListId);

/// Finds the MAL anime id for a Miruro title, cached by AniList id/title.
///
/// AniList exposes the exact `idMal` mapping, so that is tried first. Only
/// when it is missing does the title search run — and even then just a
/// verified match is cached; the first search hit is never guessed, so a
/// status is never written to the wrong MAL entry.
Future<int?> resolveMalAnimeId(
  String title, {
  int? aniListId,
  MalIdLookup? malIdLookup,
}) async {
  final prefs = await SharedPreferences.getInstance();
  final cacheKey = aniListId != null
      ? '$_idCachePrefix$aniListId'
      : '$_idCachePrefix${_normalizeTitle(title)}';
  final cached = prefs.getInt(cacheKey);
  if (cached != null && cached > 0) return cached;

  // Exact mapping from AniList — no title matching needed.
  if (aniListId != null) {
    final malId = await (malIdLookup ?? fetchAniListMalId)(aniListId);
    if (malId != null && malId > 0) {
      await prefs.setInt(cacheKey, malId);
      return malId;
    }
  }

  final Map<String, dynamic>? root;
  try {
    root = await _malGet('/v2/anime', {
      'q': title,
      'limit': '10',
      'fields': 'id,title,alternative_titles',
      'nsfw': 'true',
    });
  } catch (_) {
    return null;
  }
  if (root == null) return null;
  final candidates = ((root['data'] as List?) ?? const [])
      .cast<Map<String, dynamic>>();
  final best = pickMalAnimeId(title, candidates);
  if (best != null) await prefs.setInt(cacheKey, best);
  return best;
}

/// The viewer's MAL list status for [animeId] (AniList-style code).
Future<String?> fetchMalStatus(int animeId) async {
  try {
    final root = await _malGet('/v2/anime/$animeId', {
      'fields': 'my_list_status',
    });
    final listStatus = root?['my_list_status'] as Map<String, dynamic>?;
    if (listStatus == null) return null;
    final mapped = malStatusToAniListStatus[listStatus['status'] as String?];
    if (mapped == 'CURRENT' && listStatus['is_rewatching'] == true) {
      return 'REPEATING';
    }
    return mapped;
  } catch (_) {
    return null;
  }
}

/// Endpoint that creates, updates or deletes one MAL list entry.
Uri malStatusUri(int animeId) =>
    Uri.https('api.myanimelist.net', '/v2/anime/$animeId/my_list_status');

/// Form body for [saveMalEntry]; rewatch is only set for REPEATING.
Map<String, String> malStatusBody(String status) => {
  'status': aniListStatusToMalStatus[status]!,
  'is_rewatching': status == 'REPEATING' ? 'true' : 'false',
};

/// Creates/updates the MAL list entry with an AniList-style [status].
Future<bool> saveMalEntry(int animeId, String status) async {
  final malStatus = aniListStatusToMalStatus[status];
  if (malStatus == null) return false;
  try {
    final response = await _malSend(
      'PATCH',
      malStatusUri(animeId),
      body: malStatusBody(status),
    );
    return response?.statusCode == 200;
  } catch (_) {
    return false;
  }
}

/// Removes the MAL list entry. Returns false when it was not on the list.
Future<bool> removeMalEntry(int animeId) async {
  try {
    final response = await _malSend('DELETE', malStatusUri(animeId));
    return response?.statusCode == 200;
  } catch (_) {
    return false;
  }
}

Future<Map<String, dynamic>?> _malGet(
  String path, [
  Map<String, String>? query,
]) {
  return _malGetUri(Uri.https('api.myanimelist.net', path, query));
}

Future<Map<String, dynamic>?> _malGetUri(Uri uri) async {
  final token = await malAccessToken();
  if (token == null) return null;
  final response = await sharedHttpClient.get(
    uri,
    headers: {'authorization': 'Bearer $token', 'X-MAL-CLIENT-ID': malClientId},
  );
  if (response.statusCode == 401) {
    await signOutMal();
    return null;
  }
  if (response.statusCode != 200) {
    throw HttpException('MAL HTTP ${response.statusCode}', uri: uri);
  }
  return jsonDecode(response.body) as Map<String, dynamic>;
}

Future<ApiResponse?> _malSend(
  String method,
  Uri uri, {
  Map<String, String>? body,
}) async {
  final token = await malAccessToken();
  if (token == null) return null;
  final response = await sharedHttpClient.send(
    method,
    uri,
    headers: {
      'authorization': 'Bearer $token',
      'X-MAL-CLIENT-ID': malClientId,
      if (body != null) 'content-type': 'application/x-www-form-urlencoded',
    },
    body: body,
  );
  if (response.statusCode == 401) await signOutMal();
  return response;
}
