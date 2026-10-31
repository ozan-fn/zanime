package tmdb

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fakeUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/catalog/"):
			_ = json.NewEncoder(w).Encode(map[string]any{"metas": []map[string]any{
				{"id": "tt0145487", "name": "Spider-Man", "releaseInfo": "2002", "poster": "p.jpg"},
				{"id": "nope", "name": "bukan tt, harus tersaring", "releaseInfo": "2020"},
			}})
		case strings.HasPrefix(r.URL.Path, "/meta/"):
			_ = json.NewEncoder(w).Encode(map[string]any{"meta": map[string]any{
				"id":          "tt0145487",
				"moviedb_id":  557, // number: Cinemeta kadang kirim angka
				"name":        "Spider-Man",
				"releaseInfo": "2002",
				"description": "web slinger",
				"runtime":     "121 min",
				"genres":      []string{"Action"},
				"imdbRating":  7.3, // rating bisa number
				"poster":      "p.jpg",
				"videos": []map[string]any{
					{"season": 1, "episode": 1, "name": "Pilot", "thumbnail": "th.jpg", "released": "2002-01-01"},
				},
			}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

// TestSearch: /catalog di-map jadi []Item dengan hanya id tt… disaring, dan
// year/game diambil dari releaseInfo.
func TestSearch(t *testing.T) {
	t.Chdir(t.TempDir())
	upstream := fakeUpstream(t)
	defer upstream.Close()
	c := New()
	c.SetBase(upstream.URL)

	items, err := c.Search(t.Context(), "movie", "spiderman")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("item = %d, want 1 (yang bukan tt… tersaring)", len(items))
	}
	it := items[0]
	if it.ID != "tt0145487" || it.Name != "Spider-Man" || it.Year != "2002" || it.Type != "movie" || it.Poster != "p.jpg" {
		t.Fatalf("item salah: %+v", it)
	}
}

// TestSearchTabSalah: tab editor salah langsung error tanpa menembak upstream.
func TestSearchTabSalah(t *testing.T) {
	t.Chdir(t.TempDir())
	c := New()
	if _, err := c.Search(t.Context(), "anime", "x"); err == nil || !strings.Contains(err.Error(), "tab salah") {
		t.Fatalf("error = %v", err)
	}
}

// TestMetaMovie: moviedb_id angka diterima (FlexString) dan year diambil.
func TestMetaMovie(t *testing.T) {
	t.Chdir(t.TempDir())
	upstream := fakeUpstream(t)
	defer upstream.Close()
	c := New()
	c.SetBase(upstream.URL)

	m, err := c.Meta(t.Context(), "movie", "tt0145487")
	if err != nil {
		t.Fatalf("Meta: %v", err)
	}
	if m.TMDB != "557" || m.Rating != "7.3" || m.Year != "2002" || len(m.Genres) != 1 {
		t.Fatalf("meta salah: %+v", m)
	}
	if len(m.Episodes) != 0 {
		t.Fatalf("movie harus tanpa episodes: %+v", m.Episodes)
	}
}

// TestMetaTV: daftar episode terlekat, Title fallback ke Name, tanpa Title.
func TestMetaTV(t *testing.T) {
	t.Chdir(t.TempDir())
	upstream := fakeUpstream(t)
	defer upstream.Close()
	c := New()
	c.SetBase(upstream.URL)

	m, err := c.Meta(t.Context(), "tv", "tt0145487")
	if err != nil {
		t.Fatalf("Meta: %v", err)
	}
	if len(m.Episodes) != 1 || m.Episodes[0].Season != 1 || m.Episodes[0].Episode != 1 {
		t.Fatalf("episodes salah: %+v", m.Episodes)
	}
	if m.Episodes[0].Title != "Pilot" {
		t.Fatalf("title salah: %q", m.Episodes[0].Title)
	}
}

// TestCacheTTL: cache panjang (search & meta) terbaca; kedaluwarsa (mtime
// mundur > TTL) tidak dipakai, dan upstream yang dilangkar dua kali tetap
// satu hit saja di trip yang fresh.
func TestCacheTTL(t *testing.T) {
	t.Chdir(t.TempDir())
	var hits int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"metas":[{"id":"tt1","name":"A","releaseInfo":"2000"}]}`))
	}))
	defer upstream.Close()
	c := New()
	c.SetBase(upstream.URL)

	if _, err := c.Search(t.Context(), "movie", "q"); err != nil {
		t.Fatalf("Search 1: %v", err)
	}
	if _, err := c.Search(t.Context(), "movie", "q"); err != nil {
		t.Fatalf("Search 2 (cache): %v", err)
	}
	if hits != 1 {
		t.Fatalf("hits = %d, want 1 (cache dipakai)", hits)
	}

	// Kedaluwarsa: semua file di ./tmp/tmdb dikeluarkan dari TTL lewat mtime.
	entries, _ := filepath.Glob(filepath.Join(cacheDir, "*.json"))
	if len(entries) == 0 {
		t.Fatal("expected cache file terbentuk")
	}
	stale := time.Now().Add(-searchTTL - time.Minute)
	for _, p := range entries {
		if err := os.Chtimes(p, stale, stale); err != nil {
			t.Fatalf("Chtimes: %v", err)
		}
	}
	hits = 0
	if _, err := c.Search(t.Context(), "movie", "q"); err != nil {
		t.Fatalf("Search 3: %v", err)
	}
	if hits != 1 {
		t.Fatalf("hits = %d, want 1 (cache basi di-refresh)", hits)
	}
}

// TestUpstreamError: status bukan 200 mengembalikan error HTTP bukan JSON raw.
func TestUpstreamError(t *testing.T) {
	t.Chdir(t.TempDir())
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer upstream.Close()
	c := New()
	c.SetBase(upstream.URL)
	if _, err := c.Search(t.Context(), "movie", "q"); err == nil || !strings.Contains(err.Error(), "Bad Gateway") {
		t.Fatalf("error = %v", err)
	}
}

// TestFirstYear: bentuk rilis umum.
func TestFirstYear(t *testing.T) {
	for in, want := range map[string]string{
		"2023-10-10": "2023", "2016–2021": "2016", "2002": "2002", "": "",
	} {
		if got := firstYear(in); got != want {
			t.Fatalf("firstYear(%q) = %q, want %q", in, got, want)
		}
	}
}
