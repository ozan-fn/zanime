import 'package:flutter_test/flutter_test.dart';
import 'package:material_ui/material_ui.dart';
import 'package:test1/components/settings_screen.dart';
import 'package:test1/services/image_cache.dart';

void main() {
  test('formatCacheSize uses B, KB and MB', () {
    expect(formatCacheSize(0), '0 B');
    expect(formatCacheSize(1023), '1023 B');
    expect(formatCacheSize(2048), '2.0 KB');
    expect(formatCacheSize(5 * 1024 * 1024), '5.0 MB');
  });

  testWidgets('Setting shows the cache size and clears it', (tester) async {
    var cleared = false;

    await tester.pumpWidget(
      MaterialApp(
        home: SettingsScreen(
          cacheSize: () async => 5 * 1024 * 1024,
          clearCache: () async => cleared = true,
        ),
      ),
    );
    await tester.pump();

    expect(find.text('Cache gambar'), findsOneWidget);
    expect(find.text('5.0 MB'), findsOneWidget);

    await tester.tap(find.text('Hapus cache'));
    await tester.pump();
    await tester.pump();

    expect(cleared, isTrue);
    expect(find.text('0 B'), findsOneWidget);
    expect(find.text('Cache dihapus'), findsOneWidget);
  });
}
