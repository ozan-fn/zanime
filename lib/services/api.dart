import 'dart:convert';

import 'package:http/http.dart' as http;

import '../models/models.dart';
import 'http_client.dart';
import 'sokuja.dart';

const _miruroBase = 'https://miruro.cx';
const _miruroHeaders = {
  'accept': '*/*',
  'referer': 'https://miruro.cx/',
  'user-agent':
      'Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 '
      '(KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36',
};

final http.Client _client = sharedHttpClient;

const _bannersQuery =
    'query(\$perPage: Int) { Page(page: 1, perPage: \$perPage) { '
    'media(type: ANIME, status: RELEASING, sort: TRENDING_DESC) { '
    'title { romaji english } bannerImage coverImage { extraLarge } '
    'genres seasonYear episodes averageScore nextAiringEpisode { episode } } } }';

Future<List<AnimeBannerData>> fetchAnimeBanners({int perPage = 10}) async {
  final response = await _client.post(
    Uri.parse('https://graphql.anilist.co'),
    headers: {'content-type': 'application/json'},
    body: jsonEncode({
      'query': _bannersQuery,
      'variables': {'perPage': perPage},
    }),
  );
  if (response.statusCode != 200) {
    throw http.ClientException('AniList HTTP ${response.statusCode}');
  }
  return parseAnimeBanners(response.body);
}

/// Parses the AniList GraphQL response returned by [fetchAnimeBanners].
List<AnimeBannerData> parseAnimeBanners(String body) {
  final media =
      ((jsonDecode(body) as Map<String, dynamic>)['data']
              as Map<String, dynamic>)['Page']['media']
          as List;
  return media.cast<Map<String, dynamic>>().map((json) {
    final cover = json['coverImage'] as Map<String, dynamic>?;
    final titles = json['title'] as Map<String, dynamic>;
    final next = json['nextAiringEpisode'] as Map<String, dynamic>?;
    final episodes = json['episodes'] as int?;
    return AnimeBannerData(
      title: (titles['english'] ?? titles['romaji']) as String,
      imageUrl:
          (json['bannerImage'] as String?) ?? (cover?['extraLarge'] as String),
      genres: (json['genres'] as List).cast<String>(),
      year: json['seasonYear'] as int?,
      episodes: episodes,
      aired: next != null ? (next['episode'] as int) - 1 : episodes,
      score: (json['averageScore'] as num?)?.toInt(),
    );
  }).toList();
}

const _recommendationsQuery =
    'query(\$id: Int) { Media(id: \$id, type: ANIME) { '
    'recommendations(sort: RATING_DESC, perPage: 12) { nodes { '
    'mediaRecommendation { id title { english romaji } '
    'coverImage { extraLarge } format episodes seasonYear averageScore } '
    '} } } }';

/// AniList community recommendations for [aniListId], best rated first.
Future<List<AnimeRecommendation>> fetchRecommendations(int aniListId) async {
  final response = await _client.post(
    Uri.parse('https://graphql.anilist.co'),
    headers: {'content-type': 'application/json'},
    body: jsonEncode({
      'query': _recommendationsQuery,
      'variables': {'id': aniListId},
    }),
  );
  if (response.statusCode != 200) {
    throw http.ClientException('AniList HTTP ${response.statusCode}');
  }
  return parseRecommendations(response.body);
}

/// Parses the GraphQL payload from [fetchRecommendations], dropping empty
/// recommendation slots.
List<AnimeRecommendation> parseRecommendations(String body) {
  final media =
      (jsonDecode(body) as Map<String, dynamic>)['data']?['Media']
          as Map<String, dynamic>?;
  final nodes =
      ((media?['recommendations'] as Map<String, dynamic>?)?['nodes'] as List?)
          ?.cast<Map<String, dynamic>>() ??
      const [];
  final items = <AnimeRecommendation>[];
  for (final node in nodes) {
    final item = node['mediaRecommendation'] as Map<String, dynamic>?;
    if (item == null) continue;
    final titles = item['title'] as Map<String, dynamic>? ?? const {};
    final cover = item['coverImage'] as Map<String, dynamic>? ?? const {};
    final id = (item['id'] as num?)?.toInt();
    final title = (titles['english'] ?? titles['romaji'] as String?) ?? '';
    final image = (cover['extraLarge'] as String?) ?? '';
    if (id == null || title.isEmpty || image.isEmpty) continue;
    items.add(
      AnimeRecommendation(
        aniListId: id,
        title: title,
        imageUrl: image,
        format: item['format'] as String?,
        year: item['seasonYear'] as int?,
        episodes: item['episodes'] as int?,
        score: (item['averageScore'] as num?)?.toInt(),
      ),
    );
  }
  return items;
}

