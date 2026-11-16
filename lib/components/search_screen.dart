import 'dart:async';

import 'package:material_ui/material_ui.dart';

import '../models/models.dart';
import '../services/api.dart';
import 'watch_screen.dart';

class SearchScreen extends StatefulWidget {
  const SearchScreen({super.key, this.search});

  /// Injected in tests; defaults to [searchAnime].
  final Future<List<SearchResult>> Function(String query)? search;

  @override
  State<SearchScreen> createState() => _SearchScreenState();
}

class _SearchScreenState extends State<SearchScreen> {
  final _textController = TextEditingController();
  Timer? _debounce;
  List<SearchResult>? _results;
  bool _loading = false;
  String? _error;
  int _generation = 0;

  @override
  void dispose() {
    _debounce?.cancel();
    _textController.dispose();
    super.dispose();
  }

  void _onChanged(String value) {
    _debounce?.cancel();
    _generation++; // drop any in-flight request for an older query
    final query = value.trim();
    if (query.isEmpty) {
      setState(() {
        _results = null;
        _loading = false;
        _error = null;
      });
      return;
    }
    _debounce = Timer(const Duration(milliseconds: 500), () => _search(query));
  }

  Future<void> _search(String query) async {
    final generation = ++_generation;
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final results = await (widget.search ?? searchAnime)(query);
      if (!mounted || generation != _generation) return;
      setState(() {
        _results = results;
        _error = null;
      });
    } catch (e) {
      if (!mounted || generation != _generation) return;
      setState(() => _error = 'Gagal mencari');
    } finally {
      if (mounted && generation == _generation) {
        setState(() => _loading = false);
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: TextField(
          controller: _textController,
          autofocus: true,
          textInputAction: TextInputAction.search,
          decoration: const InputDecoration(
            hintText: 'Cari anime...',
            border: InputBorder.none,
          ),
          onChanged: _onChanged,
          onSubmitted: (value) {
            _debounce?.cancel();
            if (value.trim().isNotEmpty) _search(value.trim());
          },
        ),
      ),
      body: _body(context),
    );
  }

  Widget _body(BuildContext context) {
    if (_loading && _results == null) {
      return const Center(child: CircularProgressIndicator());
    }
    if (_error != null) {
      return Center(child: Text(_error!));
    }
    final results = _results;
    if (results == null) {
      return const Center(child: Text('Ketik judul anime untuk mencari'));
    }
    if (results.isEmpty) {
      return const Center(child: Text('Tidak ada hasil'));
    }
    return ListView.separated(
      itemCount: results.length,
      separatorBuilder: (_, _) => const Divider(height: 1),
      itemBuilder: (context, index) {
        final result = results[index];
        return ListTile(
          leading: ClipRRect(
            borderRadius: BorderRadius.circular(3),
            child: SizedBox(
              width: 40,
              height: 56,
              child: Image.network(
                result.imageUrl,
                fit: BoxFit.cover,
                errorBuilder: (_, _, _) =>
                    const ColoredBox(color: Colors.black),
              ),
            ),
          ),
          title: Text(
            result.title,
            maxLines: 2,
            overflow: TextOverflow.ellipsis,
          ),
          subtitle: Text(result.subtitle),
          onTap: () => Navigator.of(context).push(
            MaterialPageRoute<void>(
              builder: (_) => WatchScreen(animeId: result.animeId),
            ),
          ),
        );
      },
    );
  }
}
