class AnimeBannerData {
  const AnimeBannerData({
    required this.title,
    required this.imageUrl,
    required this.genres,
    this.year,
    this.episodes,
    this.aired,
  });

  factory AnimeBannerData.fromJson(Map<String, dynamic> json) {
    return AnimeBannerData(
      title: json['title'] as String,
      imageUrl: json['imageUrl'] as String,
      genres: (json['genres'] as List).cast<String>(),
      year: json['year'] as int?,
      episodes: json['episodes'] as int?,
      aired: json['aired'] as int?,
    );
  }

  final String title;
  final String imageUrl;
  final List<String> genres;
  final int? year;
  final int? episodes;
  final int? aired;

  String get meta => [
    ...genres,
    if (year != null) '$year',
    if (episodes != null) '${aired ?? 0}/$episodes eps',
  ].join(' · ');
}

class LatestEpisode {
  const LatestEpisode({
    required this.animeId,
    required this.title,
    required this.romaji,
    required this.imageUrl,
    required this.episode,
    required this.airedAt,
    required this.format,
    required this.year,
    this.subCount,
    this.dubCount,
  });

  factory LatestEpisode.fromJson(Map<String, dynamic> json) {
    return LatestEpisode(
      animeId: json['animeId'] as String,
      title: json['title'] as String,
      romaji: json['romaji'] as String,
      imageUrl: json['imageUrl'] as String,
      episode: json['episode'] as int,
      airedAt: DateTime.parse(json['airedAt'] as String),
      format: json['format'] as String,
      year: json['year'] as int,
      subCount: json['subCount'] as int?,
      dubCount: json['dubCount'] as int?,
    );
  }

  final String animeId;
  final String title;
  final String romaji;
  final String imageUrl;
  final int episode;
  final DateTime airedAt;
  final String format;
  final int year;
  final int? subCount;
  final int? dubCount;

  String get url => 'https://miruro.cx/watch/$animeId?ep=$episode';
}

class SearchResult {
  const SearchResult({
    required this.animeId,
    required this.title,
    required this.imageUrl,
    this.format,
    this.year,
    this.episodes,
    this.score,
    this.aniListId,
  });

  factory SearchResult.fromJson(Map<String, dynamic> json) {
    return SearchResult(
      animeId: json['animeId'] as String,
      title: json['title'] as String,
      imageUrl: json['imageUrl'] as String,
      format: json['format'] as String?,
      year: json['year'] as int?,
      episodes: json['episodes'] as int?,
      score: json['score'] as int?,
      aniListId: json['aniListId'] as int?,
    );
  }

  final String animeId;
  final String title;
  final String imageUrl;
  final String? format;
  final int? year;
  final int? episodes;
  final int? score;

  /// AniList media id, used to match AniList entries to a Miruro id.
  final int? aniListId;

  String get subtitle => [
    ?format,
    if (year != null) '$year',
    if (episodes != null) '$episodes eps',
    if (score != null) '★ $score',
  ].join(' · ');

  String get url => 'https://miruro.cx/watch/$animeId';
}

class WatchAnime {
  const WatchAnime({
    required this.title,
    required this.imageUrl,
    required this.description,
    required this.format,
    required this.status,
    required this.season,
    required this.genres,
    required this.studios,
    this.year,
    this.episodes,
    this.duration,
    this.score,
  });

  factory WatchAnime.fromJson(Map<String, dynamic> json) {
    return WatchAnime(
      title: json['title'] as String,
      imageUrl: json['imageUrl'] as String,
      description: json['description'] as String,
      format: json['format'] as String,
      status: json['status'] as String,
      season: json['season'] as String,
      genres: (json['genres'] as List).cast<String>(),
      studios: (json['studios'] as List).cast<String>(),
      year: json['year'] as int?,
      episodes: json['episodes'] as int?,
      duration: json['duration'] as int?,
      score: json['score'] as int?,
    );
  }

  final String title;
  final String imageUrl;
  final String description;
  final String format;
  final String status;
  final String season;
  final List<String> genres;
  final List<String> studios;
  final int? year;
  final int? episodes;
  final int? duration;
  final int? score;

  String get seasonLabel => [
    if (season.isNotEmpty)
      '${season[0]}${season.substring(1).toLowerCase()}${year == null ? '' : ' $year'}'
    else if (year != null)
      '$year',
  ].join();

  String get meta => [
    if (format.isNotEmpty) format,
    if (episodes != null) '$episodes eps',
    if (duration != null) '$duration min',
  ].join(' · ');

  String get statusLine => [
    if (status.isNotEmpty) '${status[0]}${status.substring(1).toLowerCase()}',
    if (studios.isNotEmpty) studios.join(', '),
  ].join(' · ');
}

class WatchEpisode {
  const WatchEpisode({
    required this.number,
    required this.title,
    this.imageUrl,
    this.airDate,
    this.description,
    this.duration,
  });

