import 'package:video_player/video_player.dart';

/// One video_player controller shared by the watch page and the in-app PiP, so
/// handing playback over never restarts or stops it.
class PlaybackSession {
  /// Null until a stream has been opened; the widget creates the controller
  /// for its URL (a controller is bound to one media, unlike libmpv).
  VideoPlayerController? controller;

  bool _disposed = false;

  Future<void> dispose() async {
    if (_disposed) return;
    _disposed = true;
    await controller?.dispose();
    controller = null;
  }
}
