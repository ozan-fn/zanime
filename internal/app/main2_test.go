package app

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gorilla/websocket"

	"zanime/internal/em"
	"zanime/internal/logbus"
	"zanime/internal/resolve"
	"zanime/internal/subdl"
	"zanime/internal/tmdb"
)

// fakeEM adalah em.Client palsu: mengembalikan server tetap tanpa jaringan.
func fakeEM(servers ...em.Server) Streamer { return fakeStreamer(servers) }

type fakeStreamer []em.Server

func (f fakeStreamer) Streams(context.Context, string, string, *em.TVEpisode) ([]em.Server, error) {
	if len(f) == 0 {
		return nil, errors.New("tidak ada server stream")
	}
	return f, nil
}

// emOK: satu server playable, dipakai mayoritas test.
func emOK() Streamer {
	return fakeEM(em.Server{Name: "A", Type: "mp4", Quality: "FULL HD", URL: "https://h/a.mp4"})
}

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
				"poster":      "https://img/poster.jpg",
			}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func newRouter(t *testing.T) *testApp {
	t.Helper()
	t.Chdir(t.TempDir())
	src := tmdb.New()
	src.SetBase(fakeCinemeta(t).URL)
	srv := &Server{web: fakeWebFS(), tmdb: src, em: emOK(), res: resolve.New()}
	return startTestApp(t, srv, nil)
}

func fakeWebFS() fs.FS {
	return fstest.MapFS{"index.html": {Data: []byte("<html>zanime</html>")}}
}

