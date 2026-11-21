import 'dart:async';
import 'dart:math' as math;

import 'package:flutter/services.dart';
import 'package:material_ui/material_ui.dart';
import 'package:video_player/video_player.dart';

import '../../models/models.dart';
import '../../services/app_settings.dart';
import '../../services/http_client.dart';
import '../../services/playback_session.dart';
import '../../services/subtitle_format.dart';

final _client = sharedHttpClient;

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
        math.cos((i + 0.5) * math.pi / 6) * 2,
        math.sin((i + 0.5) * math.pi / 6) * 2,
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

int _height(String label) =>
    int.tryParse(RegExp(r'\d+').firstMatch(label)?.group(0) ?? '') ?? 0;

/// Quality variants inside an HLS master playlist (`#EXT-X-STREAM-INF`),
/// best first. Relative segment URIs resolve against [base].
List<WatchStream> parseHlsVariants(String master, Uri base) {
  final lines = master.split('\n').map((line) => line.trim()).toList();
  final variants = <WatchStream>[];
  final seen = <String>{};
  for (var i = 0; i < lines.length; i++) {
    if (!lines[i].startsWith('#EXT-X-STREAM-INF')) continue;
    final uri = i + 1 < lines.length ? lines[i + 1] : '';
    if (uri.isEmpty || uri.startsWith('#')) continue;
    final url = base.resolve(uri).toString();
    if (!seen.add(url)) continue;
    final height = RegExp(r'RESOLUTION=\d+x(\d+)')
        .firstMatch(lines[i])
        ?.group(1);
    final name = RegExp(r'NAME="([^"]+)"').firstMatch(lines[i])?.group(1);
    variants.add(
      WatchStream(
        label: height != null
            ? '${height}p'
            : (name ?? 'Source ${variants.length + 1}'),
        url: url,
      ),
    );
  }
  variants.sort((a, b) => _height(b.label).compareTo(_height(a.label)));
  return variants;
}

/// Quality picker entries. A server's lone URL is the adaptive stream (its
/// master playlist picks the rendition), so the positional "Source 1" label
/// from the API is renamed instead of sitting next to the real qualities.
List<WatchStream> qualityOptions(
  List<WatchStream> streams,
  List<WatchStream> variants,
) {
  if (streams.length != 1 || !streams.first.label.startsWith('Source ')) {
    return [...streams, ...variants];
  }
  return [WatchStream(label: 'Auto', url: streams.first.url), ...variants];
}

String _formatDuration(Duration duration) {
  final hours = duration.inHours;
  final minutes = duration.inMinutes.remainder(60).toString().padLeft(2, '0');
  final seconds = duration.inSeconds.remainder(60).toString().padLeft(2, '0');
  return hours > 0 ? '$hours:$minutes:$seconds' : '$minutes:$seconds';
}