const _relatedQuery =
    'query(\$id: Int) { Media(id: \$id, type: ANIME) { relations { edges { '
    'relationType media { id type title { english romaji native } '
    'coverImage { extraLarge } format episodes seasonYear averageScore } '
    '} } } }';

/// AniList relations (prequel, sequel, side story, ...) for [aniListId].
Future<List<RelatedAnime>> fetchRelated(int aniListId) async {
  final response = await _client.post(
    Uri.parse('https://graphql.anilist.co'),
    headers: {'content-type': 'application/json'},
    body: jsonEncode({
      'query': _relatedQuery,
      'variables': {'id': aniListId},
    }),
  );
  if (response.statusCode != 200) {
    throw http.ClientException('AniList HTTP ${response.statusCode}');
  }
  return parseRelated(response.body);
}

/// Parses the GraphQL payload from [fetchRelated], keeping anime only.
List<RelatedAnime> parseRelated(String body) {
  final media =
      (jsonDecode(body) as Map<String, dynamic>)['data']?['Media']
          as Map<String, dynamic>?;
  final edges =
      ((media?['relations'] as Map<String, dynamic>?)?['edges'] as List?)
          ?.cast<Map<String, dynamic>>() ??
      const [];
  final items = <RelatedAnime>[];
  for (final edge in edges) {
    final item = edge['media'] as Map<String, dynamic>?;
    if (item == null || item['type'] != 'ANIME') continue;
    final titles = item['title'] as Map<String, dynamic>? ?? const {};
    final cover = item['coverImage'] as Map<String, dynamic>? ?? const {};
    final id = (item['id'] as num?)?.toInt();
    final title = (titles['english'] ?? titles['romaji'] as String?) ?? '';
    final image = (cover['extraLarge'] as String?) ?? '';
    final relation = (edge['relationType'] as String?) ?? '';
    if (id == null || title.isEmpty || image.isEmpty || relation.isEmpty) {
      continue;
    }
    items.add(
      RelatedAnime(
        aniListId: id,
        title: title,
        imageUrl: image,
        relationType: relation,
        format: item['format'] as String?,
        year: item['seasonYear'] as int?,
        episodes: item['episodes'] as int?,
        score: (item['averageScore'] as num?)?.toInt(),
      ),
    );
  }
  return items;
}

/// Same array and order as the "Latest Episodes" list on miruro.cx.
Future<List<LatestEpisode>> fetchLatestEpisodes() async {
  final response = await _client.get(
    Uri.parse('$_miruroBase/'),
    headers: {..._miruroHeaders, 'accept': 'text/html'},
  );
  if (response.statusCode != 200) {
    throw http.ClientException('Miruro HTTP ${response.statusCode}');
  }
  return parseLatestEpisodes(response.body);
}

