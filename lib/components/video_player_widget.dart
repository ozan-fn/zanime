import 'dart:async';
import 'dart:math' as math;

import 'package:http/http.dart' as http;
import 'package:material_ui/material_ui.dart';
import 'package:media_kit/media_kit.dart';
import 'package:media_kit_video/media_kit_video.dart';

import '../models/models.dart';
import '../services/http_client.dart';
import '../services/subtitle_format.dart';

final http.Client _client = createHttpClient();

/// Base size media_kit multiplies by the player box scale (≈ width / 1920), so
/// subtitles are ~4.8% of the video width inline and grow in fullscreen.
/// (Fullscreen re-creates its own `Video` from this config snapshot, so
/// per-mode sizes are only possible through media_kit's own scaling.)
const _subtitleFontSize = 92.0;

/// Axis-aligned shadows leave blocky corners on round glyphs; two staggered
/// rings with a little blur read as a smooth outline.
final List<Shadow> _subtitleShadows = [
  for (var i = 0; i < 12; i++)
    Shadow(
      color: const Color(0xFF000000),
      offset: Offset(
        math.cos(i * math.pi / 6) * 2,
        math.sin(i * math.pi / 6) * 2,
      ),
      blurRadius: 1,
    ),
  for (var i = 0; i < 12; i++)
    Shadow(
      color: const Color(0xFF000000),
      offset: Offset(
        math.cos((i + 0.5) * math.pi / 6),
        math.sin((i + 0.5) * math.pi / 6),
      ),
      blurRadius: 1,
    ),
];

bool _isIndonesian(WatchSubtitle subtitle) =>
    subtitle.label.toLowerCase().startsWith('indonesia');

/// Picker order: Indonesian tracks first, then the provider's order.
List<int> subtitleOrder(List<WatchSubtitle> subtitles) {
  final indonesian = <int>[];
  final rest = <int>[];
  for (var i = 0; i < subtitles.length; i++) {
    (_isIndonesian(subtitles[i]) ? indonesian : rest).add(i);
  }
  return [...indonesian, ...rest];
}

/// Default track: Indonesian, else the provider's default, else English,
/// else off.
int? defaultSubtitleIndex(List<WatchSubtitle> subtitles) {
  for (var i = 0; i < subtitles.length; i++) {
    if (_isIndonesian(subtitles[i])) return i;
  }
  for (var i = 0; i < subtitles.length; i++) {
    if (subtitles[i].isDefault) return i;
  }
  for (var i = 0; i < subtitles.length; i++) {
    if (subtitles[i].label.toLowerCase().startsWith('english')) return i;
  }
  return null;
}

/// media_kit video player. Pass the server's streams (mp4/HLS `.m3u8`);
/// quality, subtitles and the subtitle picker live inside the player controls.
class VideoPlayerWidget extends StatefulWidget {
  const VideoPlayerWidget({
    super.key,
    required this.streams,
    this.headers,
    this.subtitles = const [],
    this.subtitleHeaders,
  });

  /// Alternative qualities of one server, best first.
  final List<WatchStream> streams;

  /// Stream request headers (Referer/Origin) required by some mirrors.
  final Map<String, String>? headers;

  /// Subtitle tracks offered in the player's picker.
  final List<WatchSubtitle> subtitles;
  final Map<String, String>? subtitleHeaders;

  @override
  State<VideoPlayerWidget> createState() => _VideoPlayerWidgetState();
}

class _VideoPlayerWidgetState extends State<VideoPlayerWidget> {
  Player? _player;
  VideoController? _videoController;
  StreamSubscription<String>? _errorSubscription;
  bool _failed = false;
  int? _selectedSubtitle;
  int _subtitleGeneration = 0;
  int _selectedStream = 0;

  WatchStream get _stream => widget.streams[_selectedStream];

  @override
  void initState() {
    super.initState();
    _selectedSubtitle = _defaultSubtitle();
    try {
      // Idempotent; also covers entrypoints/hot-restart paths where main() did
      // not run this session.
      MediaKit.ensureInitialized();
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

  /// Indonesian first, then the provider's order.
  List<int> get _subtitleOrder => subtitleOrder(widget.subtitles);

  int? _defaultSubtitle() => defaultSubtitleIndex(widget.subtitles);

  Future<void> _prepare(Player player) async {
    await player.open(Media(_stream.url, httpHeaders: widget.headers));
    await _applySubtitle();
  }

  Future<void> _pickQuality() async {
    final index = await showModalBottomSheet<int>(
      context: context,
      useRootNavigator: true,
      builder: (context) => SafeArea(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            const ListTile(title: Text('Kualitas')),
            for (var i = 0; i < widget.streams.length; i++)
              ListTile(
                title: Text(widget.streams[i].label),
                selected: _selectedStream == i,
                onTap: () => Navigator.pop(context, i),
              ),
          ],
        ),
      ),
    );
    final player = _player;
    if (index == null || !mounted || index == _selectedStream || player == null) {
      return;
    }
    setState(() {
      _selectedStream = index;
      _failed = false;
    });
    await player.open(Media(_stream.url, httpHeaders: widget.headers));
    await _applySubtitle();
  }

