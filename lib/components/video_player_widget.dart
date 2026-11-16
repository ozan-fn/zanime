import 'dart:async';

import 'package:http/http.dart' as http;
import 'package:material_ui/material_ui.dart';
import 'package:media_kit/media_kit.dart';
import 'package:media_kit_video/media_kit_video.dart';

/// media_kit video player. Pass an mp4/HLS (.m3u8) URL.
class VideoPlayerWidget extends StatefulWidget {
  const VideoPlayerWidget({
    super.key,
    required this.url,
    this.headers,
    this.subtitleUrl,
    this.subtitleHeaders,
  });

  final String url;

  /// Stream request headers (Referer/Origin) required by some mirrors.
  final Map<String, String>? headers;

  /// External `.vtt` file; null disables subtitles.
  final String? subtitleUrl;
  final Map<String, String>? subtitleHeaders;

  @override
  State<VideoPlayerWidget> createState() => _VideoPlayerWidgetState();
}

class _VideoPlayerWidgetState extends State<VideoPlayerWidget> {
  Player? _player;
  VideoController? _videoController;
  StreamSubscription<String>? _errorSubscription;
  bool _failed = false;
  int _subtitleGeneration = 0;

  @override
  void initState() {
    super.initState();
    try {
      final player = Player();
      _player = player;
      _videoController = VideoController(player);
      unawaited(_prepare(player));
      _errorSubscription = player.stream.error.listen((_) {
        if (mounted) setState(() => _failed = true);
      });
    } catch (e) {
      // No libmpv available (e.g. widget test) or player setup failed.
      debugPrint('video init failed: $e');
      _failed = true;
    }
  }

  Future<void> _prepare(Player player) async {
    await player.open(Media(widget.url, httpHeaders: widget.headers));
    await _applySubtitle();
  }

  Future<void> _applySubtitle() async {
    final player = _player;
    if (player == null) return;
    final generation = ++_subtitleGeneration;
    final url = widget.subtitleUrl;
    if (url == null) {
      await player.setSubtitleTrack(SubtitleTrack.no());
      return;
    }
    try {
      // Subtitle CDNs reject bare requests; media_kit's URI track cannot send
      // the provider's Origin/Referer, so fetch the VTT ourselves.
      final response = await http.get(
        Uri.parse(url),
        headers: {
          'accept': '*/*',
          'referer': 'https://miruro.cx/',
          ...?widget.subtitleHeaders,
        },
      );
      if (generation != _subtitleGeneration || !mounted) return;
      if (response.statusCode == 200) {
        await player.setSubtitleTrack(SubtitleTrack.data(response.body));
      } else {
        await player.setSubtitleTrack(SubtitleTrack.no());
      }
    } catch (_) {
      if (generation == _subtitleGeneration) {
        await player.setSubtitleTrack(SubtitleTrack.no());
      }
    }
  }

  @override
  void didUpdateWidget(VideoPlayerWidget oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.subtitleUrl != widget.subtitleUrl) {
      unawaited(_applySubtitle());
    }
  }

  @override
  void dispose() {
    _errorSubscription?.cancel();
    _player?.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final controller = _videoController;
    return AspectRatio(
      aspectRatio: 16 / 9,
      child: controller == null || _failed
          ? Center(
              child: Text(
                'Video gagal dimuat',
                style: TextStyle(color: Theme.of(context).colorScheme.error),
              ),
            )
          : Video(controller: controller, fit: BoxFit.contain),
    );
  }
}
