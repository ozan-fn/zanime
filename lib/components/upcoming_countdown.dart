import 'dart:async';

import 'package:material_ui/material_ui.dart';

/// Countdown to the next episode, shown above the player.
class UpcomingCountdown extends StatefulWidget {
  const UpcomingCountdown({
    super.key,
    required this.episode,
    required this.airDate,
    this.now,
  });

  final int episode;
  final DateTime airDate;

  /// Injected in tests.
  final DateTime Function()? now;

  @override
  State<UpcomingCountdown> createState() => _UpcomingCountdownState();
}

class _UpcomingCountdownState extends State<UpcomingCountdown> {
  late final Timer _timer = Timer.periodic(
    const Duration(minutes: 1),
    (_) => setState(() {}),
  );

  DateTime get _now => widget.now?.call() ?? DateTime.now();

  @override
  void dispose() {
    _timer.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final remaining = widget.airDate.difference(_now);
    if (remaining.isNegative) return const SizedBox.shrink();
    return Container(
      width: double.infinity,
      color: const Color(0xFF18181B),
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
      child: Row(
        children: [
          const Icon(Icons.schedule, size: 16, color: Color(0xFFF4F4F5)),
          const SizedBox(width: 8),
          Expanded(
            child: Text(
              'Episode ${widget.episode} tayang dalam '
              '${formatCountdown(remaining)}',
              style: const TextStyle(fontSize: 12, color: Color(0xFFF4F4F5)),
            ),
          ),
        ],
      ),
    );
  }
}

/// `2d 3j`, `3j 12m`, `12m` or `<1m` for sub-minute remainders.
String formatCountdown(Duration remaining) {
  if (remaining.inMinutes < 1) return '<1m';
  final days = remaining.inDays;
  final hours = remaining.inHours % 24;
  final minutes = remaining.inMinutes % 60;
  if (days > 0) return '${days}d ${hours}j';
  if (hours > 0) return '${hours}j ${minutes}m';
  return '${minutes}m';
}
