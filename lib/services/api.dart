import 'dart:convert';

import 'package:http/http.dart' as http;

import '../models/models.dart';
import 'proxy.dart';

Future<dynamic> _getJson(String path, [Map<String, String>? query]) async {
  final uri = Uri.parse(
    '$serverBase$path',
  ).replace(queryParameters: query);
  final response = await http.get(uri);
  if (response.statusCode != 200) {
    throw http.ClientException('Server HTTP ${response.statusCode}', uri);
  }
  return jsonDecode(response.body);
}

/// Currently airing anime, trending first.
Future<List<AnimeBannerData>> fetchAnimeBanners() async {
  final items = await _getJson('/api/banners') as List;
  return items
      .cast<Map<String, dynamic>>()
      .map(AnimeBannerData.fromJson)
      .toList();
}

/// Same array and order as the "Latest Episodes" list on miruro.cx.
Future<List<LatestEpisode>> fetchLatestEpisodes() async {
  final items = await _getJson('/api/latest') as List;
  return items
      .cast<Map<String, dynamic>>()
      .map(LatestEpisode.fromJson)
      .toList();
}

Future<List<SearchResult>> searchAnime(String query) async {
  final items = await _getJson('/api/search', {'q': query}) as List;
  return items.cast<Map<String, dynamic>>().map(SearchResult.fromJson).toList();
}

Future<WatchData> fetchWatchData(String animeId, {int episode = 1}) async {
  final json = await _getJson(
    '/api/watch/${Uri.encodeComponent(animeId)}',
    {'ep': '$episode'},
  );
  return WatchData.fromJson(json as Map<String, dynamic>);
}