/// video_player player. Pass the server's streams (mp4/HLS `.m3u8`); quality,
/// subtitles and the subtitle picker live inside the player controls.
class VideoPlayerWidget extends StatefulWidget {
  const VideoPlayerWidget({
    super.key,
    required this.streams,
    this.headers,
    this.fallbacks = const [],
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

  /// Mirror hosts of the same episode, tried in order when a stream fails to
  /// open. They are never offered in the picker.
  final List<String> fallbacks;

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
  VideoPlayerController? _controller;
  VideoPlayerController? _positionSource;
  PlaybackSession? _session;
  bool _ownsSession = false;
  bool _failed = false;
  bool _fullscreen = false;
  bool _controlsVisible = true;
  Timer? _hideTimer;
  double? _drag;
  int? _selectedSubtitle;
  int _subtitleGeneration = 0;
  int _selectedStream = 0;

  /// Real qualities of a master playlist, appended after the server's own
  /// streams; empty until the playlist is parsed.
  List<WatchStream> _variants = const [];

  /// Mirrors not tried yet; a dead first host should not end playback.
  final List<String> _fallbacks = [];

  /// Indexes both lists: the server's streams first, then [_variants].
  List<WatchStream> get _qualityOptions =>
      qualityOptions(widget.streams, _variants);

  WatchStream get _stream {
    final options = _qualityOptions;
    return options[_selectedStream.clamp(0, options.length - 1)];
  }

  @override
  void initState() {
    super.initState();
    _selectedSubtitle = _defaultSubtitle();
    final provided = widget.session;
    final session = provided ?? PlaybackSession();
    _session = session;
    _ownsSession = provided == null;
    _fallbacks.addAll(widget.fallbacks);
    // A handed-over session is already playing; only a fresh one is opened.
    final existing = session.controller;
    if (existing != null) {
      _controller = existing;
      _listenPosition(existing);
    } else {
      unawaited(_open(_stream.url));
    }
  }

  @override
  void didUpdateWidget(covariant VideoPlayerWidget oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.streams.isEmpty ||
        widget.streams.isEmpty ||
        oldWidget.streams.first.url != widget.streams.first.url) {
      // Variants belong to the old playlist; a fresh `_open` refetches.
      _variants = const [];
      _fallbacks
        ..clear()
        ..addAll(widget.fallbacks);
    }
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
    if (_controller != null) unawaited(_applySubtitle());
  }

  /// Indonesian first, then the provider's order.
  List<int> get _subtitleOrder => subtitleOrder(widget.subtitles);

  int? _defaultSubtitle() => defaultSubtitleIndex(widget.subtitles);

  /// Opens [url] in a new controller and resumes at [startAt]. A controller is
  /// bound to one media, so a quality switch replaces it.
  Future<void> _open(String url, {Duration? startAt}) async {
    final next = VideoPlayerController.networkUrl(
      Uri.parse(url),
      httpHeaders: widget.headers ?? const {},
    );
    try {
      await next.initialize();
      if (!mounted) {
        await _disposeQuietly(next);
        return;
      }
      final previous = _controller;
      setState(() {
        _controller = next;
        _failed = false;
      });
      _session?.controller = next;
      _listenPosition(next);
      if (previous != null && !identical(previous, next)) {
        // The VideoPlayer for it is being unmounted in this frame.
        WidgetsBinding.instance.addPostFrameCallback(
          (_) => _disposeQuietly(previous),
        );
      }
      final resume = startAt ?? widget.startAt;
      if (resume != null && resume > Duration.zero) await next.seekTo(resume);
      await next.play();
      _scheduleHide();
      await _applySubtitle();
      // Only the provider's own host exposes qualities to the picker.
      if (url == widget.streams.first.url) unawaited(_loadQualityVariants());
    } catch (e) {
      debugPrint('video prepare failed: $e');
      // Mirror hosts go down one at a time; the next one is a fallback, not
      // an error the user has to fix.
      if (_fallbacks.isNotEmpty) {
        final fallback = _fallbacks.removeAt(0);
        await _disposeQuietly(next);
        if (mounted) return _open(fallback);
        return;
      }
      // A failed quality switch must not replace a stream that still plays.
      if (_controller != null) return;
      if (mounted) setState(() => _failed = true);
      await _disposeQuietly(next);
    }
  }

  static Future<void> _disposeQuietly(VideoPlayerController controller) async {
    try {
      await controller.dispose();
    } catch (_) {
      // Navigating away while `initialize` was in flight is not an error.
    }
  }

  void _listenPosition(VideoPlayerController controller) {
    final sink = widget.positionSink;
    if (sink == null) return;
    _positionSource?.removeListener(_pushPosition);
    _positionSource = controller;
    controller.addListener(_pushPosition);
  }

  void _pushPosition() {
    final sink = widget.positionSink;
    final controller = _positionSource;
    if (sink != null && controller != null) {
      sink.value = controller.value.position;
    }
  }

  /// A server that exposes a single HLS URL hides its renditions in the
  /// master playlist; without this the picker offers one unlabelled source.
  Future<void> _loadQualityVariants() async {
    final url = widget.streams.first.url;
    if (widget.streams.length > 1 || !url.contains('.m3u8')) return;
    try {
      final response = await _client.get(
        Uri.parse(url),
        headers: widget.headers,
      );
      if (!mounted || response.statusCode != 200) return;
      final variants = parseHlsVariants(response.body, Uri.parse(url));
      if (variants.isEmpty || !mounted) return;
      setState(() => _variants = variants);
    } catch (_) {
      // Keep the server's own URL as the only option.
    }
  }

  Future<void> _pickQuality() async {
    final options = _qualityOptions;
    final index = await showModalBottomSheet<int>(
      context: context,
      useRootNavigator: true,
      builder: (context) => SafeArea(
        child: ListView(
          shrinkWrap: true,
          children: [
            const ListTile(title: Text('Kualitas')),
            for (var i = 0; i < options.length; i++)
              ListTile(
                title: Text(options[i].label),
                selected: _selectedStream == i,
                onTap: () => Navigator.pop(context, i),
              ),
          ],
        ),
      ),
    );
    if (index == null || !mounted || index == _selectedStream) return;
    // Switching rendition is not a rewatch: resume where the user was.
    final position = _controller?.value.position ?? Duration.zero;
    setState(() => _selectedStream = index);
    await _open(_stream.url, startAt: position);
  }

  /// The Flutter caption layer parses SRT only, so ASS arrives flattened and
  /// every track is fetched here (providers reject bare requests).
  Future<void> _applySubtitle() async {
    final controller = _controller;
    if (controller == null) return;
    final generation = ++_subtitleGeneration;
    final index = _selectedSubtitle;
    if (index == null || index < 0 || index >= widget.subtitles.length) {
      await controller.setClosedCaptionFile(null);
      return;
    }
    try {
      final response = await _client.get(
        Uri.parse(widget.subtitles[index].url),
        headers: {
          'accept': '*/*',
          'referer': 'https://miruro.cx/',
          ...?widget.subtitleHeaders,
          // Per-track headers win: a sun subtitle needs sun's Origin/Referer
          // even when the playing stream comes from another provider.
          ...widget.subtitles[index].headers,
        },
      );
      if (generation != _subtitleGeneration || !mounted) return;
      final body = response.body.trim();
      if (response.statusCode != 200 || body.isEmpty) {
        await controller.setClosedCaptionFile(null);
        return;
      }
      final text = normalizeSubtitle(body);
      if (text.isEmpty) {
        await controller.setClosedCaptionFile(null);
        return;
      }
      await controller.setClosedCaptionFile(
        Future.value(
          text.trimLeft().startsWith('WEBVTT')
              ? WebVTTCaptionFile(text)
              : SubRipCaptionFile(text),
        ),
      );
    } catch (_) {
      if (!mounted || generation != _subtitleGeneration) return;
      await controller.setClosedCaptionFile(null);
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

  void _togglePlay() {
    final controller = _controller;
    if (controller == null) return;
    if (controller.value.isPlaying) {
      unawaited(controller.pause());
    } else {
      unawaited(controller.play());
      _scheduleHide();
    }
  }

  void _seekBy(int seconds) {
    final controller = _controller;
    if (controller == null) return;
    final duration = controller.value.duration;
    var target = controller.value.position + Duration(seconds: seconds);
    if (target < Duration.zero) target = Duration.zero;
    if (duration > Duration.zero && target > duration) target = duration;
    unawaited(controller.seekTo(target));
  }

  Future<void> _seekTo(double milliseconds) async {
    await _controller?.seekTo(Duration(milliseconds: milliseconds.round()));
    if (mounted) setState(() => _drag = null);
    _scheduleHide();
  }

  void _toggleControls() {
    setState(() => _controlsVisible = !_controlsVisible);
    if (_controlsVisible) _scheduleHide();
  }

  /// Hides the controls after a pause in interaction, like a native player.
  void _scheduleHide() {
    _hideTimer?.cancel();
    final controller = _controller;
    if (!widget.showControls ||
        controller == null ||
        !controller.value.isPlaying) {
      return;
    }
    _hideTimer = Timer(const Duration(seconds: 3), () {
      if (mounted) setState(() => _controlsVisible = false);
    });
  }

  Future<void> _toggleFullscreen() async {
    if (_fullscreen) {
      Navigator.of(context).maybePop();
      return;
    }
    setState(() => _fullscreen = true);
    await SystemChrome.setEnabledSystemUIMode(SystemUiMode.immersiveSticky);
    await SystemChrome.setPreferredOrientations(const [
      DeviceOrientation.landscapeLeft,
      DeviceOrientation.landscapeRight,
    ]);
    if (!mounted) return;
    await Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (_) => Scaffold(
          backgroundColor: Colors.black,
          body: Center(
            child: AspectRatio(aspectRatio: 16 / 9, child: _surface()),
          ),
        ),
      ),
    );
    if (!mounted) return;
    setState(() => _fullscreen = false);
    await SystemChrome.setEnabledSystemUIMode(SystemUiMode.edgeToEdge);
    await SystemChrome.setPreferredOrientations(DeviceOrientation.values);
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

  /// Captions are rendered here, not by the plugin, so the app's font, size
  /// and outline settings still apply.
  Widget _caption(VideoPlayerValue value) {
    final text = value.caption.text;
    if (text.isEmpty) return const SizedBox.shrink();
    final px = widget.subtitlePxOverride ?? subtitlePx.value;
    final gap = 32.0 + math.max(0, px - defaultSubtitlePx) * 1.5;
    return IgnorePointer(
      child: Align(
        alignment: Alignment.bottomCenter,
        child: Padding(
          padding: EdgeInsets.fromLTRB(
            16,
            0,
            16,
            widget.showControls && _controlsVisible ? gap + 56 : gap,
          ),
          child: Text(
            text,
            textAlign: TextAlign.center,
            style: TextStyle(
              fontFamily: 'Comic Neue',
              fontFamilyFallback: const ['Noto Sans', 'Noto Sans Symbols 2'],
              fontSize: px,
              fontWeight: FontWeight.w700,
              height: 1.25,
              color: const Color(0xFFFFFFFF),
              shadows: _subtitleShadows,
            ),
          ),
        ),
      ),
    );
  }

  Widget _controls(VideoPlayerController controller, VideoPlayerValue value) {
    final duration = value.duration.inMilliseconds.toDouble();
    final position = (_drag ?? value.position.inMilliseconds.toDouble()).clamp(
      0.0,
      duration > 0 ? duration : 1.0,
    );
    return LayoutBuilder(
      builder: (context, box) {
        var tapX = 0.0;
        return GestureDetector(
          behavior: HitTestBehavior.opaque,
          onTap: _toggleControls,
          onDoubleTapDown: (details) => tapX = details.localPosition.dx,
          onDoubleTap: () => _seekBy(tapX < box.maxWidth / 2 ? -10 : 10),
          child: AnimatedOpacity(
            opacity: _controlsVisible ? 1 : 0,
            duration: const Duration(milliseconds: 150),
            child: IgnorePointer(
              ignoring: !_controlsVisible,
              child: ColoredBox(
                color: const Color(0x44000000),
                child: Stack(
                  fit: StackFit.expand,
                  children: [
                    Positioned(
                      top: 0,
                      right: 0,
                      child: Row(
                        mainAxisSize: MainAxisSize.min,
                        children: [_qualityButton(), _subtitleButton()],
                      ),
                    ),
                    Center(
                      child: IconButton(
                        onPressed: _togglePlay,
                        iconSize: 48,
                        color: Colors.white,
                        icon: Icon(
                          value.isPlaying ? Icons.pause : Icons.play_arrow,
                        ),
                      ),
                    ),
                    Positioned(
                      left: 0,
                      right: 0,
                      bottom: 0,
                      child: ColoredBox(
                        color: Colors.black54,
                        child: Row(
                          children: [
                            IconButton(
                              visualDensity: VisualDensity.compact,
                              onPressed: _togglePlay,
                              color: Colors.white,
                              icon: Icon(
                                value.isPlaying
                                    ? Icons.pause
                                    : Icons.play_arrow,
                              ),
                            ),
                            Text(
                              '${_formatDuration(Duration(milliseconds: position.round()))}'
                              ' / ${_formatDuration(value.duration)}',
                              style: const TextStyle(
                                color: Colors.white,
                                fontSize: 12,
                              ),
                            ),
                            Expanded(
                              child: Slider(
                                value: position,
                                max: duration > 0 ? duration : 1,
                                onChanged: (x) => setState(() => _drag = x),
                                onChangeEnd: _seekTo,
                              ),
                            ),
                            IconButton(
                              visualDensity: VisualDensity.compact,
                              onPressed: _toggleFullscreen,
                              tooltip: 'Layar penuh',
                              color: Colors.white,
                              icon: Icon(
                                _fullscreen
                                    ? Icons.fullscreen_exit
                                    : Icons.fullscreen,
                              ),
                            ),
                          ],
                        ),
                      ),
                    ),
                  ],
                ),
              ),
            ),
          ),
        );
      },
    );
  }

  Widget _surface() {
    final controller = _controller!;
    return AnimatedBuilder(
      animation: subtitlePx,
      builder: (context, _) => ValueListenableBuilder<VideoPlayerValue>(
        valueListenable: controller,
        builder: (context, value, _) {
          final ratio = value.aspectRatio > 0 ? value.aspectRatio : 16 / 9;
          return Stack(
            fit: StackFit.expand,
            children: [
              const ColoredBox(color: Colors.black),
              Center(
                child: AspectRatio(
                  aspectRatio: ratio,
                  child: VideoPlayer(controller),
                ),
              ),
              if (value.isBuffering)
                const Center(child: CircularProgressIndicator()),
              Positioned.fill(child: _caption(value)),
              if (widget.showControls) _controls(controller, value),
            ],
          );
        },
      ),
    );
  }

  @override
  void dispose() {
    _hideTimer?.cancel();
    _hideTimer = null;
    _positionSource?.removeListener(_pushPosition);
    _positionSource = null;
    _controller = null;
    final session = _session;
    _session = null;
    if (_ownsSession && session != null) unawaited(session.dispose());
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final controller = _controller;
    if (controller == null || _failed) {
      return AspectRatio(
        aspectRatio: 16 / 9,
        child: ColoredBox(
          color: Colors.black,
          child: Center(
            child: _failed
                ? Text(
                    'Video gagal dimuat',
                    style: TextStyle(
                      color: Theme.of(context).colorScheme.error,
                    ),
                  )
                : const CircularProgressIndicator(),
          ),
        ),
      );
    }
    return AspectRatio(
      aspectRatio: 16 / 9,
      // The fullscreen route hosts the same controller; leaving a second
      // VideoPlayer on it would share one texture between two views.
      child: _fullscreen ? const ColoredBox(color: Colors.black) : _surface(),
    );
  }
}
