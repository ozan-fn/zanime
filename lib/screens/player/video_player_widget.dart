import 'dart:async';
import 'dart:math' as math;

import 'package:http/http.dart' as http;
import 'package:material_ui/material_ui.dart';
import 'package:media_kit/media_kit.dart';
import 'package:media_kit_video/media_kit_video.dart';

import '../../models/models.dart';
import '../../services/app_settings.dart';
import '../../services/http_client.dart';
import '../../services/playback_session.dart';
import '../../services/subtitle_format.dart';

final http.Client _client = sharedHttpClient;

/// Nudges the center play/pause button down to sit visually centered.
final List<Widget> _primaryButtonBar = [
  Spacer(flex: 2),
  MaterialSkipPreviousButton(),
  Spacer(),
  Transform.translate(
    offset: Offset(0, 10),
    child: MaterialPlayOrPauseButton(iconSize: 48.0),
  ),
  Spacer(),
  MaterialSkipNextButton(),
  Spacer(flex: 2),
];

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
    RegExp(r'\bindonesian?\b').hasMatch(subtitle.label.toLowerCase());

bool _isEnglish(WatchSubtitle subtitle) =>
    RegExp(r'\benglish\b').hasMatch(subtitle.label.toLowerCase());

/// Only Indonesian and English tracks are shown in the picker.
bool _isShownSubtitle(WatchSubtitle subtitle) =>
    _isIndonesian(subtitle) || _isEnglish(subtitle);

/// Picker order: Indonesian tracks first, then English, provider order kept.
List<int> subtitleOrder(List<WatchSubtitle> subtitles) {
  final indonesian = <int>[];
  final english = <int>[];
  for (var i = 0; i < subtitles.length; i++) {
    if (!_isShownSubtitle(subtitles[i])) continue;
    (_isIndonesian(subtitles[i]) ? indonesian : english).add(i);
  }
  return [...indonesian, ...english];
}

/// Default track: Indonesian, else the provider's default (only if it is a
/// shown language), else English, else off.
int? defaultSubtitleIndex(List<WatchSubtitle> subtitles) {
  for (var i = 0; i < subtitles.length; i++) {
    if (_isIndonesian(subtitles[i])) return i;
  }
  for (var i = 0; i < subtitles.length; i++) {
    if (subtitles[i].isDefault && _isShownSubtitle(subtitles[i])) return i;
  }
  for (var i = 0; i < subtitles.length; i++) {
    if (_isEnglish(subtitles[i])) return i;
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
    this.startAt,
    this.positionSink,
    this.showControls = true,
    this.session,
    this.subtitlePxOverride,
  });

  /// Alternative qualities of one server, best first.
  final List<WatchStream> streams;

  /// Stream request headers (Referer/Origin) required by some mirrors.
  final Map<String, String>? headers;

  /// Subtitle tracks offered in the player's picker.
  final List<WatchSubtitle> subtitles;
  final Map<String, String>? subtitleHeaders;

  /// Where playback resumes (floating player handoff).
  final Duration? startAt;

  /// Receives the current playback position, used to hand off to the
  /// floating player without restarting.
  final ValueNotifier<Duration>? positionSink;

  /// False for the floating player: no controls overlay.
  final bool showControls;

  /// Shared player; when omitted the widget owns the session it creates.
  final PlaybackSession? session;

  /// Fixed on-screen subtitle size, ignoring the user setting (floating
  /// player: slightly larger than the default, independent of the slider).
  final double? subtitlePxOverride;

  @override
  State<VideoPlayerWidget> createState() => _VideoPlayerWidgetState();
}

class _VideoPlayerWidgetState extends State<VideoPlayerWidget> {
  Player? _player;
  VideoController? _videoController;
  PlaybackSession? _session;
  bool _ownsSession = false;
  StreamSubscription<String>? _errorSubscription;
  StreamSubscription<Duration>? _positionSubscription;
  bool _failed = false;
  int? _selectedSubtitle;
  int _subtitleGeneration = 0;
  int _selectedStream = 0;

  WatchStream get _stream => widget.streams[_selectedStream];

