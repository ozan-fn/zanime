import 'package:flutter_test/flutter_test.dart';
import 'package:material_ui/material_ui.dart';
import 'package:test1/components/update_screen.dart';
import 'package:test1/services/app_update.dart';

void main() {
  test('shortVersion keeps only major.minor.patch', () {
    expect(shortVersion('v1.0.1+8'), '1.0.1');
    expect(shortVersion('1.2.3'), '1.2.3');
    expect(shortVersion('v2.0.0-beta.1'), '2.0.0');
    expect(shortVersion('nightly'), isNull);
    expect(parseVersion('v1.0.1+8'), [1, 0, 1]);
  });

  test('version comparison ignores build metadata', () {
    expect(isNewerVersion('v1.0.1+8', '1.0.1'), isFalse);
    expect(isNewerVersion('v1.0.1+9', '1.0.1'), isFalse);
    expect(isNewerVersion('1.1.0', '1.0.1'), isTrue);
    expect(isNewerVersion('1.0.0', '1.0.1'), isFalse);
    expect(isNewerVersion('2.0.0', '1.9.9'), isTrue);
    expect(isNewerVersion('nightly', '1.0.1'), isFalse);
  });

  testWidgets('UpdateScreen lists installed and latest versions', (
    tester,
  ) async {
    await tester.pumpWidget(
      MaterialApp(
        home: UpdateScreen(
          loadVersion: () async => '1.0.1',
          loadRelease: () async => const AppRelease(
            version: '1.1.0',
            name: 'Zanime v1.1.0',
            notes: 'Full Changelog',
            url: 'https://github.com/ozan-fn/zanime/releases/tag/v1.1.0',
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Cek update'), findsOneWidget);
    expect(find.text('Versi terpasang'), findsOneWidget);
    expect(find.text('1.0.1'), findsOneWidget);
    expect(find.text('Versi terbaru'), findsOneWidget);
    expect(find.text('1.1.0'), findsOneWidget);
    expect(find.text('Update tersedia'), findsOneWidget);
    expect(find.text('Versi 1.0.1 → 1.1.0'), findsOneWidget);
    expect(find.text('Buka halaman rilis'), findsOneWidget);
    expect(find.text(releasesUrl), findsOneWidget);
    expect(find.text('Catatan rilis'), findsOneWidget);
  });

  testWidgets('UpdateScreen ignores the release build number', (tester) async {
    await tester.pumpWidget(
      MaterialApp(
        home: UpdateScreen(
          loadVersion: () async => '1.0.1',
          loadRelease: () async => const AppRelease(
            version: '1.0.1',
            name: 'Zanime v1.0.1',
            notes: '',
            url: '',
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Sudah versi terbaru'), findsOneWidget);
    expect(find.text('Update tersedia'), findsNothing);
    expect(find.text('Buka halaman rilis'), findsNothing);
  });

  testWidgets('UpdateScreen offers a retry when the check fails', (
    tester,
  ) async {
    await tester.pumpWidget(
      MaterialApp(
        home: UpdateScreen(
          loadVersion: () async => '1.0.1',
          loadRelease: () async => throw Exception('offline'),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Gagal mengecek update'), findsOneWidget);
    expect(find.text('1.0.1'), findsOneWidget, reason: 'still shows app version');
    expect(find.byTooltip('Periksa lagi'), findsOneWidget);
  });
}
