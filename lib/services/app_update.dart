import 'dart:convert';

import 'package:http/http.dart' as http;
import 'package:package_info_plus/package_info_plus.dart';
import 'package:url_launcher/url_launcher.dart';

import 'http_client.dart';

const githubOwner = 'ozan-fn';
const githubRepo = 'zanime';

/// Where users are sent when an update is available.
const releasesUrl =
    'https://github.com/$githubOwner/$githubRepo/releases/latest';

/// A GitHub release, reduced to what the update check shows.
class AppRelease {
  const AppRelease({
    required this.version,
    required this.name,
    required this.notes,
    required this.url,
  });

  /// Tag reduced to `major.minor.patch`.
  final String version;
  final String name;
  final String notes;
  final String url;
}

/// Latest published (non-draft, non-prerelease) release of the repo.
Future<AppRelease> fetchLatestRelease() async {
  final response = await sharedHttpClient.get(
    Uri.parse('https://api.github.com/repos/$githubOwner/$githubRepo/releases/latest'),
    headers: const {
      'Accept': 'application/vnd.github+json',
      'User-Agent': 'Zanime',
    },
  );
  if (response.statusCode != 200) {
    throw http.ClientException('GitHub HTTP ${response.statusCode}');
  }
  final json = jsonDecode(response.body) as Map<String, dynamic>;
  final tag = json['tag_name'] as String? ?? '';
  return AppRelease(
    version: shortVersion(tag) ?? tag,
    name: json['name'] as String? ?? tag,
    notes: json['body'] as String? ?? '',
    url: json['html_url'] as String? ?? '',
  );
}

/// Installed app version, e.g. `1.0.1` (build number excluded).
Future<String> installedAppVersion() async {
  final info = await PackageInfo.fromPlatform();
  return shortVersion(info.version) ?? info.version;
}

/// `major.minor.patch` from a tag like `v1.0.1+8`; null when unparseable.
List<int>? parseVersion(String value) {
  final match = RegExp(r'(\d+)\.(\d+)\.(\d+)').firstMatch(value);
  if (match == null) return null;
  return [for (var i = 1; i <= 3; i++) int.parse(match[i]!)];
}

/// Build metadata (`+8`) and a leading `v` are ignored, so `v1.0.1+8`
/// compares equal to `1.0.1`.
String? shortVersion(String value) {
  final parts = parseVersion(value);
  return parts?.join('.');
}

/// True when [candidate] is a newer major.minor.patch than [current].
bool isNewerVersion(String candidate, String current) {
  final a = parseVersion(candidate);
  final b = parseVersion(current);
  if (a == null || b == null) return false;
  for (var i = 0; i < 3; i++) {
    if (a[i] != b[i]) return a[i] > b[i];
  }
  return false;
}

/// Result of one update check.
class AppUpdateInfo {
  const AppUpdateInfo({required this.installed, required this.release});

  final String installed;
  final AppRelease release;

  bool get available => isNewerVersion(release.version, installed);
}

/// How long a check result is reused before hitting GitHub again.
const updateCheckInterval = Duration(minutes: 1);

AppUpdateInfo? _cachedCheck;
DateTime? _cachedAt;

/// Update check shared by the settings row and the update page; reuses the
/// last result for [updateCheckInterval] and null when offline.
Future<AppUpdateInfo?> checkForUpdate() async {
  final now = DateTime.now();
  final cached = _cachedCheck;
  final at = _cachedAt;
  if (cached != null && at != null && now.difference(at) < updateCheckInterval) {
    return cached;
  }
  try {
    final installed = await installedAppVersion();
    final release = await fetchLatestRelease();
    final info = AppUpdateInfo(installed: installed, release: release);
    _cachedCheck = info;
    _cachedAt = now;
    return info;
  } catch (_) {
    return null;
  }
}

/// Stores a check result so the settings row can reuse it.
void cacheUpdateInfo(String installed, AppRelease release) {
  _cachedCheck = AppUpdateInfo(installed: installed, release: release);
  _cachedAt = DateTime.now();
}

/// Sends the user to the latest release page. Returns false if no browser.
Future<bool> openLatestRelease() =>
    launchUrl(Uri.parse(releasesUrl), mode: LaunchMode.externalApplication);
