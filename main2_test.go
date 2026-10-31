package main

import "io/fs"

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"

	"zanime/internal/em"
	"zanime/internal/tmdb"
)

// fakeEM membangun *em.Client dengan pipeline stub (tanpa bun/network).
func fakeEM(rows string) *em.Client {
	c := em.New()
	c.Run = func(context.Context, []string) ([]byte, string, error) {
		return []byte(rows), "", nil
	}
	return c
}

const emRowsOK = `{"server":"A","data":{"ok":true,"url":"https://h/a.mp4","type":"progressive","quality":"FULL HD"}}` + "\n" +
	`{"done":true,"resolved":1,"total":1,"meta":{}}` + "\n"

// fakeCinemeta meniru Cinemeta cukup untuk search & meta.
func fakeCinemeta(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/catalog/"):
			_ = json.NewEncoder(w).Encode(map[string]any{"metas": []map[string]any{
				{"id": "tt0145487", "name": "Spider-Man", "releaseInfo": "2002", "poster": "p.jpg"},
			}})
		case strings.HasPrefix(r.URL.Path, "/meta/"):
			_ = json.NewEncoder(w).Encode(map[string]any{"meta": map[string]any{
				"id":          "tt0145487",
				"moviedb_id":  557,
				"name":        "Spider-Man",
				"releaseInfo": "2002",
			}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func newRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	src := tmdb.New()
	src.SetBase(fakeCinemeta(t).URL)
	srv := &server{web: fakeWebFS(), tmdb: src, em: fakeEM(emRowsOK)}
	r := gin.New()
	r.GET("/api/search", srv.apiSearch)
	r.GET("/api/meta/:tab/:id", srv.apiMeta)
	r.GET("/api/streams", srv.apiStreams)
	r.GET("/fetch", srv.fetchProxy)
	r.NoRoute(srv.spaFallback)
	return r
}

func fakeWebFS() fs.FS {
	return fstest.MapFS{"index.html": {Data: []byte("<html>zanime</html>")}}
}

// TestAPIMeta: bentuk meta movie; id bukan tt… dan tab salah → 400.
func TestAPIMeta(t *testing.T) {
	r := newRouter(t)

	res := httptest.NewRecorder()
	r.ServeHTTP(res, httptest.NewRequest("GET", "/api/meta/movie/tt0145487", nil))
	if res.Code != http.StatusOK {
		t.Fatalf("status %d: %s", res.Code, res.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatalf("bukan JSON: %v", err)
	}
	if body["tmdb"] != "557" {
		t.Fatalf("meta body: %v", body)
	}

	for _, path := range []string{"/api/meta/movie/557", "/api/meta/anime/tt0145487"} {
		res = httptest.NewRecorder()
		r.ServeHTTP(res, httptest.NewRequest("GET", path, nil))
		if res.Code != http.StatusBadRequest {
			t.Fatalf("%q status %d, want 400", path, res.Code)
		}
	}
}

// TestAPIStreams: movie tanpa s/e jalan; tv butuh s/e; id kosong → 400.
func TestAPIStreams(t *testing.T) {
	r := newRouter(t)

	res := httptest.NewRecorder()
	r.ServeHTTP(res, httptest.NewRequest("GET", "/api/streams?tab=movie&id=557", nil))
	if res.Code != http.StatusOK {
		t.Fatalf("status %d: %s", res.Code, res.Body.String())
	}
	var body struct {
		Servers []em.Server `json:"servers"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatalf("bukan JSON: %v", err)
	}
	if len(body.Servers) != 1 || body.Servers[0].URL != "https://h/a.mp4" {
		t.Fatalf("servers body: %+v", body)
	}

	for _, q := range []string{
		"/api/streams?tab=tv&id=283938",           // tanpa s/e
		"/api/streams?tab=tv&id=283938&s=1&e=bad", // e bukan angka
		"/api/streams?tab=movie",                  // tanpa id
	} {
		res = httptest.NewRecorder()
		r.ServeHTTP(res, httptest.NewRequest("GET", q, nil))
		if res.Code != http.StatusBadRequest {
			t.Fatalf("%q status %d, want 400", q, res.Code)
		}
	}
}

// TestFetchProxyPlaylistRewrite: m3u8 di-rewrite ke /fetch?url=<abs> —
// playlist turun maupun URI= dalam tag; Referer dikirim ke host video;
// child playlist menyelesaikan URI relatif di dir child-nya sendiri.
func TestFetchProxyPlaylistRewrite(t *testing.T) {
	var gotHeaders http.Header
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		switch {
		case strings.HasSuffix(r.URL.Path, "/master.m3u8"):
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = w.Write([]byte("#EXTM3U\n" +
				"#EXT-X-KEY:METHOD=AES-128,URI=\"https://up.host/key.bin\"\n" +
				"child.m3u8\n"))
		case strings.HasSuffix(r.URL.Path, "/child.m3u8"):
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = w.Write([]byte("#EXTM3U\nseg001.html?t=1\n"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer upstream.Close()
	gin.SetMode(gin.TestMode)
	srv := &server{web: fakeWebFS(), tmdb: tmdb.New(), em: fakeEM("")}
	r := gin.New()
	r.GET("/fetch", srv.fetchProxy)

	res := httptest.NewRecorder()
	r.ServeHTTP(res, httptest.NewRequest("GET", "/fetch?url="+url.QueryEscape(upstream.URL+"/master.m3u8"), nil))
	body := res.Body.String()
	escaped := url.QueryEscape(upstream.URL)
	for _, want := range []string{
		"/fetch?url=" + escaped + "%2Fchild.m3u8",
		"/fetch?url=https%3A%2F%2Fup.host%2Fkey.bin",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("rewrite hilang %q dalam %s", want, body)
		}
	}
	if gotHeaders.Get("Referer") != "https://embdmstrplayer.com/" {
		t.Fatalf("Referer ke host video salah: %v", gotHeaders)
	}

	// Child playlist di subdirektori: URI rel di-relative kanan ke dir child.
	res = httptest.NewRecorder()
	r.ServeHTTP(res, httptest.NewRequest("GET", "/fetch?url="+url.QueryEscape(upstream.URL+"/dir/child.m3u8"), nil))
	if !strings.Contains(res.Body.String(), "/fetch?url="+escaped+"%2Fdir%2Fseg001.html%3Ft%3D1") {
		t.Fatalf("child rewrite salah: %s", res.Body.String())
	}
}

// TestFetchProxyPassthrough: byte biner lewat tak berubah; header Range /
// upstream 500 → 502; URL rusak → 400.
func TestFetchProxyPassthrough(t *testing.T) {
	seenFail := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("bin") == "1" {
			w.Header().Set("Content-Type", "video/mp4")
			w.Header().Set("Accept-Ranges", "bytes")
			_, _ = w.Write([]byte("0123456789"))
			return
		}
		seenFail = true
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer upstream.Close()
	gin.SetMode(gin.TestMode)
	srv := &server{web: fakeWebFS(), tmdb: tmdb.New(), em: fakeEM("")}
	r := gin.New()
	r.GET("/fetch", srv.fetchProxy)

	res := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/fetch?url="+url.QueryEscape(upstream.URL+"/f.mp4?bin=1"), nil)
	req.Header.Set("Range", "bytes=0-4")
	r.ServeHTTP(res, req)
	if res.Code != http.StatusOK || res.Body.String() != "0123456789" {
		t.Fatalf("passthrough salah: %d %q", res.Code, res.Body.String())
	}
	if res.Header().Get("Accept-Ranges") != "bytes" {
		t.Fatalf("header passthrough: %v", res.Header())
	}

	res = httptest.NewRecorder()
	r.ServeHTTP(res, httptest.NewRequest("GET", "/fetch?url=%zz", nil))
	if res.Code != http.StatusBadRequest {
		t.Fatalf("url rusak status %d, want 400", res.Code)
	}

	res = httptest.NewRecorder()
	r.ServeHTTP(res, httptest.NewRequest("GET", "/fetch?url="+url.QueryEscape(upstream.URL+"/fail"), nil))
	if res.Code != http.StatusBadGateway || !seenFail {
		t.Fatalf("upstream 500 harus 502, dapat %d", res.Code)
	}
}

// TestSPAFallback: path tidak dikenal → index.html; path /api/… tak dikenal →
// JSON 404 (bukan index) supaya client bisa bedakan error.
func TestSPAFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	srv := &server{web: fakeWebFS(), tmdb: tmdb.New(), em: fakeEM("")}
	r := gin.New()
	r.NoRoute(srv.spaFallback)

	res := httptest.NewRecorder()
	r.ServeHTTP(res, httptest.NewRequest("GET", "/somewhere", nil))
	if res.Code != http.StatusOK || res.Body.String() != "<html>zanime</html>" {
		t.Fatalf("fallback index: %d %s", res.Code, res.Body.String())
	}

	res = httptest.NewRecorder()
	r.ServeHTTP(res, httptest.NewRequest("GET", "/api/tidak-ada", nil))
	if res.Code != http.StatusNotFound || !strings.Contains(res.Body.String(), "not found") {
		t.Fatalf("api 404: %d %s", res.Code, res.Body.String())
	}
}