// TestAPISubtitles: hasil subdl dikembalikan dengan slug /api/subtitles/file,
// zip diubah jadi VTT bertimestamp valid; tab/id/tv tanpa s,e → 400.
func TestAPISubtitles(t *testing.T) {
	var zipData bytes.Buffer
	zw := zip.NewWriter(&zipData)
	f, err := zw.Create("spider-man.srt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.Write([]byte("1\r\n00:00:01,000 --> 00:00:02,500\r\nHalo\r\n"))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	var up *httptest.Server
	up = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auto":
			_, _ = w.Write([]byte(`{"results":[{"type":"movie","name":"Spider-Man","year":2002,"link":"/subtitle/sd1/spider-man"}]}`))
		case "/subtitle/sd1/spider-man":
			_, _ = w.Write([]byte(`<div data-language="english" data-language-name="English">
<li class="flex" data-downloads="42" data-row data-id="555">
<a data-subtitle-title href="/s/info/m1/x"><h4>Spider-Man 2002 English</h4></a>
<a data-subtitle-download href="` + up.URL + `/sub/555-1.zip">dl</a>
</li></div>`))
		case "/sub/555-1.zip":
			_, _ = w.Write(zipData.Bytes())
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer up.Close()

	r := newRouter(t)
	sd := subdl.New()
	sd.SetBase(up.URL, up.URL)
	r.srv.sub = sd

	res := httptest.NewRecorder()
	r.ServeHTTP(res, httptest.NewRequest("GET", "/api/subtitles?tab=movie&id=tt0145487", nil))
	if res.Code != http.StatusOK {
		t.Fatalf("status %d: %s", res.Code, res.Body.String())
	}
	var body struct {
		Subtitles []subdl.Subtitle `json:"subtitles"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatalf("bukan JSON: %v", err)
	}
	if len(body.Subtitles) != 1 || body.Subtitles[0].Language != "English" || body.Subtitles[0].ID != "555" {
		t.Fatalf("subtitles body: %+v", body.Subtitles)
	}
	u := body.Subtitles[0].URL
	if !strings.HasPrefix(u, "/api/subtitles/file?u=") {
		t.Fatalf("URL harus endpoint VTT, dapat %q", u)
	}

	res = httptest.NewRecorder()
	r.ServeHTTP(res, httptest.NewRequest("GET", u, nil))
	got := res.Body.String()
	if res.Code != http.StatusOK || !strings.Contains(got, "WEBVTT") ||
		!strings.Contains(got, "00:00:01.000 --> 00:00:02.500") {
		t.Fatalf("VTT salah: %d %q", res.Code, got)
	}

	for _, q := range []string{
		"/api/subtitles?tab=movie",
		"/api/subtitles?tab=anime&id=tt0145487",
		"/api/subtitles?tab=tv&id=tt22248376",
		"/api/subtitles/file?u=tidak-ada",
	} {
		res = httptest.NewRecorder()
		r.ServeHTTP(res, httptest.NewRequest("GET", q, nil))
		if res.Code != http.StatusBadRequest && res.Code != http.StatusNotFound {
			t.Fatalf("%q status %d, want 400/404", q, res.Code)
		}
	}
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

// TestAPIStreams: URL direspons jadi slug pendek /s/… yang ter-mapping ke
// URL asli di pool; tv butuh s/e; id kosong → 400.
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
	u := body.Servers[0].URL
	if !strings.HasPrefix(u, "/s/") {
		t.Fatalf("URL harus slug /s/…, dapat %q", u)
	}
	if got, ok := r.srv.res.Load(strings.TrimPrefix(u, "/s/")); !ok || got != "https://h/a.mp4" {
		t.Fatalf("slug tidak ter-mapping: %q ok=%v got=%q", u, ok, got)
	}
	if len(body.Servers) != 1 {
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

// TestStreamProxyPlaylistRewrite: m3u8 di-rewrite ke slug /s/… — playlist
// turun maupun URI= dalam tag; Referer dikirim ke host video; child playlist
// menyelesaikan URI relatif di dir child-nya sendiri.
func TestStreamProxyPlaylistRewrite(t *testing.T) {
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
	t.Chdir(t.TempDir())
	srv := &Server{web: fakeWebFS(), tmdb: tmdb.New(), em: fakeEM(), res: resolve.New()}
	r := startTestApp(t, srv, func(app *fiber.App) {
		app.Get("/s/:slug", srv.streamProxy)
		app.Head("/s/:slug", srv.streamProxy)
	})

	res := httptest.NewRecorder()
	r.ServeHTTP(res, httptest.NewRequest("GET", "/s/"+srv.res.Register(upstream.URL+"/master.m3u8", "master"), nil))
	body := res.Body.String()
	rewritten := lines(body)
	if !strings.HasPrefix(rewritten[2], "/s/") {
		t.Fatalf("child m3u8 harus slug: %s", body)
	}
	if !strings.Contains(rewritten[1], "URI=\"/s/") {
		t.Fatalf("URI= tag harus slug: %s", body)
	}
	// Slug rewrite menunjuk ke URL yang benar.
	childSlug := strings.TrimPrefix(rewritten[2], "/s/")
	if got, ok := srv.res.Load(childSlug); !ok || got != upstream.URL+"/child.m3u8" {
		t.Fatalf("child slug tidak ter-mapping: %q ok=%v got=%q", childSlug, ok, got)
	}
	if gotHeaders.Get("Referer") != "https://embdmstrplayer.com/" {
		t.Fatalf("Referer ke host video salah: %v", gotHeaders)
	}

	// Child playlist di subdirektori: URI rel di-resolve ke dir child.
	add2 := httptest.NewRecorder()
	r.ServeHTTP(add2, httptest.NewRequest("GET", "/s/"+srv.res.Register(upstream.URL+"/dir/child.m3u8", "child"), nil))
	addLines := strings.Split(strings.TrimSpace(add2.Body.String()), "\n")
	segSlugChild := strings.TrimPrefix(addLines[1], "/s/")
	if got, ok := srv.res.Load(segSlugChild); !ok || got != upstream.URL+"/dir/seg001.html?t=1" {
		t.Fatalf("seg child dir salah: %q got=%q ok=%v", segSlugChild, got, ok)
	}
}

// TestPlaylistCacheControl: playlist VOD (ada #EXT-X-ENDLIST) boleh di-cache
// browser (menghindari fetch ulang playlist besar), playlist live tidak.
func TestPlaylistCacheControl(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		if strings.HasSuffix(r.URL.Path, "/live.m3u8") {
			_, _ = w.Write([]byte("#EXTM3U\n#EXT-X-TARGETDURATION:6\nseg1.ts\n"))
			return
		}
		_, _ = w.Write([]byte("#EXTM3U\n#EXT-X-ENDLIST\nseg1.ts\n"))
	}))
	defer upstream.Close()
	t.Chdir(t.TempDir())
	srv := &Server{web: fakeWebFS(), tmdb: tmdb.New(), em: fakeEM(), res: resolve.New()}
	r := startTestApp(t, srv, func(app *fiber.App) {
		app.Get("/s/:slug", srv.streamProxy)
		app.Head("/s/:slug", srv.streamProxy)
	})

	res := httptest.NewRecorder()
	r.ServeHTTP(res, httptest.NewRequest("GET", "/s/"+srv.res.Register(upstream.URL+"/vod.m3u8", "vod"), nil))
	if cc := res.Header().Get("Cache-Control"); !strings.Contains(cc, "max-age") {
		t.Fatalf("playlist VOD harus cacheable, dapat %q", cc)
	}

	res = httptest.NewRecorder()
	r.ServeHTTP(res, httptest.NewRequest("GET", "/s/"+srv.res.Register(upstream.URL+"/live.m3u8", "live"), nil))
	if cc := res.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("playlist live harus no-store, dapat %q", cc)
	}
}

// lines memecah body playlist m3u8 jadi baris non-empty.
func lines(body string) []string {
	var out []string
	for _, l := range strings.Split(body, "\n") {
		l = strings.TrimSpace(l)
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

// TestStreamProxyPassthrough: byte biner lewat tak berubah; header Range /
// Accept-Ranges diteruskan; upstream 500 → 502; slug tidak dikenal → 404.
func TestStreamProxyPassthrough(t *testing.T) {
	seenFail := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "" {
			w.Header().Set("Content-Range", "bytes 0-4/10")
		}
		if strings.HasSuffix(r.URL.Path, "fail") {
			seenFail = true
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Accept-Ranges", "bytes")
		_, _ = w.Write([]byte("0123456789"))
	}))
	defer upstream.Close()
	t.Chdir(t.TempDir())
	srv := &Server{web: fakeWebFS(), tmdb: tmdb.New(), em: fakeEM(), res: resolve.New()}
	r := startTestApp(t, srv, func(app *fiber.App) {
		app.Get("/s/:slug", srv.streamProxy)
		app.Head("/s/:slug", srv.streamProxy)
	})

	res := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/s/"+srv.res.Register(upstream.URL+"/f.mp4", "film"), nil)
	req.Header.Set("Range", "bytes=0-4")
	r.ServeHTTP(res, req)
	if res.Code != http.StatusOK || res.Body.String() != "0123456789" {
		t.Fatalf("passthrough salah: %d %q", res.Code, res.Body.String())
	}
	if res.Header().Get("Accept-Ranges") != "bytes" {
		t.Fatalf("header passthrough: %v", res.Header())
	}

	// HEAD: no body.
	res = httptest.NewRecorder()
	r.ServeHTTP(res, httptest.NewRequest(http.MethodHead, "/s/"+srv.res.Register(upstream.URL+"/f.mp4", "film"), nil))
	if res.Code != http.StatusOK || res.Body.String() != "" {
		t.Fatalf("HEAD salah: %d %q", res.Code, res.Body.String())
	}

	// slug tidak dikenal → 404.
	res = httptest.NewRecorder()
	r.ServeHTTP(res, httptest.NewRequest("GET", "/s/tidak-ada-xyz", nil))
	if res.Code != http.StatusNotFound {
		t.Fatalf("slug tidak ada status %d, want 404", res.Code)
	}

	// Upstream 500 → 502.
	res = httptest.NewRecorder()
	r.ServeHTTP(res, httptest.NewRequest("GET", "/s/"+srv.res.Register(upstream.URL+"/fail", "fail"), nil))
	if res.Code != http.StatusBadGateway || !seenFail {
		t.Fatalf("upstream 500 harus 502, dapat %d", res.Code)
	}
}

// TestStreamProxySubtitle: subtitle dari CDN diteruskan apa adanya (timestamp
// tidak dinormalisasi/digeser), dan extension .vtt tanpa content-type tetap
// dikenali sebagai subtitle.
func TestStreamProxySubtitle(t *testing.T) {
	const raw = "WEBVTT\n\n01:18.579 --> 01:21.206\n<i>Hi.</i>\n"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Referer") != "https://embdmstrplayer.com/" {
			t.Errorf("Referer subtitle salah: %v", r.Header)
		}
		if strings.HasSuffix(r.URL.Path, "/plain.vtt") {
			_, _ = w.Write([]byte(raw))
			return
		}
		w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
		_, _ = w.Write([]byte(raw))
	}))
	defer upstream.Close()
	t.Chdir(t.TempDir())
	srv := &Server{web: fakeWebFS(), tmdb: tmdb.New(), em: fakeEM(), res: resolve.New()}
	r := startTestApp(t, srv, func(app *fiber.App) {
		app.Get("/s/:slug", srv.streamProxy)
		app.Head("/s/:slug", srv.streamProxy)
	})

	res := httptest.NewRecorder()
	r.ServeHTTP(res, httptest.NewRequest("GET", "/s/"+srv.res.Register(upstream.URL+"/sub.vtt", "sub"), nil))
	if res.Code != http.StatusOK || res.Body.String() != raw {
		t.Fatalf("subtitle harus apa adanya: %d %q", res.Code, res.Body.String())
	}
	if !strings.Contains(res.Header().Get("Content-Type"), "text/vtt") {
		t.Fatalf("content-type subtitle: %q", res.Header().Get("Content-Type"))
	}

	// Extension .vtt tanpa content-type tetap dikenali dan diteruskan apa adanya.
	res = httptest.NewRecorder()
	r.ServeHTTP(res, httptest.NewRequest("GET", "/s/"+srv.res.Register(upstream.URL+"/plain.vtt", "plain"), nil))
	if res.Code != http.StatusOK || res.Body.String() != raw {
		t.Fatalf(".vtt tanpa content-type: %d %q", res.Code, res.Body.String())
	}
}

// TestWebFilePrecompressed: aset dist punya varian .br/.zst/.gz; server
// memilih sesuai Accept-Encoding (Content-Encoding + Vary), jatuh ke berkas
// asli bila tidak didukung, dan index.html SPA tetap dilayani dinamis.
func TestWebFilePrecompressed(t *testing.T) {
	web := fstest.MapFS{
		"index.html":        {Data: []byte("<html>zanime</html>")},
		"assets/app.js":     {Data: []byte("console.log(1)")},
		"assets/app.js.br":  {Data: []byte("BROTLI")},
		"assets/app.js.gz":  {Data: []byte("GZIP")},
		"assets/app.js.zst": {Data: []byte("ZSTD")},
		"favicon.svg":       {Data: []byte("<svg/>")},
	}
	srv := &Server{web: web, tmdb: tmdb.New(), em: fakeEM(), res: resolve.New()}
	r := startTestApp(t, srv, func(app *fiber.App) {
		app.Get("/assets/*", srv.webFile)
		app.Get("/favicon.svg", srv.webFile)
		app.Use(srv.spaFallback)
	})

	cases := []struct{ accept, body, enc string }{
		{"gzip, deflate, br", "BROTLI", "br"},
		{"zstd", "ZSTD", "zstd"},
		{"gzip", "GZIP", "gzip"},
		{"br;q=0, identity", "console.log(1)", ""},
		{"", "console.log(1)", ""},
	}
	for _, tc := range cases {
		req := httptest.NewRequest("GET", "/assets/app.js", nil)
		if tc.accept != "" {
			req.Header.Set("Accept-Encoding", tc.accept)
		}
		res := httptest.NewRecorder()
		r.ServeHTTP(res, req)
		if res.Code != http.StatusOK || res.Body.String() != tc.body {
			t.Fatalf("Accept-Encoding %q: %d %q, want %q", tc.accept, res.Code, res.Body.String(), tc.body)
		}
		if got := res.Header().Get("Content-Encoding"); got != tc.enc {
			t.Fatalf("Accept-Encoding %q: Content-Encoding %q, want %q", tc.accept, got, tc.enc)
		}
		if !strings.Contains(res.Header().Get("Vary"), "Accept-Encoding") {
			t.Fatalf("Accept-Encoding %q: Vary %q", tc.accept, res.Header().Get("Vary"))
		}
		if ct := res.Header().Get("Content-Type"); !strings.Contains(ct, "text/javascript") {
			t.Fatalf("Accept-Encoding %q: Content-Type %q", tc.accept, ct)
		}
	}

	// Aset tanpa varian terkompresi tetap jalan, dan SPA fallback selalu HTML
	// dinamis (varian index.html.br tidak dipakai).
	res := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/favicon.svg", nil)
	req.Header.Set("Accept-Encoding", "br")
	r.ServeHTTP(res, req)
	if res.Body.String() != "<svg/>" || res.Header().Get("Content-Encoding") != "" {
		t.Fatalf("favicon: %q enc=%q", res.Body.String(), res.Header().Get("Content-Encoding"))
	}

	res = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/judul/apa-saja", nil)
	req.Header.Set("Accept-Encoding", "br")
	r.ServeHTTP(res, req)
	if res.Body.String() != "<html>zanime</html>" || res.Header().Get("Content-Encoding") != "" {
		t.Fatalf("SPA fallback: %q enc=%q", res.Body.String(), res.Header().Get("Content-Encoding"))
	}
}

// TestSPAFallback: path tidak dikenal → index.html; path /api/… dan /s/… tak
// dikenal → JSON 404 (bukan index) supaya client bisa bedakan error.
func TestSPAFallback(t *testing.T) {
	srv := &Server{web: fakeWebFS(), tmdb: tmdb.New(), em: fakeEM(), res: resolve.New()}
	r := startTestApp(t, srv, func(app *fiber.App) { app.Use(srv.spaFallback) })

	res := httptest.NewRecorder()
	r.ServeHTTP(res, httptest.NewRequest("GET", "/somewhere", nil))
	if res.Code != http.StatusOK || res.Body.String() != "<html>zanime</html>" {
		t.Fatalf("fallback index: %d %s", res.Code, res.Body.String())
	}

	for _, p := range []string{"/api/tidak-ada", "/s/tidak-ada-xyz"} {
		res = httptest.NewRecorder()
		r.ServeHTTP(res, httptest.NewRequest("GET", p, nil))
		if res.Code != http.StatusNotFound || !strings.Contains(res.Body.String(), "not found") {
			t.Fatalf("%s: %d %s", p, res.Code, res.Body.String())
		}
	}
}

// TestSubSlug: terjemahan menerima URL track player maupun endpoint subdl,
// dan URL asing ditolak.
func TestSubSlug(t *testing.T) {
	cases := map[string]string{
		"/s/sub-en.vtt":                   "sub-en.vtt",
		"/api/subtitles/file?u=sub-x.zip": "sub-x.zip",
		"":                                "",
		"https://evil.example/sub.vtt":    "",
		"/api/subtitles/file":             "",
		"/api/subtitles/file?u=&x=1":      "",
	}
	for in, want := range cases {
		if got := subSlug(in); got != want {
			t.Fatalf("subSlug(%q) = %q, want %q", in, got, want)
		}
	}
}

// drainLog membaca semua event yang sudah ada di kanal (non-blocking).
func drainLog(ch <-chan logbus.Event) string {
	var b strings.Builder
	for {
		select {
		case ev := <-ch:
			b.WriteString(ev.Level + " " + ev.Text + "\n")
		default:
			return b.String()
		}
	}
}

// TestStreamProxyLogs: tiap permintaan proxy menulis satu baris log (jumlah
// rendition/segmen, VOD) ke hub — dasar panel log di UI.
func TestStreamProxyLogs(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		_, _ = w.Write([]byte("#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1\nchild.m3u8\n#EXTINF:6,\nseg1.ts\n#EXT-X-ENDLIST\n"))
	}))
	defer upstream.Close()
	t.Chdir(t.TempDir())
	logs := logbus.New()
	ch, cancel := logs.Subscribe()
	defer cancel()
	srv := &Server{web: fakeWebFS(), tmdb: tmdb.New(), em: fakeEM(), res: resolve.New(), logs: logs}
	r := startTestApp(t, srv, func(app *fiber.App) {
		app.Get("/s/:slug", srv.streamProxy)
		app.Head("/s/:slug", srv.streamProxy)
	})

	res := httptest.NewRecorder()
	r.ServeHTTP(res, httptest.NewRequest("GET", "/s/"+srv.res.Register(upstream.URL+"/master.m3u8", "master"), nil))

	got := drainLog(ch)
	for _, want := range []string{"Playlist HLS", "1 rendition", "1 segmen", "VOD"} {
		if !strings.Contains(got, want) {
			t.Fatalf("log proxy tanpa %q: %q", want, got)
		}
	}
}

// TestAPILogsWebSocket: klien websocket menerima riwayat lalu baris baru.
func TestAPILogsWebSocket(t *testing.T) {
	logs := logbus.New()
	srv := &Server{web: fakeWebFS(), tmdb: tmdb.New(), em: fakeEM(), res: resolve.New(), logs: logs}
	r := startTestApp(t, srv, func(app *fiber.App) { app.Get("/api/logs", srv.logsRoute()) })

	// Ditulis sebelum klien menyambung: harus dikirim sebagai riwayat.
	logs.Emit(logbus.Event{ID: "srv:X:verify", Level: logbus.LevelInfo, Text: "uji X…"})

	conn, _, err := websocket.DefaultDialer.Dial(r.wsURL("/api/logs"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// Tunggu handler benar-benar berlangganan sebelum Emit berikutnya.
	deadline := time.Now().Add(2 * time.Second)
	for logs.Subscribers() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if logs.Subscribers() == 0 {
		t.Fatal("handler websocket tidak berlangganan")
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var got logbus.Event
	if err := conn.ReadJSON(&got); err != nil {
		t.Fatalf("baca riwayat: %v", err)
	}
	if got.Text != "uji X…" || got.Level != logbus.LevelInfo {
		t.Fatalf("riwayat = %+v", got)
	}

	logs.OK("live")
	if err := conn.ReadJSON(&got); err != nil {
		t.Fatalf("baca live: %v", err)
	}
	if got.Text != "live" || got.Level != logbus.LevelOK {
		t.Fatalf("live = %+v", got)
	}
}