  factory WatchEpisode.fromJson(Map<String, dynamic> json) {
    return WatchEpisode(
      number: json['number'] as int,
      title: (json['title'] as String?) ?? '',
      imageUrl: json['imageUrl'] as String?,
      airDate: json['airDate'] as String?,
      description: json['description'] as String?,
      duration: json['duration'] as int?,
    );
  }

  final int number;
  final String title;
  final String? imageUrl;
  final String? airDate;
  final String? description;
  final int? duration;
}

class WatchSubtitle {
  const WatchSubtitle({
    required this.label,
    required this.url,
    this.isDefault = false,
  });

  factory WatchSubtitle.fromJson(Map<String, dynamic> json) {
    return WatchSubtitle(
      label: json['label'] as String,
      url: json['url'] as String,
      isDefault: json['isDefault'] as bool? ?? false,
    );
  }

  final String label;
  final String url;
  final bool isDefault;
}

class WatchStream {
  const WatchStream({required this.label, required this.url});

  /// `quality`/`resolution`/`label` become the picker label; anything else
  /// falls back to a positional name.
  factory WatchStream.fromJson(Map<String, dynamic> json, int index) {
    final raw = json['quality'] ?? json['resolution'] ?? json['label'];
    return WatchStream(
      label: raw is String && raw.trim().isNotEmpty
          ? raw.trim()
          : raw is num && raw > 0
          ? '${raw.toInt()}p'
          : 'Source ${index + 1}',
      url: json['url'] as String,
    );
  }

  final String label;
  final String url;
}

class WatchServer {
  const WatchServer({
    required this.provider,
    required this.streams,
    required this.headers,
  });

  factory WatchServer.fromJson(Map<String, dynamic> json) {
    final raw = ((json['streams'] as List?) ?? const [])
        .cast<Map<String, dynamic>>();
    final streams = [
      for (var i = 0; i < raw.length; i++)
        if ((raw[i]['url'] as String?)?.isNotEmpty ?? false)
          WatchStream.fromJson(raw[i], i),
    ];
    return WatchServer(
      provider: json['provider'] as String,
      streams: streams.isNotEmpty
          ? streams
          : [WatchStream(label: 'Auto', url: json['url'] as String)],
      headers: (json['headers'] as Map?)?.cast<String, String>() ?? const {},
    );
  }

  final String provider;

  /// Alternative qualities of the same server, best first.
  final List<WatchStream> streams;
  final Map<String, String> headers;

  String get url => streams.first.url;
}

class WatchData {
  const WatchData({
    required this.anime,
    required this.episodes,
    required this.servers,
    required this.currentEpisode,
    this.subtitles = const [],
    this.aniListId,
  });

  factory WatchData.fromJson(Map<String, dynamic> json) {
    return WatchData(
      anime: WatchAnime.fromJson(json['anime'] as Map<String, dynamic>),
      episodes: (json['episodes'] as List)
          .cast<Map<String, dynamic>>()
          .map(WatchEpisode.fromJson)
          .toList(),
      servers: (json['servers'] as List)
          .cast<Map<String, dynamic>>()
          .map(WatchServer.fromJson)
          .toList(),
      currentEpisode: json['currentEpisode'] as int,
      subtitles: ((json['subtitles'] as List?) ?? const [])
          .cast<Map<String, dynamic>>()
          .map(WatchSubtitle.fromJson)
          .toList(),
      aniListId: (json['aniListId'] as num?)?.toInt(),
    );
  }

  final WatchAnime anime;
  final List<WatchEpisode> episodes;
  final List<WatchServer> servers;
  final int currentEpisode;
  final List<WatchSubtitle> subtitles;

  /// AniList media id, used to save list entries.
  final int? aniListId;

  WatchEpisode? get episode {
    for (final item in episodes) {
      if (item.number == currentEpisode) return item;
    }
    return null;
  }

  /// e.g. "Jan 4, 2024".
  static String formatDate(String? isoDate) {
    if (isoDate == null) return '';
    final date = DateTime.tryParse(isoDate);
    if (date == null) return isoDate;
    const months = [
      'Jan',
      'Feb',
      'Mar',
      'Apr',
      'May',
      'Jun',
      'Jul',
      'Aug',
      'Sep',
      'Oct',
      'Nov',
      'Dec',
    ];
    return '${months[date.month - 1]} ${date.day}, ${date.year}';
  }
}

/// One AniList recommendation shown on the watch page.
class AnimeRecommendation {
  const AnimeRecommendation({
    required this.aniListId,
    required this.title,
    required this.imageUrl,
    this.format,
    this.year,
    this.episodes,
  });

  final int aniListId;
  final String title;
  final String imageUrl;
  final String? format;
  final int? year;
  final int? episodes;
}
