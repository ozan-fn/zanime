import 'package:shared_preferences/shared_preferences.dart';

import 'anilist_auth.dart';
import 'api.dart';
import 'mal_auth.dart';

/// Anime list service the user is currently signed in with.
enum ListService { anilist, myanimelist }

const listServiceKey = 'list_service';
const _anilistValue = 'anilist';
const _malValue = 'myanimelist';

Future<ListService> loadActiveListService() async {
  try {
    final prefs = await SharedPreferences.getInstance();
    return prefs.getString(listServiceKey) == _malValue
        ? ListService.myanimelist
        : ListService.anilist;
  } catch (_) {
    return ListService.anilist;
  }
}

Future<void> setActiveListService(ListService service) async {
  try {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(
      listServiceKey,
      service == ListService.myanimelist ? _malValue : _anilistValue,
    );
  } catch (_) {
    // The in-memory choice still drives this session.
  }
}

Future<AniListProfile?> loadActiveProfile() async {
  final service = await loadActiveListService();
  return service == ListService.myanimelist
      ? loadMalProfile()
      : loadAniListProfile();
}

Future<Map<String, List<WatchingAnime>>?> fetchActiveLists() async {
  final service = await loadActiveListService();
  return service == ListService.myanimelist
      ? fetchMalLists()
      : fetchAniListLists();
}

Future<List<WatchingAnime>?> fetchActiveWatching() async {
  final service = await loadActiveListService();
  return service == ListService.myanimelist
      ? fetchMalWatching()
      : fetchAniListWatching();
}

/// List status for the active service. MAL needs a title lookup because
/// Miruro only exposes AniList ids.
Future<String?> fetchActiveStatus({
  required String title,
  int? aniListId,
}) async {
  final service = await loadActiveListService();
  if (service == ListService.myanimelist) {
    final malId = await resolveMalAnimeId(title, aniListId: aniListId);
    return malId == null ? null : fetchMalStatus(malId);
  }
  if (aniListId == null) return null;
  return fetchAniListStatus(aniListId);
}

Future<bool> saveActiveEntry({
  required String title,
  required String status,
  int? aniListId,
}) async {
  final service = await loadActiveListService();
  if (service == ListService.myanimelist) {
    final malId = await resolveMalAnimeId(title, aniListId: aniListId);
    return malId == null ? false : saveMalEntry(malId, status);
  }
  if (aniListId == null) return false;
  return saveAniListEntry(aniListId, status);
}

Future<bool> removeActiveEntry({required String title, int? aniListId}) async {
  final service = await loadActiveListService();
  if (service == ListService.myanimelist) {
    final malId = await resolveMalAnimeId(title, aniListId: aniListId);
    return malId == null ? false : removeMalEntry(malId);
  }
  if (aniListId == null) return false;
  return removeAniListEntry(aniListId);
}

Future<void> signOutActive() async {
  final service = await loadActiveListService();
  if (service == ListService.myanimelist) {
    await signOutMal();
  } else {
    await signOutAniList();
  }
}

/// Maps a Watching entry to Miruro using the id for AniList and the title for
/// MAL (its media ids are not AniList ids).
Future<String?> resolveMiruroAnimeIdForActive(String title, int mediaId) async {
  final service = await loadActiveListService();
  return service == ListService.myanimelist
      ? resolveMiruroAnimeIdByTitle(title)
      : resolveMiruroAnimeId(title, mediaId);
}
