import 'package:flutter_test/flutter_test.dart';
import 'package:material_ui/material_ui.dart';
import 'package:test1/screens/watching/watching_screen.dart';
import 'package:test1/services/anilist_auth.dart';

void main() {
  testWidgets('Watching prompts login when there is no token', (tester) async {
    var opened = false;
    await tester.pumpWidget(
      MaterialApp(
        home: WatchingScreen(
          loader: () async => null,
          onLogin: () => opened = true,
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Watching'), findsOneWidget);
    expect(
      find.text(
        'Login AniList atau MyAnimeList untuk melihat anime yang sedang '
        'ditonton',
      ),
      findsOneWidget,
    );
    await tester.tap(find.text('Buka Setting'));
    expect(opened, isTrue);
  });

  testWidgets('Watching lists progress and opens the next episode', (
    tester,
  ) async {
    await tester.pumpWidget(
      MaterialApp(
        home: WatchingScreen(
          loader: () async => const [
            WatchingAnime(
              mediaId: 21,
              title: 'One Piece',
              imageUrl: '',
              progress: 1085,
              totalEpisodes: 1100,
              nextEpisode: 1086,
              format: 'TV',
              year: 1999,
            ),
          ],
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('One Piece'), findsOneWidget);
    expect(find.text('TV · 1999 · 1085/1100'), findsOneWidget);
    expect(find.text('Eps 1086'), findsOneWidget);
  });

  testWidgets('Watching shows an empty state', (tester) async {
    await tester.pumpWidget(
      MaterialApp(home: WatchingScreen(loader: () async => const [])),
    );
    await tester.pumpAndSettle();

    expect(find.text('Belum ada anime berstatus watching'), findsOneWidget);
  });
}