/// Parses the serialized `raw:{latest:[...]}` array out of the miruro home HTML.
/// shortcut: inline JS is minified; parses fields with regex and resolves
/// `title:w` / `episodeCounts:x` references, upgrade to a real API if it breaks.
List<LatestEpisode> parseLatestEpisodes(String html) {
  final start = html.indexOf('raw:{latest:[');
  final end = html.indexOf('],upcoming:', start);
  if (start < 0 || end < 0) {
    throw const FormatException('Latest Episodes array not found');
  }

  final scriptStart = html.lastIndexOf('<script', start);
  final scriptEnd = html.indexOf('</script>', start);
  final script = html.substring(
    scriptStart < 0 ? 0 : scriptStart,
    scriptEnd < 0 ? html.length : scriptEnd,
  );
  final english = <String, String>{};
  final romaji = <String, String>{};
  final sub = <String, int>{};
  final dub = <String, int>{};
  for (final m in RegExp(
    r'([A-Za-z0-9_$]+)\.english="((?:[^"\\]|\\.)*)"',
  ).allMatches(script)) {
    english[m[1]!] = _unescape(m[2]!);
  }
  for (final m in RegExp(
    r'([A-Za-z0-9_$]+)\.romaji="((?:[^"\\]|\\.)*)"',
  ).allMatches(script)) {
    romaji[m[1]!] = _unescape(m[2]!);
  }
  for (final m in RegExp(r'([A-Za-z0-9_$]+)\.sub=(\d+)').allMatches(script)) {
    sub[m[1]!] = int.parse(m[2]!);
  }
  for (final m in RegExp(r'([A-Za-z0-9_$]+)\.dub=(\d+)').allMatches(script)) {
    dub[m[1]!] = int.parse(m[2]!);
  }

  final segment = html.substring(start, end);
  return [
    for (final chunk in segment.split('{animeId:"').skip(1))
      ?_episode(chunk, english, romaji, sub, dub),
  ];
}

LatestEpisode? _episode(
  String chunk,
  Map<String, String> english,
  Map<String, String> romaji,
  Map<String, int> sub,
  Map<String, int> dub,
) {
  final animeId = RegExp(r'^([^"]+)').firstMatch(chunk)?[1];
  final episode = int.tryParse(
    RegExp(r'(?<![A-Za-z])episode:(\d+)').firstMatch(chunk)?[1] ?? '',
  );
  final cover = RegExp(r'coverUrl:"([^"]+)"').firstMatch(chunk)?[1];
  final airedAt = int.tryParse(
    RegExp(r'airingAt:(\d+)').firstMatch(chunk)?[1] ?? '',
  );
  final format = RegExp(r'format:"([^"]+)"').firstMatch(chunk)?[1];
  final year = int.tryParse(
    RegExp(r'seasonYear:(\d+)').firstMatch(chunk)?[1] ?? '',
  );
  final score = int.tryParse(
    RegExp(r'averageScore:(\d+)').firstMatch(chunk)?[1] ?? '',
  );
  final titleToken = RegExp(r'title:(\{[^{}]*\}|[A-Za-z0-9_$]+)')
      .firstMatch(chunk)?[1];
  final countToken = RegExp(r'episodeCounts:(\{[^{}]*\}|[A-Za-z0-9_$]+)')
      .firstMatch(chunk)?[1];
  if (animeId == null ||
      episode == null ||
      cover == null ||
      airedAt == null ||
      format == null ||
      year == null) {
    return null;
  }
  final titleEn = _field(titleToken, 'english', english);
  final titleRo = _field(titleToken, 'romaji', romaji);
  return LatestEpisode(
    animeId: animeId,
    title: titleEn ?? titleRo ?? animeId,
    romaji: titleRo ?? '',
    imageUrl: cover,
    episode: episode,
    airedAt: DateTime.fromMillisecondsSinceEpoch(airedAt * 1000, isUtc: true),
    format: format,
    year: year,
    subCount: _count(countToken, 'sub', sub),
    dubCount: _count(countToken, 'dub', dub),
    score: score,
  );
}

String? _field(String? token, String key, Map<String, String> refs) {
  if (token == null) return null;
  if (!token.startsWith('{')) return refs[token];
  final m = RegExp('$key:"((?:[^"\\\\]|\\\\.)*)"').firstMatch(token);
  return m == null ? null : _unescape(m[1]!);
}

int? _count(String? token, String field, Map<String, int> refs) {
  if (token == null) return null;
  if (!token.startsWith('{')) return refs[token];
  return int.tryParse(RegExp('$field:(\\d+)').firstMatch(token)?[1] ?? '');
}

