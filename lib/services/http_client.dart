import 'dart:io';

import 'package:cronet_http/cronet_http.dart';
import 'package:http/http.dart' as http;
import 'package:http/io_client.dart';

/// Android uses Cronet (Chromium's network stack); other platforms fall back
/// to dart:io.
http.Client createHttpClient() {
  if (Platform.isAndroid) {
    final engine = CronetEngine.build(
      cacheMode: CacheMode.memory,
      cacheMaxSize: 2 * 1024 * 1024,
    );
    return CronetClient.fromCronetEngine(engine, closeEngine: true);
  }
  return IOClient(HttpClient());
}

/// One client for the whole app: every Cronet engine spawns its own threads
/// and cache, and building it is slow enough to jank the first screen.
final http.Client sharedHttpClient = createHttpClient();
