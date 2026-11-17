import 'package:flutter/widgets.dart';

import '../models/models.dart';

/// Playback that keeps running in the in-app floating player.
class MiniPlayback {
  const MiniPlayback({
    required this.animeId,
    required this.episode,
    required this.streams,
    required this.headers,
    required this.position,
    this.subtitles = const [],
    this.subtitleHeaders,
  });

  final String animeId;
  final int episode;
  final List<WatchStream> streams;
  final Map<String, String>? headers;
  final List<WatchSubtitle> subtitles;
  final Map<String, String>? subtitleHeaders;
  final Duration position;
}

/// Non-null while the floating player is visible.
final ValueNotifier<MiniPlayback?> miniPlayback =
    ValueNotifier<MiniPlayback?>(null);

/// Lets the floating player push the watch page from outside the navigator.
final navigatorKey = GlobalKey<NavigatorState>();