  Future<void> _applySubtitle() async {
    final player = _player;
    if (player == null) return;
    final generation = ++_subtitleGeneration;
    final index = _selectedSubtitle;
    if (index == null) {
      await player.setSubtitleTrack(SubtitleTrack.no());
      return;
    }
    try {
      // Subtitle CDNs reject bare requests; media_kit's URI track cannot send
      // the provider's Origin/Referer, so fetch the VTT ourselves.
      final response = await _client.get(
        Uri.parse(widget.subtitles[index].url),
        headers: {
          'accept': '*/*',
          'referer': 'https://miruro.cx/',
          ...?widget.subtitleHeaders,
        },
      );
      if (generation != _subtitleGeneration || !mounted) return;
      if (response.statusCode == 200) {
        await player.setSubtitleTrack(
          SubtitleTrack.data(normalizeSubtitle(response.body)),
        );
      } else {
        await player.setSubtitleTrack(SubtitleTrack.no());
      }
    } catch (_) {
      if (generation == _subtitleGeneration) {
        await player.setSubtitleTrack(SubtitleTrack.no());
      }
    }
  }

  Future<void> _pickSubtitle() async {
    final index = await showModalBottomSheet<int>(
      context: context,
      useRootNavigator: true,
      builder: (context) => SafeArea(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            const ListTile(title: Text('Subtitle')),
            ListTile(
              title: const Text('Off'),
              selected: _selectedSubtitle == null,
              onTap: () => Navigator.pop(context, -1),
            ),
            for (final i in _subtitleOrder)
              ListTile(
                title: Text(widget.subtitles[i].label),
                selected: _selectedSubtitle == i,
                onTap: () => Navigator.pop(context, i),
              ),
          ],
        ),
      ),
    );
    if (index == null || !mounted) return;
    setState(() => _selectedSubtitle = index < 0 ? null : index);
    await _applySubtitle();
  }

  Widget _subtitleButton() {
    if (widget.subtitles.isEmpty) return const SizedBox.shrink();
    return IconButton(
      onPressed: _pickSubtitle,
      tooltip: 'Subtitle',
      icon: Icon(
        Icons.subtitles_outlined,
        color: _selectedSubtitle == null ? Colors.white54 : Colors.white,
      ),
    );
  }

  Widget _qualityButton() {
    if (widget.streams.length < 2) return const SizedBox.shrink();
    return TextButton(
      onPressed: _pickQuality,
      child: Text(
        _stream.label,
        style: const TextStyle(color: Colors.white),
      ),
    );
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
    if (controller == null || _failed) {
      return AspectRatio(
        aspectRatio: 16 / 9,
        child: Center(
          child: Text(
            'Video gagal dimuat',
            style: TextStyle(color: Theme.of(context).colorScheme.error),
          ),
        ),
      );
    }
    return AspectRatio(
      aspectRatio: 16 / 9,
      child: MaterialVideoControlsTheme(
        normal: MaterialVideoControlsThemeData(
          topButtonBar: [_qualityButton(), _subtitleButton()],
        ),
        fullscreen: MaterialVideoControlsThemeData(
          topButtonBar: [_qualityButton(), _subtitleButton()],
          // media_kit's built-in gestures: double tap left/right seeks 10s.
          seekGesture: true,
          seekOnDoubleTap: true,
          seekOnDoubleTapEnabledWhileControlsVisible: true,
        ),
        child: Video(
          controller: controller,
          fit: BoxFit.contain,
          subtitleViewConfiguration: SubtitleViewConfiguration(
            // No textScaler: media_kit then scales the font by the player box,
            // which is what makes subtitles larger in fullscreen.
            style: TextStyle(
              fontFamily: 'Comic Neue',
              fontSize: _subtitleFontSize,
              fontWeight: FontWeight.w700,
              height: 1.25,
              color: const Color(0xFFFFFFFF),
              backgroundColor: const Color(0x00000000),
              shadows: _subtitleShadows,
            ),
          ),
        ),
      ),
    );
  }
}
