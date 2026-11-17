import 'package:flutter/foundation.dart';
import 'package:shared_preferences/shared_preferences.dart';

const _historyKey = 'search_history';
const maxSearchHistory = 10;

/// Recent search queries, newest first.
final ValueNotifier<List<String>> searchHistory = ValueNotifier<List<String>>(
  const [],
);

/// Restores saved history; called from main so it survives restarts.
Future<void> loadSearchHistory() async {
  try {
    final prefs = await SharedPreferences.getInstance();
    final saved = prefs.getStringList(_historyKey);
    if (saved != null) searchHistory.value = List.unmodifiable(saved);
  } catch (_) {
    // Keep whatever is in memory when preferences are unavailable.
  }
}

/// Adds [query] at the front, drops an older duplicate and trims the list.
Future<void> addSearchHistory(String query) async {
  final trimmed = query.trim();
  if (trimmed.isEmpty) return;
  final next = [
    trimmed,
    ...searchHistory.value.where(
      (item) => item.toLowerCase() != trimmed.toLowerCase(),
    ),
  ];
  if (next.length > maxSearchHistory) {
    next.removeRange(maxSearchHistory, next.length);
  }
  searchHistory.value = List.unmodifiable(next);
  await _save();
}

/// Removes one query from the history.
Future<void> removeSearchHistory(String query) async {
  final next = searchHistory.value.where((item) => item != query).toList();
  searchHistory.value = List.unmodifiable(next);
  await _save();
}

Future<void> _save() async {
  try {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setStringList(_historyKey, searchHistory.value.toList());
  } catch (_) {
    // The in-memory value still applies for this session.
  }
}
