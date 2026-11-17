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
    expect(find.byType(NavigationDestination), findsNWidgets(4));
  });

  testWidgets('search tab does not open the keyboard', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(const MyApp());

    expect(find.byType(TextField), findsNothing);

    await tester.tap(find.byIcon(Icons.search));
    await tester.pump();

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

    await tester.tap(find.byIcon(Icons.search));
    await tester.pump();
    await tester.enterText(find.byType(TextField), 'frieren');
    await tester.pump(const Duration(milliseconds: 600));
    await tester.pump();
    expect(find.text('Frieren'), findsOneWidget);

    await tester.tap(find.byIcon(Icons.home_outlined));
    await tester.pump();
    expect(find.text('Frieren'), findsNothing);

    await tester.tap(find.byIcon(Icons.search));
    await tester.pump();
    expect(find.text('Frieren'), findsOneWidget);
    expect(find.text('frieren'), findsOneWidget, reason: 'query is kept');
  });
}