String _unescape(String raw) {
  try {
    return jsonDecode('"$raw"') as String;
  } catch (_) {
    return raw;
  }
}

/// Search through the miruro search page's SvelteKit data.
/// (The `/api/search/browse` endpoint 403s for non-HTTP/2 clients.)
Future<List<SearchResult>> searchAnime(String query) async {
  final root = await _miruroRoot('/search/__data.json', {'q': query});
  return parseSearchResults(root);
}

/// Maps the `items` array of a SvelteKit search payload.
List<SearchResult> parseSearchResults(Map<String, dynamic> root) {
  return ((root['items'] as List?) ?? const [])
      .cast<Map<String, dynamic>>()
      .map((item) {
        final title = item['title'] as Map<String, dynamic>? ?? const {};
        return SearchResult(
          animeId: item['id'] as String,
          title: (title['english'] ?? title['romaji'] as String?) ?? '',
          imageUrl: (item['cover_url'] as String?) ?? '',
          format: item['format'] as String?,
          year: item['season_year'] as int?,
          episodes: item['episode_count'] as int?,
          score: item['average_score'] as int?,
          aniListId: _aniListId(item['external_ids']),
        );
      })
      .toList();
}

/// Miruro stores external ids as lists (`anilist:[21]`); older payloads use a
/// plain number.
int? _aniListId(Object? externalIds) {
  final ids = (externalIds as Map?)?['anilist'];
  if (ids is num) return ids.toInt();
  if (ids is List && ids.isNotEmpty) return int.tryParse('${ids.first}');
  return null;
}

/// Miruro ids are opaque hashes, so an AniList entry is resolved by title and
/// matched on its AniList id. Null when Miruro does not list the anime.
Future<String?> resolveMiruroAnimeId(String title, int aniListId) async =>
    pickMiruroAnimeId(await searchAnime(title), aniListId);

String? pickMiruroAnimeId(List<SearchResult> results, int aniListId) {
  for (final result in results) {
    if (result.aniListId == aniListId) return result.animeId;
  }
  return null;
}

String _normalizeTitle(String value) =>
    value.toLowerCase().replaceAll(RegExp('[^a-z0-9]+'), ' ').trim();

/// Resolves by title only, for services whose ids differ from AniList (MAL).
Future<String?> resolveMiruroAnimeIdByTitle(String title) async {
  final results = await searchAnime(title);
  if (results.isEmpty) return null;
  final wanted = _normalizeTitle(title);
  for (final result in results) {
    if (_normalizeTitle(result.title) == wanted) return result.animeId;
  }
  return results.first.animeId;
}

/// Watch page data for one episode: anime info, full episode list and servers.
Future<WatchData> fetchWatchData(String animeId, {int episode = 1}) async {
  final root = await _miruroRoot('/watch/$animeId/x/__data.json', {
    'ep': '$episode',
  });
  final data = parseWatchData(root, episode);
  try {
    final titles =
        (root['media'] as Map<String, dynamic>)['title']
            as Map<String, dynamic>;
    final romaji = (titles['romaji'] as String?)?.trim();
    final server = await fetchSokujaServer(
      data.anime,
      episode,
      altTitle: romaji == null || romaji == data.anime.title ? null : romaji,
    );
    if (server != null) data.servers.add(server);
  } catch (_) {
    // Miruro streams still play when sokuja is unreachable.
  }
  return data;
}

