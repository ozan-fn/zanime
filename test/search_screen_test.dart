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
}
