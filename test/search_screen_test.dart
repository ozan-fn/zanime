import 'package:flutter_test/flutter_test.dart';
import 'package:material_ui/material_ui.dart';
import 'package:test1/components/search_screen.dart';
import 'package:test1/models/models.dart';

const _frierenJson = {
  'animeId': 'wHWR9m4VQ8GLW4GP9_ZTblQXn9f0phB8',
  'title': 'Frieren: Beyond Journey’s End',
  'format': 'TV',
  'year': 2023,
  'episodes': 28,
  'score': 91,
  'imageUrl':
      'https://s4.anilist.co/file/anilistcdn/media/anime/cover/large/bx154587-gHSraOSa0nA0.jpg',
};

void main() {
  test('SearchResult parses the miruro search item', () {
    final result = SearchResult.fromJson(_frierenJson);

    expect(result.animeId, 'wHWR9m4VQ8GLW4GP9_ZTblQXn9f0phB8');
    expect(result.title, 'Frieren: Beyond Journey’s End');
    expect(result.imageUrl, startsWith('https://'));
    expect(result.subtitle, 'TV · 2023 · 28 eps · ★ 91');
  });

  testWidgets('SearchScreen autosuggests after the debounce', (tester) async {
    final queries = <String>[];
    Future<List<SearchResult>> fakeSearch(String query) async {
      queries.add(query);
      return [SearchResult.fromJson(_frierenJson)];
    }

    await tester.pumpWidget(
      MaterialApp(home: SearchScreen(search: fakeSearch)),
    );

    await tester.enterText(find.byType(TextField), 'f');
    await tester.pump(const Duration(milliseconds: 100));
    await tester.enterText(find.byType(TextField), 'frie');
    await tester.pump(const Duration(milliseconds: 100));
    await tester.enterText(find.byType(TextField), 'frieren');
    expect(queries, isEmpty);
    await tester.pump(const Duration(milliseconds: 600));
    await tester.pump();

    expect(queries, ['frieren']);
    expect(find.text('Frieren: Beyond Journey’s End'), findsOneWidget);
    expect(find.text('TV · 2023 · 28 eps · ★ 91'), findsOneWidget);
  });

  testWidgets('SearchScreen shows the empty, error and no-result states', (
    tester,
  ) async {
    var failing = false;
    var empty = true;
    Future<List<SearchResult>> search(String query) async {
      if (failing) throw Exception('boom');
      return empty ? const [] : [SearchResult.fromJson(_frierenJson)];
    }

    await tester.pumpWidget(MaterialApp(home: SearchScreen(search: search)));
    expect(find.text('Ketik judul anime untuk mencari'), findsOneWidget);

    await tester.enterText(find.byType(TextField), 'frieren');
    await tester.pump(const Duration(milliseconds: 600));
    await tester.pump();
    expect(find.text('Tidak ada hasil'), findsOneWidget);

    empty = false;
    await tester.enterText(find.byType(TextField), 'frieren ');
    await tester.pump(const Duration(milliseconds: 600));
    await tester.pump();
    expect(find.text('Frieren: Beyond Journey’s End'), findsOneWidget);

    failing = true;
    await tester.enterText(find.byType(TextField), 'frieren x');
    await tester.pump(const Duration(milliseconds: 600));
    await tester.pump();
    expect(find.text('Gagal mencari'), findsOneWidget);
  });

  testWidgets('SearchScreen clears results when the query is emptied', (
    tester,
  ) async {
    await tester.pumpWidget(
      MaterialApp(
        home: SearchScreen(
          search: (query) async => [SearchResult.fromJson(_frierenJson)],
        ),
      ),
    );

    await tester.enterText(find.byType(TextField), 'frieren');
    await tester.pump(const Duration(milliseconds: 600));
    await tester.pump();
    expect(find.text('Frieren: Beyond Journey’s End'), findsOneWidget);

    await tester.enterText(find.byType(TextField), '');
    await tester.pump();
    expect(find.text('Frieren: Beyond Journey’s End'), findsNothing);
    expect(find.text('Riwayat pencarian'), findsOneWidget);
    expect(find.text('frieren'), findsOneWidget);
  });

  testWidgets('SearchScreen re-runs a query from the history', (tester) async {
    final queries = <String>[];
    await tester.pumpWidget(
      MaterialApp(
        home: SearchScreen(
          search: (query) async {
            queries.add(query);
            return [SearchResult.fromJson(_frierenJson)];
          },
        ),
      ),
    );

    await tester.enterText(find.byType(TextField), 'frieren');
    await tester.pump(const Duration(milliseconds: 600));
    await tester.pump();
    await tester.enterText(find.byType(TextField), '');
    await tester.pump();

    await tester.tap(find.text('frieren'));
    await tester.pump();
    await tester.pump();

    expect(queries, ['frieren', 'frieren']);
    expect(find.text('Frieren: Beyond Journey’s End'), findsOneWidget);
  });
}
