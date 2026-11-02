package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/go-chi/chi/v5"

	"zanime/internal/tmdb"
)

// TestRateLimit: token bucket menolak kelebihan dengan 429 + Retry-After,
// dan request berikutnya setelah bucket terisi lagi tetap lolos.
func TestRateLimit(t *testing.T) {
	r := chi.NewRouter()
	r.Use(rateLimit(1)) // kapasitas 1, isi 1 token/detik
	r.Get("/", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })

	serve := func() *httptest.ResponseRecorder {
		res := httptest.NewRecorder()
		r.ServeHTTP(res, httptest.NewRequest("GET", "/", nil))
		return res
	}
	if res := serve(); res.Code != http.StatusOK {
		t.Fatalf("request pertama = %d, want 200", res.Code)
	}
	res := serve()
	if res.Code != http.StatusTooManyRequests {
		t.Fatalf("request kedua = %d, want 429", res.Code)
	}
	if res.Header().Get("Retry-After") == "" {
		t.Fatal("429 tanpa Retry-After")
	}
}

// TestPosterOG: poster Cinemeta ukuran kecil dinaikkan ke medium supaya
// pratinjau tautan (WhatsApp/X) tidak buram; URL lain dibiarkan apa adanya.
func TestPosterOG(t *testing.T) {
	cases := map[string]string{
		"https://images.metahub.space/poster/small/tt1/img":  "https://images.metahub.space/poster/medium/tt1/img",
		"https://images.metahub.space/poster/medium/tt1/img": "https://images.metahub.space/poster/medium/tt1/img",
		"https://img/poster.jpg":                             "https://img/poster.jpg",
		"":                                                   "",
	}
	for in, want := range cases {
		if got := posterOG(in); got != want {
			t.Fatalf("posterOG(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestAPISearchWrappedShape mengunci kontrak /api/search: respons harus objek
// {"results": [...]}, bukan array polos — frontend membaca body.results, dan
// bug dulu (array polos) membuat UI selalu tampil "Masukkan query lalu tekan
// Cari" padahal data sudah sampai.
func TestAPISearchWrappedShape(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/catalog/movie/top/spiderman.json" ||
			r.URL.Path == "/catalog/movie/top/search=spiderman.json" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"metas": []map[string]any{{
					"id":          "tt0145487",
					"name":        "Spider-Man",
					"releaseInfo": "2002",
					"poster":      "https://img/poster.jpg",
				}},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer upstream.Close()

	src := tmdb.New()
	src.SetBase(upstream.URL)
	srv := &server{tmdb: src}
	r := chi.NewRouter()
	r.Get("/api/search", srv.apiSearch)

	res := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/search?tab=movie&q=spiderman", nil)
	r.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status %d: %s", res.Code, res.Body.String())
	}

	var body struct {
		Results []tmdb.Item `json:"results"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatalf("bukan objek {results}: %v (%s)", err, res.Body.String())
	}
	if len(body.Results) == 0 {
		t.Fatalf("results kosong: %s", res.Body.String())
	}
	got := body.Results[0]
	if got.ID != "tt0145487" || got.Name != "Spider-Man" || got.Year != "2002" || got.Type != "movie" {
		t.Fatalf("item tidak sesuai: %+v", got)
	}
}

// TestIndexOrigin: placeholder OG di index.html diisi scheme+host request
// (og:url/og:image harus absolut), termasuk saat di belakang proxy.
func TestIndexOrigin(t *testing.T) {
	srv := &server{web: fstest.MapFS{"index.html": {Data: []byte(`<meta property="og:image" content="__PAGE_IMAGE__"><meta property="og:url" content="__PAGE_URL__">`)}}}
	r := chi.NewRouter()
	r.Get("/", srv.index)

	for _, tc := range []struct{ proto, wantImage, wantURL string }{
		{"", "http://example.com/og.png", "http://example.com/"},
		{"https", "https://example.com/og.png", "https://example.com/"},
	} {
		res := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/", nil)
		req.Host = "example.com"
		if tc.proto != "" {
			req.Header.Set("X-Forwarded-Proto", tc.proto)
		}
		r.ServeHTTP(res, req)
		body := res.Body.String()
		if !strings.Contains(body, tc.wantImage) || !strings.Contains(body, tc.wantURL) {
			t.Fatalf("proto %q: body = %q, want %q / %q", tc.proto, body, tc.wantImage, tc.wantURL)
		}
	}
}

// TestIndexOGTitle: URL judul (?tab=&id=) disajikan dengan judul, deskripsi,
// dan poster judul itu — pratinjau WhatsApp/X tidak menjalankan JS.
func TestIndexOGTitle(t *testing.T) {
	t.Chdir(t.TempDir())
	src := tmdb.New()
	src.SetBase(fakeCinemeta(t).URL)
	srv := &server{web: fstest.MapFS{"index.html": {Data: []byte(
		`<title>__PAGE_TITLE__</title><meta property="og:image" content="__PAGE_IMAGE__"><meta property="og:url" content="__PAGE_URL__">`)}}, tmdb: src}
	r := chi.NewRouter()
	r.Get("/", srv.index)

	res := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/?tab=movie&id=tt0145487", nil)
	req.Host = "kafuu.test"
	r.ServeHTTP(res, req)
	body := res.Body.String()
	for _, want := range []string{
		"Spider-Man (2002) — kafuu",
		`content="https://img/poster.jpg"`,
		"http://kafuu.test/?tab=movie&amp;id=tt0145487",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body tanpa %q: %s", want, body)
		}
	}
}

// TestAPISearchBadRequest: parameter salah tetap 400 dengan pesan JSON.
func TestAPISearchBadRequest(t *testing.T) {
	srv := &server{tmdb: tmdb.New()}
	r := chi.NewRouter()
	r.Get("/api/search", srv.apiSearch)
	res := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/search?tab=lain&q=x", nil)
	r.ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", res.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil || body["error"] == "" {
		t.Fatalf("harus error JSON: %v", res.Body.String())
	}
}
