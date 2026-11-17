import 'package:flutter/foundation.dart';
import 'package:shared_preferences/shared_preferences.dart';

const _textScaleKey = 'text_scale';
const minTextScale = 0.85;
const maxTextScale = 1.2;

/// App-wide font scale, applied by [MaterialApp.builder] in main.dart.
final ValueNotifier<double> textScale = ValueNotifier<double>(1);

Future<void> loadTextScale() async {
  try {
    final prefs = await SharedPreferences.getInstance();
    final value = prefs.getDouble(_textScaleKey);
    if (value != null && value >= minTextScale && value <= maxTextScale) {
      textScale.value = value;
    }
  } catch (_) {
    // Keep the default when preferences are unavailable (e.g. tests).
  }
}

Future<void> setTextScale(double value) async {
  textScale.value = value.clamp(minTextScale, maxTextScale);
  try {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setDouble(_textScaleKey, textScale.value);
  } catch (_) {
    // The in-memory value still applies for this session.
  }
}
