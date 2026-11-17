import 'package:flutter/foundation.dart';
import 'package:shared_preferences/shared_preferences.dart';

const _sizeKey = 'subtitle_px';

const minSubtitlePx = 12.0;
const maxSubtitlePx = 32.0;
const defaultSubtitlePx = 16.0;

/// Subtitle size in on-screen px.
final ValueNotifier<double> subtitlePx = ValueNotifier<double>(
  defaultSubtitlePx,
);

/// Loads the saved subtitle size; called from main so the player has it on
/// first open.
Future<void> loadSubtitleStyle() async {
  try {
    final prefs = await SharedPreferences.getInstance();
    final value = prefs.getDouble(_sizeKey);
    if (value != null && value >= minSubtitlePx && value <= maxSubtitlePx) {
      subtitlePx.value = value;
    }
  } catch (_) {
    // Keep the default when preferences are unavailable (e.g. tests).
  }
}

Future<void> setSubtitlePx(double px) async {
  subtitlePx.value = px.clamp(minSubtitlePx, maxSubtitlePx);
  try {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setDouble(_sizeKey, subtitlePx.value);
  } catch (_) {
    // The in-memory value still applies for this session.
  }
}
