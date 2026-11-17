import 'package:flutter_test/flutter_test.dart';
import 'package:material_ui/material_ui.dart';
import 'package:test1/screens/home/widgets/anime_banner.dart';
import 'package:test1/models/models.dart';

void main() {
  const items = [
    AnimeBannerData(
      title: 'Hello, I am a Witch and my Crush Wants me to Make a Love Potion!',
      imageUrl: 'https://s4.anilist.co/file/anilistcdn/media/anime/banner/207191-xYgpkCDnXc5m.jpg',
      genres: ['Fantasy', 'Romance'],
      year: 2026,
      episodes: 12,
      aired: 7,
    ),
    AnimeBannerData(
      title: 'The Apothecary Diaries Season 3',
      imageUrl: 'https://s4.anilist.co/file/anilistcdn/media/anime/banner/195516-B5M0cPaQbQXI.jpg',
      genres: ['Drama', 'Mystery'],
      year: 2026,
      episodes: 12,
      aired: 7,
    ),
  ];

  testWidgets('banner carousel renders and swipes through the array', (
    tester,
  ) async {
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(body: AnimeBanner(future: Future.value(items))),
      ),
    );
    await tester.pump();

    expect(find.text(items[0].title), findsOneWidget);
    expect(find.text('Fantasy · Romance · 2026 · 7/12 eps'), findsOneWidget);

    await tester.drag(find.byType(PageView), const Offset(-500, 0));
    await tester.pumpAndSettle();

    expect(find.text(items[1].title), findsOneWidget);
    expect(find.text('Drama · Mystery · 2026 · 7/12 eps'), findsOneWidget);
  });
}