  @override
  void initState() {
    super.initState();
    _selectedSubtitle = _defaultSubtitle();
    final provided = widget.session;
    final session = provided ?? PlaybackSession.create();
    _session = session;
    _ownsSession = provided == null;
    if (session == null) {
      _failed = true;
      return;
    }
    _player = session.player;
    _videoController = session.controller;
    subtitlePx.addListener(_applySubtitleScale);
    unawaited(_applySubtitleScale());
    _errorSubscription = session.player.stream.error.listen((_) {
      if (mounted) setState(() => _failed = true);
    });
    final sink = widget.positionSink;
    if (sink != null) {
      _positionSubscription = session.player.stream.position.listen(
        (position) => sink.value = position,
      );
    }
    // A handed-over session is already playing; only a fresh one is prepared.
    if (session.openedUrl == null) {
      unawaited(_prepare(session.player));
    }
  }

  @override
  void didUpdateWidget(covariant VideoPlayerWidget oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (identical(oldWidget.subtitles, widget.subtitles)) return;
    // The track list can change while the player stays mounted (episode or
    // server refresh). Keep the same file selected when it is still offered;
    // otherwise fall back to the default (Indonesian) instead of pointing at
    // a stale index that would silently drop the subtitle.
    if (_selectedSubtitle == null &&
        !(oldWidget.subtitles.isEmpty && widget.subtitles.isNotEmpty)) {
      return; // The user chose "Off": keep it off.
    }
    final previous =
        _selectedSubtitle != null &&
            _selectedSubtitle! >= 0 &&
            _selectedSubtitle! < oldWidget.subtitles.length
        ? oldWidget.subtitles[_selectedSubtitle!].url
        : null;
    var next = previous == null
        ? -1
        : widget.subtitles.indexWhere((subtitle) => subtitle.url == previous);
    if (next == -1) next = _defaultSubtitle() ?? -1;
    final nextIndex = next == -1 ? null : next;
    if (nextIndex == _selectedSubtitle) return;
    _selectedSubtitle = nextIndex;
    if (_player != null) unawaited(_applySubtitle());
  }

  /// Indonesian first, then the provider's order.
  List<int> get _subtitleOrder => subtitleOrder(widget.subtitles);

  int? _defaultSubtitle() => defaultSubtitleIndex(widget.subtitles);

  Future<void> _prepare(Player player) async {
    try {
      await player.open(Media(_stream.url, httpHeaders: widget.headers));
      // The widget may have been removed while `open` was in flight; that
      // player is disposed and must not be touched again.
      if (!mounted || !identical(_player, player)) return;
      _session?.openedUrl = _stream.url;
      final startAt = widget.startAt;
      if (startAt != null && startAt > Duration.zero) {
        await player.seek(startAt);
      }
      await _applySubtitle();
    } catch (e) {
      if (!mounted || !identical(_player, player)) return;
      debugPrint('video prepare failed: $e');
      setState(() => _failed = true);
    }
  }

