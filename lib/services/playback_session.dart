import 'package:flutter/foundation.dart';
import 'package:media_kit/media_kit.dart';
import 'package:media_kit_video/media_kit_video.dart';

/// One libmpv player shared by the watch page and the in-app PiP, so handing
/// playback over never restarts or stops it.
class PlaybackSession {
  PlaybackSession._(this.player) : controller = VideoController(player);

  /// Null when libmpv is unavailable (e.g. widget tests).
  static PlaybackSession? create() {
    try {
      // Idempotent; also covers entrypoints/hot-restart paths where main()
      // did not run this session.
      MediaKit.ensureInitialized();
      final player = Player(
        // libass (mpv) renders ASS/SSA natively, keeping \an top/bottom
        // positioning that Flutter's flat text layer cannot express.
        configuration: const PlayerConfiguration(
          libass: true,
          libassAndroidFont: 'assets/fonts/ComicNeue-Bold.ttf',
          libassAndroidFontName: 'Comic Neue',
        ),
      );
      return PlaybackSession._(player);
    } catch (e) {
      debugPrint('video init failed: $e');
      return null;
    }
  }

  final Player player;
  final VideoController controller;

  /// Stream currently open; null until the first `open` succeeds.
  String? openedUrl;

  bool _disposed = false;

  Future<void> dispose() async {
    if (_disposed) return;
    _disposed = true;
    await player.dispose();
  }
}
