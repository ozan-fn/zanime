package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"zanime/internal/tmdb"
)

// TestAPISearchWrappedShape mengunci kontrak /api/search: respons harus objek
// {"results": [...]}, bukan array polos — frontend membaca body.results, dan
// bug dulu (array polos) membuat UI selalu tampil "Masukkan query lalu tekan
// Cari" padahal data sudah sampai.
func TestAPISearchWrappedShape(t *testing.T) {
	gin.SetMode(gin.TestMode)
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
	r := gin.New()
	r.GET("/api/search", srv.apiSearch)

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

// TestAPISearchBadRequest: parameter salah tetap 400 dengan pesan JSON.
func TestAPISearchBadRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	srv := &server{tmdb: tmdb.New()}
	r := gin.New()
	r.GET("/api/search", srv.apiSearch)
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
