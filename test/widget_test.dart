import 'package:flutter_test/flutter_test.dart';
import 'package:material_ui/material_ui.dart';

import 'package:test1/main.dart';
import 'package:test1/models/models.dart';

void main() {
  testWidgets('home shows the Zanime title and four navigation tabs', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(const MyApp());

    expect(find.text('Zanime'), findsOneWidget);
    expect(find.byType(NavigationBar), findsOneWidget);
    for (final label in ['Home', 'Search', 'History', 'Setting']) {
      expect(find.text(label), findsOneWidget);
    }
  });

  testWidgets('search tab does not open the keyboard', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(const MyApp());

    expect(find.byIcon(Icons.search), findsOneWidget, reason: 'tab icon only');
    expect(find.byType(TextField), findsNothing);

    await tester.tap(find.text('Search'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));

    expect(find.byType(TextField), findsOneWidget);
    expect(tester.testTextInput.isVisible, isFalse);
    expect(
      find.byType(NavigationBar),
      findsOneWidget,
      reason: 'tab bar stays visible on the search tab',
    );
  });

  testWidgets('search keeps the query and results across tabs', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(
      MaterialApp(
        home: HomeShell(
          search: (query) async => [
            const SearchResult(
              animeId: 'id1',
              title: 'Frieren',
              imageUrl: 'https://example.com/c.jpg',
            ),
          ],
        ),
      ),
    );

    await tester.tap(find.text('Search'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));
    await tester.enterText(find.byType(TextField), 'frieren');
    await tester.pump(const Duration(milliseconds: 600));
    await tester.pump();
    expect(find.text('Frieren'), findsOneWidget);

    await tester.tap(find.text('Home'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));
    expect(find.text('Frieren'), findsNothing);

    await tester.tap(find.text('Search'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));
    expect(find.text('Frieren'), findsOneWidget);
    expect(find.text('frieren'), findsOneWidget, reason: 'query is kept');
  });
}
