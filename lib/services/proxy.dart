/// Local Go server base URL. On a device/emulator, `adb reverse tcp:8080
/// tcp:8080` exposes the host server on the device's localhost.
const String serverBase = 'http://localhost:8080';

/// Streams and subtitles are relayed through the local server so provider
/// Origin/Referer headers can be added and HLS manifests rewritten.
String proxiedUrl(String url, {Map<String, String>? headers}) {
  final query = <String, String>{'url': url};
  for (final entry in (headers ?? const <String, String>{}).entries) {
    final key = entry.key.toLowerCase();
    if (key == 'referer' || key == 'origin') query[key] = entry.value;
  }
  return Uri.parse('$serverBase/stream')
      .replace(queryParameters: query)
      .toString();
}
