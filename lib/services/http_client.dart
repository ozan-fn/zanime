import 'dart:convert';
import 'dart:io';

import 'package:flutter_curl/flutter_curl.dart' as curl;

/// Transport failure or a non-success status; callers surface the message.
class ApiException implements Exception {
  ApiException(this.message, [this.uri]);

  final String message;
  final Uri? uri;

  @override
  String toString() => message;
}

/// Fully buffered response, body decoded as UTF-8.
class ApiResponse {
  ApiResponse({required this.statusCode, required this.body});

  final int statusCode;
  final String body;
}

/// One HTTP client for the whole app: each libcurl client owns an engine
/// isolate and a connection pool, and starting it is slow enough to jank.
class ApiClient {
  final curl.Client _curl = curl.Client();
  Future<void>? _init;

  Future<ApiResponse> get(Uri url, {Map<String, String>? headers}) =>
      send('GET', url, headers: headers);

  Future<ApiResponse> post(
    Uri url, {
    Map<String, String>? headers,
    Object? body,
  }) => send('POST', url, headers: headers, body: body);

  /// [body] is JSON text, or a map sent form-encoded.
  Future<ApiResponse> send(
    String method,
    Uri url, {
    Map<String, String>? headers,
    Object? body,
  }) async {
    final text = switch (body) {
      null => null,
      Map() => Uri(queryParameters: body.cast<String, String>()).query,
      _ => '$body',
    };
    // libcurl ships no desktop binary, and the plugin only attaches a body to
    // POST/PUT -- MAL saves with PATCH. ponytail: dart:io fallback, drop it
    // once the plugin covers both.
    if (!_hasLibcurl || (text != null && method != 'POST' && method != 'PUT')) {
      return _ioSend(method, url, headers: headers, body: text);
    }
    await (_init ??= _curl.init());
    final response = await _curl.send(
      curl.Request(
        url: '$url',
        method: method,
        headers: headers ?? const {},
        body: text == null ? null : curl.RequestBody.string(text),
      ),
    );
    if (response.errorCode != null) {
      throw ApiException(
        response.errorMessage ?? 'HTTP ${response.errorCode}',
        url,
      );
    }
    return ApiResponse(statusCode: response.statusCode, body: response.text());
  }

  static bool get _hasLibcurl =>
      Platform.isAndroid || Platform.isIOS || Platform.isMacOS;
}

Future<ApiResponse> _ioSend(
  String method,
  Uri url, {
  Map<String, String>? headers,
  String? body,
}) async {
  final client = HttpClient();
  try {
    final request = await client.openUrl(method, url);
    headers?.forEach(request.headers.set);
    if (body != null) {
      final bytes = utf8.encode(body);
      request.contentLength = bytes.length;
      request.add(bytes);
    }
    final response = await request.close();
    return ApiResponse(
      statusCode: response.statusCode,
      body: await response
          .transform(const Utf8Decoder(allowMalformed: true))
          .join(),
    );
  } finally {
    client.close();
  }
}

final ApiClient sharedHttpClient = ApiClient();