/// Maps one SvelteKit watch payload to [WatchData].
WatchData parseWatchData(Map<String, dynamic> root, int episode) {
  final media = root['media'] as Map<String, dynamic>;
  final titles = media['title'] as Map<String, dynamic>;
  final native = (titles['native'] as String?)?.trim();
  final romaji = (titles['romaji'] as String?)?.trim();
  final cover = media['coverImage'] as Map<String, dynamic>;
  final studios =
      ((media['studios'] as Map<String, dynamic>?)?['edges'] as List? ??
              const [])
          .cast<Map<String, dynamic>>()
          .where((edge) => edge['isMain'] == true)
          .map(
            (edge) => (edge['node'] as Map<String, dynamic>)['name'] as String,
          )
          .toList();

  final episodesData = root['episodesData'] as Map<String, dynamic>;
  final episodes = (episodesData['episodes'] as List)
      .cast<Map<String, dynamic>>()
      .map(WatchEpisode.fromJson)
      .toList();

  final sources = root['sourcesData'] as Map<String, dynamic>;
  final tracks = (sources['tracks'] as List).cast<Map<String, dynamic>>();
  Map<String, dynamic>? track;
  for (final name in const ['ssub', 'sub']) {
    for (final candidate in tracks) {
      if (candidate['track'] == name) {
        track = candidate;
        break;
      }
    }
    if (track != null) break;
  }
  track ??= tracks.isEmpty ? null : tracks.first;
  final servers = <WatchServer>[];
  // Every track from every provider is listed; the provider id (bee, sum,
  // ...) is appended so the user knows where the subtitle comes from.
  final subtitles = <WatchSubtitle>[];
  var subtitleCount = 0;
  for (final provider
      in ((track?['providers'] as List?) ?? const [])
          .cast<Map<String, dynamic>>()) {
    final providerId = ((provider['provider'] as String?) ?? '').trim();
    // Subtitle files live on the provider's CDN, which rejects requests sent
    // with another provider's Origin/Referer. The headers sit on the servers,
    // not on the subtitle entries, so copy them onto every track below.
    var providerHeaders = <String, String>{};
    for (final server
        in ((provider['servers'] as List?) ?? const [])
            .cast<Map<String, dynamic>>()) {
      final headers = ((server['headers'] as Map?) ?? const {})
          .cast<String, String>();
      if (headers.isNotEmpty) {
        providerHeaders = headers;
        break;
      }
    }
    for (final item
        in ((provider['subtitles'] as List?) ?? const [])
            .cast<Map<String, dynamic>>()) {
      final url = ((item['file'] as String?) ?? '').trim();
      if (url.isEmpty) continue;
      var label = ((item['label'] as String?) ?? '').trim();
      // The picker must never show a blank row.
      if (label.isEmpty) label = 'Subtitle ${++subtitleCount}';
      final tagged = providerId.isEmpty ? label : '$label · $providerId';
      final key = url.toLowerCase();
      final index = subtitles.indexWhere(
        (subtitle) => subtitle.url.toLowerCase() == key,
      );
      if (index == -1) {
        subtitles.add(
          WatchSubtitle(
            label: tagged,
            url: url,
            isDefault: item['default'] == true,
            headers: providerHeaders,
          ),
        );
      } else if (item['default'] == true) {
        // Same file offered by another provider, marked default there.
        subtitles[index] = WatchSubtitle(
          label: subtitles[index].label,
          url: subtitles[index].url,
          isDefault: true,
          headers: subtitles[index].headers.isNotEmpty
              ? subtitles[index].headers
              : providerHeaders,
        );
      }
    }
    final serversData = (provider['servers'] as List? ?? const [])
        .cast<Map<String, dynamic>>();
    for (final server in serversData) {
      final rawStreams = (server['streams'] as List? ?? const [])
          .cast<Map<String, dynamic>>();
      final streams = [
        for (var i = 0; i < rawStreams.length; i++)
          if ((rawStreams[i]['url'] as String?)?.isNotEmpty ?? false)
            WatchStream.fromJson(rawStreams[i], i),
      ];
      if (streams.isEmpty) continue;
      servers.add(
        WatchServer(
          provider: provider['provider'] as String,
          streams: streams,
          headers: ((server['headers'] as Map?) ?? const {})
              .cast<String, String>(),
        ),
      );
      break;
    }
  }

  return WatchData(
    anime: WatchAnime(
      title: (titles['english'] ?? titles['romaji']) as String,
      imageUrl: cover['extraLarge'] as String,
      description: media['description'] as String,
      format: media['format'] as String,
      status: media['status'] as String,
      season: media['season'] as String,
      genres: (media['genres'] as List).cast<String>(),
      studios: studios,
      year: media['seasonYear'] as int?,
      episodes: media['episodes'] as int?,
      duration: media['duration'] as int?,
      score: media['averageScore'] as int?,
      japaneseTitle: (native?.isNotEmpty ?? false) ? native! : (romaji ?? ''),
    ),
    episodes: episodes,
    servers: servers,
    subtitles: subtitles,
    currentEpisode:
        ((episodesData['currentEpisode'] as Map<String, dynamic>?)?['number']
            as int?) ??
        episode,
    aniListId: (root['anilistId'] as num?)?.toInt(),
  );
}

