import 'dart:async';

import 'package:cached_network_image/cached_network_image.dart';
import 'package:material_ui/material_ui.dart';

import '../../models/models.dart';
import '../../services/api.dart';
import '../../services/search_history.dart';
import '../detail/watch_screen.dart';

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
  void initState() {
    super.initState();
    // History is restored once from storage; the shared notifier keeps it
    // after the app is reopened.
    loadSearchHistory();
  }

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
    setState(() {}); // reflect the clear (×) button
    _debounce = Timer(const Duration(milliseconds: 500), () => _search(query));
  }

  /// Typing only searches; history is written on submit (Enter), never by the
  /// debounce timer.
  void _submit(String value) {
    _debounce?.cancel();
    final query = value.trim();
    if (query.isEmpty) return;
    unawaited(addSearchHistory(query));
    _search(query);
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

  void _clear() {
    _debounce?.cancel();
    _generation++;
    _textController.clear();
    setState(() {
      _results = null;
      _loading = false;
      _error = null;
    });
  }

  /// Re-runs the current query (pull-to-refresh or the app-bar action).
  Future<void> _refresh() async {
    final query = _textController.text.trim();
    if (query.isEmpty) return;
    _debounce?.cancel();
    await _search(query);
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: TextField(
          controller: _textController,
          textInputAction: TextInputAction.search,
          decoration: InputDecoration(
            hintText: 'Cari anime...',
            border: InputBorder.none,
            suffixIcon: _textController.text.isEmpty
                ? null
                : IconButton(
                    onPressed: _clear,
                    tooltip: 'Hapus pencarian',
                    icon: const Icon(Icons.close, size: 18),
                  ),
          ),
          onChanged: _onChanged,
          onSubmitted: _submit,
        ),
        actions: [
          IconButton(
            onPressed: _textController.text.trim().isEmpty ? null : _refresh,
            tooltip: 'Muat ulang',
            icon: const Icon(Icons.refresh),
          ),
        ],
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
      return ValueListenableBuilder<List<String>>(
        valueListenable: searchHistory,
        builder: (context, history, _) {
          if (history.isEmpty) {
            return const Center(child: Text('Ketik judul anime untuk mencari'));
          }
          return ListView(
            children: [
              const ListTile(title: Text('Riwayat pencarian')),
              for (final query in history)
                ListTile(
                  leading: const Icon(Icons.history),
                  title: Text(query),
                  trailing: IconButton(
                    onPressed: () => removeSearchHistory(query),
                    tooltip: 'Hapus riwayat',
                    icon: const Icon(Icons.close, size: 18),
                  ),
                  onTap: () {
                    _debounce?.cancel();
                    _textController.text = query;
                    _search(query);
                  },
                ),
            ],
          );
        },
      );
    }
    if (results.isEmpty) {
      return const Center(child: Text('Tidak ada hasil'));
    }
    return RefreshIndicator(
      onRefresh: _refresh,
      child: ListView.separated(
        physics: const AlwaysScrollableScrollPhysics(),
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
                child: CachedNetworkImage(
                  imageUrl: result.imageUrl,
                  fit: BoxFit.cover,
                  errorWidget: (_, _, _) =>
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
      ),
    );
  }
}
