import 'dart:convert';

import '../models/models.dart';
import 'http_client.dart';

const sokujaBase = 'https://x6.sokuja.uk';
const sokujaProvider = 'sokuja';

const _headers = {
  'accept': '*/*',
  'referer': '$sokujaBase/',
  'user-agent':
      'Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 '
      '(KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36',
};

class SokujaCandidate {
  const SokujaCandidate({
    required this.slug,
    required this.title,
    this.year,
    this.type,
  });

  final String slug;
  final String title;
  final int? year;
  final String? type;
}

class SokujaEpisode {
  const SokujaEpisode({
    required this.id,
    required this.slug,
    required this.number,
  });

  final int id;
  final String slug;
  final int number;
}

class SokujaDetail {
  const SokujaDetail({
    required this.title,
    required this.episodes,
    this.year,
    this.type,
    this.studios = const [],
    this.genres = const [],
    this.synopsis = '',
  });

  final String title;
  final int? year;
  final String? type;
  final List<String> studios;
  final List<String> genres;
  final String synopsis;
  final List<SokujaEpisode> episodes;
}

/// Parses `/api/search/?q=`.
List<SokujaCandidate> parseSokujaSearch(String body) {
  final root = jsonDecode(body) as Map<String, dynamic>;
  return [
    for (final item
        in (root['results'] as List? ?? const []).cast<Map<String, dynamic>>())
      if ((item['slug'] as String?)?.isNotEmpty ?? false)
        SokujaCandidate(
          slug: item['slug'] as String,
          title: (item['title'] as String? ?? '').trim(),
          year: item['year'] as int?,
          type: item['type'] as String?,
        ),
  ];
}

/// Parses an anime detail page. The episode list only exists inside Next's
/// base64 RSC payload, not in the rendered HTML.
SokujaDetail? parseSokujaDetail(String html) {
  final title = _text(
    _first(html, RegExp(r'<h1[^>]*>(.*?)</h1>', dotAll: true)),
  ).replaceFirst(RegExp(r'\s*Subtitle Indonesia$'), '').trim();
  if (title.isEmpty) return null;

  final decoded = _decodeRsc(html);
  final episodes = [
    for (final match in RegExp(
      r'"id":(\d+),"slug":"([^"]+)","title":".*?","episodeNumber":(\d+)',
    ).allMatches(decoded))
      SokujaEpisode(
        id: int.parse(match.group(1)!),
        slug: match.group(2)!,
        number: int.parse(match.group(3)!),
      ),
  ];
  if (episodes.isEmpty) return null;

  final studioBlock = _first(
    html,
    RegExp(r'>Studio</dt><dd[^>]*>(.*?)</dd>', dotAll: true),
  );
  final synopsisStart = html.indexOf('Sinopsis');
  final synopsis = synopsisStart < 0
      ? ''
      : _text(
          _first(
            html.substring(synopsisStart),
            RegExp(r'<p>(.*?)</p>', dotAll: true),
          ),
        );

  return SokujaDetail(
    title: title,
    year: int.tryParse(
      _first(html, RegExp(r'>Tahun</dt><dd[^>]*>(\d{4})</dd>')),
    ),
    type: _text(_first(html, RegExp(r'>Tipe</dt><dd[^>]*>([^<]+)</dd>'))),
    studios: [
      for (final match in RegExp(
        r'<a[^>]*>([^<]+)</a>',
      ).allMatches(studioBlock))
        _text(match.group(1)!).trim(),
    ],
    genres: [
      for (final match in RegExp(
        r'<a class="rounded-full[^"]*"[^>]*href="/genre/[^"]*"[^>]*>([^<]+)</a>',
      ).allMatches(html))
        _text(match.group(1)!).trim(),
    ],
    synopsis: synopsis,
    episodes: episodes,
  );
}