Future<Map<String, dynamic>> _miruroRoot(
  String path,
  Map<String, String> query,
) async {
  var uri = Uri.parse('$_miruroBase$path').replace(queryParameters: query);
  var response = await _client.get(uri, headers: _miruroHeaders);
  if (response.statusCode != 200) {
    throw http.ClientException('Miruro HTTP ${response.statusCode}', uri);
  }

  // SvelteKit signals slug redirects in the JSON body, not with HTTP 3xx.
  final first = response.body.split('\n').first;
  final header = jsonDecode(first);
  if (header is Map && header['type'] == 'redirect') {
    final location = Uri.parse(header['location'] as String);
    final nextQuery = location.hasQuery ? location.queryParameters : query;
    uri = Uri.parse('$_miruroBase${location.path}/__data.json')
        .replace(queryParameters: nextQuery);
    response = await _client.get(uri, headers: _miruroHeaders);
    if (response.statusCode != 200) {
      throw http.ClientException('Miruro HTTP ${response.statusCode}', uri);
    }
  }

  return parseMiruroRoot(response.body);
}

/// Resolves SvelteKit's line-delimited devalue payload into the page `data`.
Map<String, dynamic> parseMiruroRoot(String body) {
  return _Devalue.parse(body).root(1) as Map<String, dynamic>;
}

class _Devalue {
  final List<Map<String, dynamic>> _nodes;
  final Map<int, List<dynamic>> _chunks;
  final Map<int, dynamic> _chunkRoots = {};

  factory _Devalue.parse(String body) {
    List<Map<String, dynamic>>? nodes;
    final chunks = <int, List<dynamic>>{};
    for (final line in body.split('\n')) {
      if (line.trim().isEmpty) continue;
      final object = jsonDecode(line) as Map<String, dynamic>;
      if (object['type'] == 'data') {
        nodes = (object['nodes'] as List).cast<Map<String, dynamic>>();
      } else if (object['type'] == 'chunk') {
        chunks[object['id'] as int] = object['data'] as List;
      }
    }
    if (nodes == null) throw const FormatException('SvelteKit data missing');
    return _Devalue._(nodes, chunks);
  }

  _Devalue._(this._nodes, this._chunks);

  dynamic root(int node) {
    final data = _nodes[node]['data'] as List;
    return _resolve(data, 0, <int, dynamic>{});
  }

  dynamic _resolve(List<dynamic> data, dynamic value, Map<int, dynamic> cache) {
    if (value is! int) return _deep(data, value, cache);
    if (value < 0) return null;
    if (cache.containsKey(value)) return cache[value];
    final resolved = _deep(data, data[value], cache);
    cache[value] = resolved;
    return resolved;
  }

  dynamic _deep(List<dynamic> data, dynamic value, Map<int, dynamic> cache) {
    if (value is Map) {
      return <String, dynamic>{
        for (final entry in value.entries)
          entry.key as String: _resolve(data, entry.value, cache),
      };
    }
    if (value is List) {
      if (value.length == 2 && value[0] == 'Promise') {
        // The id is itself a reference into the current data array.
        final id = _resolve(data, value[1], cache) as int;
        return _chunkRoots.putIfAbsent(id, () {
          final chunk = _chunks[id]!;
          return _resolve(chunk, 0, <int, dynamic>{});
        });
      }
      return [for (final entry in value) _resolve(data, entry, cache)];
    }
    return value;
  }
}
