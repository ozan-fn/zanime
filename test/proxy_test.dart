import 'package:flutter_test/flutter_test.dart';
import 'package:test1/services/proxy.dart';

void main() {
  test('proxiedUrl routes through the local server and keeps provider headers', () {
    final uri = Uri.parse(
      proxiedUrl(
        'https://cdn.example.com/hls/index.m3u8',
        headers: {'Referer': 'https://ultracloud.cc/', 'Origin': 'https://ultracloud.cc'},
      ),
    );

    expect(uri.host, 'localhost');
    expect(uri.path, '/stream');
    expect(
      uri.queryParameters['url'],
      'https://cdn.example.com/hls/index.m3u8',
    );
    expect(uri.queryParameters['referer'], 'https://ultracloud.cc/');
    expect(uri.queryParameters['origin'], 'https://ultracloud.cc');
  });
}