/// Mirrors for one episode, labelled by quality.
List<WatchStream> parseSokujaMirrors(String body) {
  final root = jsonDecode(body) as Map<String, dynamic>;
  final streams = [
    for (final mirror
        in (root['mirrors'] as List? ?? const []).cast<Map<String, dynamic>>())
      if ((mirror['embedUrl'] as String?)?.isNotEmpty ?? false)
        WatchStream(
          label: (mirror['quality'] as String? ?? '').trim().isEmpty
              ? 'Source'
              : (mirror['quality'] as String).trim(),
          url: mirror['embedUrl'] as String,
        ),
  ];
  streams.sort((a, b) => _quality(b.label).compareTo(_quality(a.label)));
  return streams;
}

int _quality(String label) => int.tryParse(label.replaceAll('p', '')) ?? 0;

/// Title similarity that ignores punctuation and common particles.
double sokujaTitleScore(String a, String b) {
  final left = _words(a);
  final right = _words(b);
  if (left.isEmpty || right.isEmpty) return 0;
  final shared = left.intersection(right).length;
  return 2 * shared / (left.length + right.length);
}

/// Scores how likely [detail] is the same anime as [anime]. Years, main
/// studio, genres and episode count disambiguate same-title seasons.
double sokujaMatchScore({
  required SokujaDetail detail,
  required WatchAnime anime,
  String? animeTitle,
}) {
  final title = sokujaTitleScore(animeTitle ?? anime.title, detail.title);
  var score = title * 0.5;

  if (anime.year != null && detail.year != null) {
    final diff = (anime.year! - detail.year!).abs();
    score += diff == 0 ? 0.2 : (diff <= 1 ? 0.1 : 0);
  }

  final studios = _words(detail.studios.join(' '));
  if (studios.isNotEmpty &&
      anime.studios.any((studio) {
        final words = _words(studio);
        return words.isNotEmpty && studios.containsAll(words);
      })) {
    score += 0.15;
  }

  final genres = _words(detail.genres.join(' '));
  final animeGenres = _words(anime.genres.join(' '));
  final sharedGenres = genres.intersection(animeGenres).length;
  if (genres.isNotEmpty) score += 0.1 * sharedGenres / genres.length;

  if (anime.episodes != null && detail.episodes.length == anime.episodes) {
    score += 0.1;
  }

  // The synopsis is Indonesian while AniList describes in English, so it
  // only contributes when the wording happens to overlap.
  final synopsis = _words(detail.synopsis);
  final description = _words(anime.description);
  if (synopsis.isNotEmpty && description.isNotEmpty) {
    final shared = synopsis.intersection(description).length;
    score += 0.05 * shared / synopsis.length;
  }

  return score.clamp(0, 1);
}

final _detailCache = <String, SokujaDetail>{};

/// Best sokuja server for [anime] episode [episode], or null when no
/// candidate is a confident match (wrong anime is worse than no server).
Future<WatchServer?> fetchSokujaServer(
  WatchAnime anime,
  int episode, {
  String? altTitle,
}) async {
  final candidates = <String, SokujaCandidate>{};
  for (final query in _queries([altTitle ?? anime.title, anime.title])) {
    try {
      final response = await sharedHttpClient.get(
        Uri.parse('$sokujaBase/api/search/')
            .replace(queryParameters: {'q': query, 'limit': '20'}),
        headers: _headers,
      );
      if (response.statusCode != 200) continue;
      for (final candidate in parseSokujaSearch(response.body)) {
        candidates.putIfAbsent(candidate.slug, () => candidate);
      }
      final title = altTitle ?? anime.title;
      if (candidates.values.any(
        (candidate) => sokujaTitleScore(title, candidate.title) >= 0.6,
      )) {
        break;
      }
    } catch (_) {
      // Try the next, shorter query.
    }
  }

  final ranked = candidates.values.toList()
    ..sort(
      (a, b) => _cheapScore(
        b,
        anime,
        altTitle,
      ).compareTo(_cheapScore(a, anime, altTitle)),
    );

  SokujaDetail? bestDetail;
  var bestScore = 0.0;
  for (final candidate in ranked.take(3)) {
    final detail = await _detail(candidate.slug);
    if (detail == null) continue;
    final score = sokujaMatchScore(
      detail: detail,
      anime: anime,
      animeTitle: altTitle,
    );
    if (score > bestScore) {
      bestScore = score;
      bestDetail = detail;
    }
  }
  if (bestDetail == null || bestScore < 0.55) return null;

  SokujaEpisode? match;
  for (final item in bestDetail.episodes) {
    if (item.number == episode) {
      match = item;
      break;
    }
  }
  if (match == null) return null;

  try {
    final response = await sharedHttpClient.get(
      Uri.parse('$sokujaBase/api/video-mirrors/')
          .replace(queryParameters: {'e': '${match.id}'}),
      headers: _headers,
    );
    if (response.statusCode != 200) return null;
    final streams = parseSokujaMirrors(response.body);
    if (streams.isEmpty) return null;
    return WatchServer(
      provider: sokujaProvider,
      streams: streams,
      headers: const {'referer': '$sokujaBase/'},
    );
  } catch (_) {
    return null;
  }
}

