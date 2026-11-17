import 'package:material_ui/material_ui.dart';
import 'package:media_kit/media_kit.dart';

import 'screens/home/home_shell.dart';
import 'services/app_settings.dart';
import 'services/mini_player.dart';

const _zinc950 = Color(0xFF09090B);
const _zinc900 = Color(0xFF18181B);
const _zinc800 = Color(0xFF27272A);
const _zinc700 = Color(0xFF3F3F46);
const _zinc400 = Color(0xFFA1A1AA);
const _zinc100 = Color(0xFFF4F4F5);

/// 3px corners everywhere, matching the rest of the UI.
const _shape3 = RoundedRectangleBorder(
  borderRadius: BorderRadius.all(Radius.circular(3)),
);

void main() {
  WidgetsFlutterBinding.ensureInitialized();
  MediaKit.ensureInitialized();
  loadSubtitleStyle();
  runApp(const MyApp());
}

class MyApp extends StatelessWidget {
  const MyApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'Zanime',
      navigatorKey: navigatorKey,
      theme: ThemeData(
        brightness: Brightness.dark,
        scaffoldBackgroundColor: _zinc950,
        colorScheme: const ColorScheme.dark(
          primary: _zinc100,
          onPrimary: _zinc950,
          surface: _zinc900,
          onSurface: _zinc100,
          outline: _zinc700,
          outlineVariant: _zinc800,
        ),
        dividerTheme: const DividerThemeData(color: _zinc800),
        appBarTheme: const AppBarTheme(
          backgroundColor: _zinc950,
          foregroundColor: _zinc100,
        ),
        dialogTheme: const DialogThemeData(
          backgroundColor: _zinc900,
          shape: _shape3,
        ),
        cardTheme: const CardThemeData(shape: _shape3),
        popupMenuTheme: const PopupMenuThemeData(shape: _shape3),
        bottomSheetTheme: const BottomSheetThemeData(shape: _shape3),
        snackBarTheme: const SnackBarThemeData(shape: _shape3),
        filledButtonTheme: FilledButtonThemeData(
          style: FilledButton.styleFrom(shape: _shape3),
        ),
        outlinedButtonTheme: OutlinedButtonThemeData(
          style: OutlinedButton.styleFrom(shape: _shape3),
        ),
        elevatedButtonTheme: ElevatedButtonThemeData(
          style: ElevatedButton.styleFrom(shape: _shape3),
        ),
        textButtonTheme: TextButtonThemeData(
          style: TextButton.styleFrom(shape: _shape3),
        ),
        iconButtonTheme: IconButtonThemeData(
          style: IconButton.styleFrom(shape: _shape3),
        ),
        segmentedButtonTheme: SegmentedButtonThemeData(
          style: ButtonStyle(shape: const WidgetStatePropertyAll(_shape3)),
        ),
        // Flat zinc bar: selected tab is just a brighter icon/label, no box.
        navigationBarTheme: NavigationBarThemeData(
          backgroundColor: _zinc950,
          indicatorColor: Colors.transparent,
          indicatorShape: const RoundedRectangleBorder(
            borderRadius: BorderRadius.zero,
          ),
          surfaceTintColor: Colors.transparent,
          // Kills the pressed overlay box M3 draws behind the icon.
          overlayColor: const WidgetStatePropertyAll(Colors.transparent),
          elevation: 0,
          // shadcn-like compact bar.
          height: 48,
          iconTheme: WidgetStateProperty.resolveWith(
            (states) => IconThemeData(
              size: 22,
              color: states.contains(WidgetState.selected)
                  ? _zinc100
                  : _zinc400,
            ),
          ),
          labelTextStyle: WidgetStateProperty.resolveWith(
            (states) => TextStyle(
              fontSize: 12,
              color: states.contains(WidgetState.selected)
                  ? _zinc100
                  : _zinc400,
            ),
          ),
        ),
      ),
      home: const HomeShell(),
    );
  }
}
