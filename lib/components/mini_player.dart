import 'package:material_ui/material_ui.dart';

import '../services/mini_player.dart';
import 'video_player_widget.dart';
import 'watch_screen.dart';

/// Current position of the floating player, used to resume where it left off.
final ValueNotifier<Duration> miniPosition = ValueNotifier<Duration>(
  Duration.zero,
);

OverlayEntry? _entry;

/// Shows the in-app floating player above every route.
void showMiniPlayer(MiniPlayback playback) {
  miniPlayback.value = playback;
  miniPosition.value = playback.position;
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
void hideMiniPlayer() {
  final entry = _entry;
  _entry = null;
  if (entry != null && entry.mounted) entry.remove();
  miniPlayback.value = null;
}

void _reopen(MiniPlayback playback) {
  final position = miniPosition.value;
  hideMiniPlayer();
  navigatorKey.currentState?.push(
    MaterialPageRoute<void>(
      builder: (_) => WatchScreen(
        animeId: playback.animeId,
        episode: playback.episode,
        startAt: position,
      ),
    ),
  );
}

/// Compact card in the bottom-right corner; tapping it reopens the watch page.
class _MiniPlayerCard extends StatelessWidget {
  const _MiniPlayerCard();

  @override
  Widget build(BuildContext context) {
    final playback = miniPlayback.value;
    if (playback == null) return const SizedBox.shrink();
    return Positioned(
      right: 12,
      bottom: 72,
      width: 176,
      child: Material(
        color: Colors.black,
        elevation: 6,
        borderRadius: BorderRadius.circular(3),
        clipBehavior: Clip.antiAlias,
        child: Stack(
          children: [
            VideoPlayerWidget(
              streams: playback.streams,
              headers: playback.headers,
              subtitles: playback.subtitles,
              subtitleHeaders: playback.subtitleHeaders,
              startAt: playback.position,
              positionSink: miniPosition,
              showControls: false,
            ),
            Positioned.fill(
              child: GestureDetector(
                behavior: HitTestBehavior.opaque,
                onTap: () => _reopen(playback),
              ),
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
    );
  }
}