double _cheapScore(SokujaCandidate candidate, WatchAnime anime, String? title) {
  var score = sokujaTitleScore(title ?? anime.title, candidate.title);
  if (anime.year != null && candidate.year != null) {
    final diff = (anime.year! - candidate.year!).abs();
    score += diff == 0 ? 0.2 : (diff <= 1 ? 0.1 : 0);
  }
  return score;
}

Future<SokujaDetail?> _detail(String slug) async {
  final cached = _detailCache[slug];
  if (cached != null) return cached;
  try {
    final response = await sharedHttpClient.get(
      Uri.parse('$sokujaBase/anime/$slug/'),
      headers: {..._headers, 'accept': 'text/html'},
    );
    if (response.statusCode != 200) return null;
    final detail = parseSokujaDetail(response.body);
    if (detail != null) _detailCache[slug] = detail;
    return detail;
  } catch (_) {
    return null;
  }
}

/// Search is a title prefix match, so retry with fewer leading words.
Iterable<String> _queries(List<String> titles) sync* {
  final seen = <String>{};
  for (final title in titles) {
    final words = title.trim().split(RegExp(r'\s+'));
    for (final length in [words.length, 5, 3, 2]) {
      if (length <= 0 || length > words.length) continue;
      final query = words.take(length).join(' ');
      if (seen.add(query.toLowerCase())) yield query;
    }
  }
}

final _particles = {
  'the',
  'a',
  'an',
  'and',
  'of',
  'to',
  'wa',
  'wo',
  'no',
  'ni',
  'de',
  'ga',
};

Set<String> _words(String text) => {
  for (final word in text.toLowerCase().split(RegExp(r'[^a-z0-9]+')))
    if (word.length > 1 && !_particles.contains(word)) word,
};

String _decodeRsc(String html) {
  final out = StringBuffer();
  for (final match in RegExp(
    r'self\.__next_f\.push\(\[1,"(.*?)"\]\)',
    dotAll: true,
  ).allMatches(html)) {
    final raw = match.group(1)!;
    final base64Chunk = RegExp(r'^[A-Za-z0-9+/=]+$').hasMatch(raw);
    try {
      out.write(
        base64Chunk
            ? utf8.decode(base64.decode(base64.normalize(raw)))
            // Older builds ship the RSC payload as a JSON string literal.
            : jsonDecode('"$raw"') as String,
      );
    } catch (_) {
      out.write(raw);
    }
  }
  return out.toString();
}

String _first(String text, RegExp pattern) =>
    pattern.firstMatch(text)?.group(1) ?? '';

String _text(String html) => html
    .replaceAll(RegExp(r'<[^>]*>'), ' ')
    .replaceAll('&amp;', '&')
    .replaceAll('&quot;', '"')
    .replaceAll('&#39;', "'")
    .replaceAll('&lt;', '<')
    .replaceAll('&gt;', '>')
    .replaceAll('&nbsp;', ' ')
    .replaceAll(RegExp(r'\s+'), ' ')
    .trim();
