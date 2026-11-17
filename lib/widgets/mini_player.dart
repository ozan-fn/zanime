import 'dart:async';

import 'package:material_ui/material_ui.dart';

import '../services/mini_player.dart';
import '../screens/player/video_player_widget.dart';
import '../screens/detail/watch_screen.dart';

OverlayEntry? _entry;

/// Shows the in-app floating player above every route.
void showMiniPlayer(MiniPlayback playback) {
  miniPlayback.value = playback;
  final existing = _entry;
  if (existing != null) {
    existing.markNeedsBuild();
    return;
  }
  final overlay = navigatorKey.currentState?.overlay;
  if (overlay == null) return;
  final entry = OverlayEntry(builder: (_) => const _MiniPlayerCard());
  _entry = entry;
  overlay.insert(entry);
}

/// Hides the floating player; safe to call when it is not visible.
///
/// [disposeSession] is false when the watch page takes the player back, so
/// playback never stops between PiP and the watch page.
void hideMiniPlayer({bool disposeSession = true}) {
  final playback = miniPlayback.value;
  final entry = _entry;
  _entry = null;
  if (entry != null && entry.mounted) entry.remove();
  miniPlayback.value = null;
  if (disposeSession && playback?.session != null) {
    unawaited(playback!.session!.dispose());
  }
}

void _reopen(MiniPlayback playback) {
  hideMiniPlayer(disposeSession: false);
  navigatorKey.currentState?.push(
    MaterialPageRoute<void>(
      builder: (_) => WatchScreen(
        animeId: playback.animeId,
        episode: playback.episode,
        session: playback.session,
        initialData: playback.data,
      ),
    ),
  );
}

/// Draggable card like YouTube's mini player; a tap reopens the watch page.
class _MiniPlayerCard extends StatefulWidget {
  const _MiniPlayerCard();

  @override
  State<_MiniPlayerCard> createState() => _MiniPlayerCardState();
}

class _MiniPlayerCardState extends State<_MiniPlayerCard> {
  static const _width = 176.0;
  static const _margin = 12.0;
  static const _bottomOffset = 72.0;

  /// Drag position (top-left); null until the first drag.
  Offset? _position;

  @override
  Widget build(BuildContext context) {
    final playback = miniPlayback.value;
    if (playback == null) return const SizedBox.shrink();
    final size = MediaQuery.sizeOf(context);
    const height = _width * 9 / 16;
    final defaultOffset = Offset(
      size.width - _width - _margin,
      size.height - height - _bottomOffset,
    );
    final offset = _position ?? defaultOffset;
    return Positioned(
      left: offset.dx,
      top: offset.dy,
      width: _width,
      child: GestureDetector(
        behavior: HitTestBehavior.opaque,
        onTap: () => _reopen(playback),
        onPanStart: (_) => setState(() => _position = offset),
        onPanUpdate: (details) {
          final next = (_position ?? offset) + details.delta;
          setState(() {
            _position = Offset(
              next.dx.clamp(_margin, size.width - _width - _margin),
              next.dy.clamp(_margin, size.height - height - _margin),
            );
          });
        },
        child: Material(
          color: Colors.black,
          elevation: 6,
          borderRadius: BorderRadius.circular(3),
          clipBehavior: Clip.antiAlias,
          child: Stack(
            children: [
              VideoPlayerWidget(
                session: playback.session,
                streams: playback.streams,
                headers: playback.headers,
                subtitles: playback.subtitles,
                subtitleHeaders: playback.subtitleHeaders,
                showControls: false,
                subtitlePxOverride: 18,
              ),
              Positioned(
                top: 0,
                right: 0,
                child: IconButton(
                  onPressed: hideMiniPlayer,
                  tooltip: 'Tutup',
                  iconSize: 16,
                  style: IconButton.styleFrom(
                    backgroundColor: Colors.black54,
                    foregroundColor: Colors.white,
                    fixedSize: const Size(28, 28),
                    padding: EdgeInsets.zero,
                    minimumSize: Size.zero,
                    shape: RoundedRectangleBorder(
                      borderRadius: BorderRadius.circular(3),
                    ),
                  ),
                  icon: const Icon(Icons.close),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
