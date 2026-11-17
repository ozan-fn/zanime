import 'package:flutter_svg/flutter_svg.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:material_ui/material_ui.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:test1/screens/profile/settings_screen.dart';
import 'package:test1/services/anilist_auth.dart';
import 'package:test1/services/app_settings.dart';
import 'package:test1/services/app_update.dart';
import 'package:test1/services/image_cache.dart';
import 'package:test1/services/list_service.dart';

void main() {
  setUp(() {
    SharedPreferences.setMockInitialValues({});
  });

  test('formatCacheSize uses B, KB and MB', () {
    expect(formatCacheSize(0), '0 B');
    expect(formatCacheSize(1023), '1023 B');
    expect(formatCacheSize(2048), '2.0 KB');
    expect(formatCacheSize(5 * 1024 * 1024), '5.0 MB');
  });

  testWidgets('Setting shows the app cache size and clears it', (tester) async {
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

    expect(find.text('Cache aplikasi'), findsOneWidget);
    expect(find.text('Cache gambar'), findsNothing);
    expect(find.text('5.0 MB'), findsOneWidget);
    expect(find.byIcon(Icons.delete_outline), findsOneWidget);

    await tester.tap(find.byTooltip('Hapus cache'));
    await tester.pumpAndSettle();
    expect(cleared, isFalse, reason: 'asks for confirmation first');
    expect(find.text('Hapus cache aplikasi?'), findsOneWidget);

    await tester.tap(
      find.descendant(
        of: find.byType(AlertDialog),
        matching: find.text('Hapus'),
      ),
    );
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
            bannerUrl: 'https://example.com/banner.jpg',
            animeCount: 42,
            episodesWatched: 510,
          ),
          signOut: () async => profile = null,
        ),
      ),
    );
    await tester.pump();

    expect(find.text('ani'), findsOneWidget);
    expect(find.text('mal'), findsOneWidget);
    expect(find.byType(SvgPicture), findsNWidgets(2));

    await tester.tap(find.text('ani'));
    await tester.pumpAndSettle();

    expect(find.text('zanime'), findsOneWidget);
    expect(find.text('ID 18615 · 42 anime · 510 episode'), findsOneWidget);
    expect(
      tester
          .widget<FilledButton>(find.widgetWithText(FilledButton, 'Keluar'))
          .style
          ?.foregroundColor
          ?.resolve({}),
      const Color(0xFFFEF2F2),
      reason: 'solid shadcn destructive',
    );

    await tester.tap(find.text('Keluar'));
    await tester.pumpAndSettle();
    expect(find.text('Keluar dari AniList?'), findsOneWidget);

    await tester.tap(
      find.descendant(
        of: find.byType(AlertDialog),
        matching: find.text('Keluar'),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('ani'), findsOneWidget);
    expect(find.text('Logout dari AniList'), findsOneWidget);
  });

  testWidgets('Setting logs in to MyAnimeList and logs out', (tester) async {
    AniListProfile? profile;
    var malSignIn = false;
    await tester.pumpWidget(
      MaterialApp(
        home: SettingsScreen(
          cacheSize: () async => 0,
          clearCache: () async {},
          loadProfile: () async => profile,
          loadLists: () async => null,
          loadService: () async => ListService.myanimelist,
          signInMal: () async {
            malSignIn = true;
            profile = const AniListProfile(
              id: 42,
              name: 'maluser',
              avatarUrl: '',
              bannerUrl: 'https://example.com/banner.jpg',
              animeCount: 7,
              episodesWatched: 99,
            );
          },
          signOut: () async => profile = null,
        ),
      ),
    );
    await tester.pump();
    await tester.pump();

    expect(find.text('ani'), findsOneWidget);
    expect(find.text('mal'), findsOneWidget);

    await tester.tap(find.text('mal'));
    await tester.pumpAndSettle();

    expect(malSignIn, isTrue);
    expect(find.text('maluser'), findsOneWidget);
    expect(find.text('ID 42 · 7 anime · 99 episode'), findsOneWidget);

    await tester.tap(find.text('Keluar'));
    await tester.pumpAndSettle();
    expect(find.text('Keluar dari MyAnimeList?'), findsOneWidget);

    await tester.tap(
      find.descendant(
        of: find.byType(AlertDialog),
        matching: find.text('Keluar'),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Logout dari MyAnimeList'), findsOneWidget);
    expect(find.text('ani'), findsOneWidget);
  });

  testWidgets('Profil falls back to the bundled avatar and banner', (
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
          loadProfile: () async =>
              const AniListProfile(id: 1, name: 'zanime', avatarUrl: ''),
          loadLists: () async => null,
          checkUpdate: () async => null,
        ),
      ),
    );
    await tester.pump();
    await tester.pump();

    Image imageWith(String asset) => tester
        .widgetList<Image>(find.byType(Image))
        .firstWhere((image) => (image.image as AssetImage).assetName == asset);

    expect(imageWith('assets/images/profile_fallback.jpg'), isNotNull);
    expect(imageWith('assets/images/banner_fallback.gif'), isNotNull);
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
            bannerUrl: 'https://example.com/banner.jpg',
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
    tester.view.physicalSize = const Size(600, 1200);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    await tester.pumpWidget(
      MaterialApp(
        builder: (context, child) => MediaQuery(
          data: MediaQuery.of(context)
              .copyWith(textScaler: const TextScaler.linear(2)),
          child: child!,
        ),
        home: SettingsScreen(
          cacheSize: () async => 0,
          clearCache: () async {},
          loadProfile: () async => const AniListProfile(
            id: 1,
            name: 'zanime',
            avatarUrl: '',
            bannerUrl: 'https://example.com/banner.jpg',
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

  testWidgets('Profil row links straight to GitHub when an update exists', (
    tester,
  ) async {
    await tester.pumpWidget(
      MaterialApp(
        home: SettingsScreen(
          cacheSize: () async => 0,
          clearCache: () async {},
          loadProfile: () async => null,
          checkUpdate: () async => const AppUpdateInfo(
            installed: '1.0.1',
            release: AppRelease(
              version: '1.1.0',
              name: 'Zanime v1.1.0',
              notes: '',
              url: '',
            ),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Versi 1.1.0 tersedia'), findsOneWidget);
    expect(find.text('Update'), findsOneWidget);
    expect(find.byIcon(Icons.open_in_new), findsOneWidget);
    expect(find.byIcon(Icons.system_update), findsOneWidget);
  });

  testWidgets('Profil row opens the update page when up to date', (
    tester,
  ) async {
    await tester.pumpWidget(
      MaterialApp(
        home: SettingsScreen(
          cacheSize: () async => 0,
          clearCache: () async {},
          loadProfile: () async => null,
          checkUpdate: () async => const AppUpdateInfo(
            installed: '1.1.0',
            release: AppRelease(
              version: '1.1.0',
              name: 'Zanime v1.1.0',
              notes: '',
              url: '',
            ),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Update'), findsNothing);
    expect(
      find.descendant(
        of: find.byKey(const ValueKey('update-tile')),
        matching: find.byIcon(Icons.chevron_right),
      ),
      findsOneWidget,
    );
    expect(find.text('Versi 1.1.0 tersedia'), findsNothing);
  });

  testWidgets('Subtitle and update rows share the same height', (tester) async {
    tester.view.physicalSize = const Size(900, 1600);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

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

    expect(
      tester.getSize(find.byKey(const ValueKey('subtitle-tile'))).height,
      tester.getSize(find.byKey(const ValueKey('update-tile'))).height,
    );
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
