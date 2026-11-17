import 'package:flutter_test/flutter_test.dart';
import 'package:material_ui/material_ui.dart';
import 'package:test1/components/settings_screen.dart';
import 'package:test1/services/anilist_auth.dart';
import 'package:test1/services/app_settings.dart';
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
          loadProfile: () async => null,
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

  testWidgets('Setting logs in to AniList and logs out', (tester) async {
    AniListProfile? profile;
    await tester.pumpWidget(
      MaterialApp(
        home: SettingsScreen(
          cacheSize: () async => 0,
          clearCache: () async {},
          loadProfile: () async => profile,
          loadLists: () async => null,
          signIn: () async => profile = const AniListProfile(
            id: 18615,
            name: 'zanime',
            avatarUrl: '',
            animeCount: 42,
            episodesWatched: 510,
          ),
          signOut: () async => profile = null,
        ),
      ),
    );
    await tester.pump();

    expect(find.text('Login dengan AniList'), findsOneWidget);

    await tester.tap(find.text('Login dengan AniList'));
    await tester.pumpAndSettle();

    expect(find.text('zanime'), findsOneWidget);
    expect(find.text('ID 18615 · 42 anime · 510 episode'), findsOneWidget);

    await tester.tap(find.text('Keluar'));
    await tester.pumpAndSettle();

    expect(find.text('Login dengan AniList'), findsOneWidget);
    expect(find.text('Logout dari AniList'), findsOneWidget);
  });

  testWidgets('Profil shows status totals and opens the list page', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(900, 1600);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    await tester.pumpWidget(
      MaterialApp(
        home: SettingsScreen(
          cacheSize: () async => 0,
          clearCache: () async {},
          loadProfile: () async => const AniListProfile(
            id: 1,
            name: 'zanime',
            avatarUrl: '',
          ),
          loadLists: () async => {
            'CURRENT': [
              const WatchingAnime(
                mediaId: 1,
                title: 'Frieren',
                imageUrl: '',
                progress: 3,
                status: 'CURRENT',
              ),
            ],
            'COMPLETED': [
              const WatchingAnime(
                mediaId: 2,
                title: 'Bocchi',
                imageUrl: '',
                progress: 12,
                status: 'COMPLETED',
              ),
              const WatchingAnime(
                mediaId: 3,
                title: 'Dungeon Meshi',
                imageUrl: '',
                progress: 24,
                status: 'COMPLETED',
              ),
            ],
          },
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Watching'), findsOneWidget);
    expect(find.text('Completed'), findsOneWidget);

    await tester.tap(find.text('Completed'));
    await tester.pumpAndSettle();

    expect(find.text('Completed · 2'), findsOneWidget);
    expect(find.text('Bocchi'), findsOneWidget);
    expect(find.text('Dungeon Meshi'), findsOneWidget);
  });

  testWidgets('Profil fits the status grid at a large text scale', (
    tester,
  ) async {
    await tester.pumpWidget(
      MaterialApp(
        builder: (context, child) => MediaQuery(
          data: MediaQuery.of(
            context,
          ).copyWith(textScaler: const TextScaler.linear(1.3)),
          child: child!,
        ),
        home: SettingsScreen(
          cacheSize: () async => 0,
          clearCache: () async {},
          loadProfile: () async => const AniListProfile(
            id: 1,
            name: 'zanime',
            avatarUrl: '',
          ),
          loadLists: () async => {
            'COMPLETED': [
              const WatchingAnime(
                mediaId: 2,
                title: 'Bocchi',
                imageUrl: '',
                progress: 12,
                status: 'COMPLETED',
              ),
            ],
          },
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(tester.takeException(), isNull);
    expect(find.text('Completed'), findsOneWidget);
  });

  testWidgets('Profil changes the subtitle px size', (tester) async {
    addTearDown(() => subtitlePx.value = defaultSubtitlePx);
    await tester.pumpWidget(
      MaterialApp(
        home: SettingsScreen(
          cacheSize: () async => 0,
          clearCache: () async {},
          loadProfile: () async => null,
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(subtitlePx.value, defaultSubtitlePx);
    expect(find.text('${defaultSubtitlePx.round()} px'), findsWidgets);
    expect(find.text('Huruf tebal'), findsNothing, reason: 'removed');

    tester.widget<Slider>(find.byType(Slider)).onChanged!(maxSubtitlePx);
    await tester.pump();
    expect(subtitlePx.value, maxSubtitlePx);
    expect(find.text('${maxSubtitlePx.round()} px'), findsWidgets);

    tester.widget<Slider>(find.byType(Slider)).onChanged!(minSubtitlePx);
    await tester.pump();
    expect(subtitlePx.value, minSubtitlePx);
  });
}