  Future<void> _pickQuality() async {
    final index = await showModalBottomSheet<int>(
      context: context,
      useRootNavigator: true,
      builder: (context) => SafeArea(
        child: ListView(
          shrinkWrap: true,
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
    if (index == null ||
        !mounted ||
        index == _selectedStream ||
        player == null) {
      return;
    }
    setState(() {
      _selectedStream = index;
      _failed = false;
    });
    await player.open(Media(_stream.url, httpHeaders: widget.headers));
    _session?.openedUrl = _stream.url;
    await _applySubtitle();
  }

  /// libass renders ASS/SSA tracks itself and ignores the Flutter subtitle
  /// style, so the size setting is forwarded as mpv's `sub-scale` multiplier
  /// (16 px is the reference size). Without it, ASS subtitles stay small no
  /// matter what the slider says.
  Future<void> _applySubtitleScale() async {
    final player = _player;
    if (player == null) return;
    final px = widget.subtitlePxOverride ?? subtitlePx.value;
    final scale = px / defaultSubtitlePx;
    try {
      final platform = player.platform;
      if (platform == null) return;
      // `setProperty` only exists on the native player.
      await (platform as dynamic).setProperty(
        'sub-scale',
        scale.toStringAsFixed(2),
      );
    } catch (_) {
      // Non-native platforms simply keep their default scale.
    }
  }

  Future<void> _applySubtitle() async {
    final player = _player;
    if (player == null) return;
    await _applySubtitleScale();
    final generation = ++_subtitleGeneration;
    final index = _selectedSubtitle;
    if (index == null || index < 0 || index >= widget.subtitles.length) {
      await player.setSubtitleTrack(SubtitleTrack.no());
      return;
    }
    // A fresh session (server switch) sometimes drops a track added right
    // after `open`; mpv is settled by the time this runs, so one attach is
    // enough.
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
      // Drop the previous track first: media_kit adds external tracks with
      // `sub-add`, so switching repeatedly would stack tracks (and any
      // auto-selected embedded track) on top of each other.
      await player.setSubtitleTrack(SubtitleTrack.no());
      if (response.statusCode == 200 && response.body.trim().isNotEmpty) {
        await player.setSubtitleTrack(
          SubtitleTrack.data(normalizeSubtitle(response.body)),
        );
      }
    } catch (_) {
      if (!mounted || generation != _subtitleGeneration) return;
      await player.setSubtitleTrack(SubtitleTrack.no());
    }
  }

  Future<void> _pickSubtitle() async {
    final index = await showModalBottomSheet<int>(
      context: context,
      useRootNavigator: true,
      builder: (context) => SafeArea(
        child: ListView(
          shrinkWrap: true,
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
    if (_subtitleOrder.isEmpty) return const SizedBox.shrink();
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
    if (widget.streams.isEmpty) return const SizedBox.shrink();
    return IconButton(
      onPressed: _pickQuality,
      tooltip: _stream.label,
      icon: const Icon(Icons.high_quality_outlined, color: Colors.white),
    );
  }

  @override
  void dispose() {
    _errorSubscription?.cancel();
    _errorSubscription = null;
    _positionSubscription?.cancel();
    _positionSubscription = null;
    _player = null;
    _videoController = null;
    final session = _session;
    _session = null;
    subtitlePx.removeListener(_applySubtitleScale);
    if (_ownsSession && session != null) unawaited(session.dispose());
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
      child: LayoutBuilder(
        builder: (context, constraints) {
          final width = constraints.maxWidth <= 0
              ? 360.0
              : constraints.maxWidth;
          return AnimatedBuilder(
            animation: subtitlePx,
            builder: (context, _) {
              final px = widget.subtitlePxOverride ?? subtitlePx.value;
              // Keep the gap below the subtitle proportional to its size so a
              // bigger line is lifted instead of sitting lower on the video.
              final bottomGap =
                  32.0 + math.max(0, px - defaultSubtitlePx) * 1.5;
              final video = Video(
                controller: controller,
                fit: BoxFit.contain,
                // PiP hides controls entirely; passing null would be ignored by
                // media_kit's copyWith (null means "keep existing controls"),
                // and the adaptive bottom bar overflows the 176x99 card.
                controls: widget.showControls
                    ? AdaptiveVideoControls
                    : (_) => const SizedBox.shrink(),
                subtitleViewConfiguration: SubtitleViewConfiguration(
                  // Slightly higher than media_kit's default bottom padding.
                  padding: EdgeInsets.fromLTRB(16, 0, 16, bottomGap),
                  // media_kit multiplies the size by the player box scale
                  // (≈ width / 1920); divide it back out so the settings value
                  // is the on-screen px size.
                  style: TextStyle(
                    fontFamily: 'Comic Neue',
                    fontFamilyFallback: const [
                      'Noto Sans',
                      'Noto Sans Symbols 2',
                    ],
                    fontSize: px * 1920 / width,
                    fontWeight: FontWeight.w700,
                    height: 1.25,
                    color: const Color(0xFFFFFFFF),
                    backgroundColor: const Color(0x00000000),
                    shadows: _subtitleShadows,
                  ),
                ),
              );
              if (!widget.showControls) return video;
              return MaterialVideoControlsTheme(
                normal: kDefaultMaterialVideoControlsThemeData.copyWith(
                  topButtonBar: [_qualityButton(), _subtitleButton()],
                  primaryButtonBar: _primaryButtonBar,
                  seekGesture: true,
                  seekOnDoubleTap: true,
                ),
                // Built-in fullscreen defaults bring the safe-area padding,
                // seek bar position and double-tap ±10s gestures; only the
                // buttons change.
                fullscreen: kDefaultMaterialVideoControlsThemeDataFullscreen
                    .copyWith(
                      topButtonBar: [_qualityButton(), _subtitleButton()],
                      seekBarMargin: const EdgeInsets.only(
                        left: 16,
                        right: 16,
                        bottom: 28,
                      ),
                    ),
                child: video,
              );
            },
          );
        },
      ),
    );
  }
}
