package main

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"zanime/internal/em"
	"zanime/internal/logbus"
	"zanime/internal/resolve"
	"zanime/internal/tmdb"
)

// fakeEM adalah em.Client palsu: mengembalikan server tetap tanpa jaringan.
func fakeEM(servers ...em.Server) streamer { return fakeStreamer(servers) }

type fakeStreamer []em.Server

func (f fakeStreamer) Streams(context.Context, string, string, *em.TVEpisode) ([]em.Server, error) {
	if len(f) == 0 {
		return nil, errors.New("tidak ada server stream")
	}
	return f, nil
}

// emOK: satu server playable, dipakai mayoritas test.
func emOK() streamer {
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
			}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func newRouter(t *testing.T) (*gin.Engine, *server) {
	t.Helper()
	t.Chdir(t.TempDir())
	gin.SetMode(gin.TestMode)
	src := tmdb.New()
	src.SetBase(fakeCinemeta(t).URL)
	srv := &server{web: fakeWebFS(), tmdb: src, em: emOK(), res: resolve.New()}
	r := gin.New()
	r.GET("/api/search", srv.apiSearch)
	r.GET("/api/meta/:tab/:id", srv.apiMeta)
	r.GET("/api/streams", srv.apiStreams)
	r.GET("/s/:slug", srv.streamProxy)
	r.NoRoute(srv.spaFallback)
	return r, srv
}

func fakeWebFS() fs.FS {
	return fstest.MapFS{"index.html": {Data: []byte("<html>zanime</html>")}}
}

// TestAPIMeta: bentuk meta movie; id bukan tt… dan tab salah → 400.
func TestAPIMeta(t *testing.T) {
	r, _ := newRouter(t)

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
	r, srv := newRouter(t)

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
	if got, ok := srv.res.Load(strings.TrimPrefix(u, "/s/")); !ok || got != "https://h/a.mp4" {
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
	gin.SetMode(gin.TestMode)
	srv := &server{web: fakeWebFS(), tmdb: tmdb.New(), em: fakeEM(), res: resolve.New()}
	r := gin.New()
	r.GET("/s/:slug", srv.streamProxy)

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
	gin.SetMode(gin.TestMode)
	srv := &server{web: fakeWebFS(), tmdb: tmdb.New(), em: fakeEM(), res: resolve.New()}
	r := gin.New()
	r.GET("/s/:slug", srv.streamProxy)

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
	gin.SetMode(gin.TestMode)
	srv := &server{web: fakeWebFS(), tmdb: tmdb.New(), em: fakeEM(), res: resolve.New()}
	r := gin.New()
	r.GET("/s/:slug", srv.streamProxy)
	r.HEAD("/s/:slug", srv.streamProxy)

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

// TestNormalizeVTT: timestamp tanpa jam (format CDN subtitle) diberi 00:,
// jam satu digit dipad, milidetik pendek dipad, dan bentuk yang sudah benar
// tidak diubah ganda. Baris non-cue tidak disentuh.
func TestNormalizeVTT(t *testing.T) {
	in := "WEBVTT\n\n01:18.579 --> 01:21.206\n<i>Hi.</i>\n\n\n1:23:45.5 --> 1:23:47.25\nbaris lain\n\n00:00:01.000 --> 00:00:02.000\nsudah benar\n"
	want := "WEBVTT\n\n00:01:18.579 --> 00:01:21.206\n<i>Hi.</i>\n\n\n01:23:45.500 --> 01:23:47.250\nbaris lain\n\n00:00:01.000 --> 00:00:02.000\nsudah benar\n"
	if got := normalizeVTT(in); got != want {
		t.Fatalf("normalizeVTT =\n%q\nwant\n%q", got, want)
	}
}

// TestStreamProxySubtitle: VTT dari CDN (timestamp MM:SS.mmm) normalisasi lewat
// proxy → 00:MM:SS.mmm, dan extension .vtt tanpa content-type ikut dikenali.
func TestStreamProxySubtitle(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Referer") != "https://embdmstrplayer.com/" {
			t.Errorf("Referer subtitle salah: %v", r.Header)
		}
		if strings.HasSuffix(r.URL.Path, "/plain.vtt") {
			_, _ = w.Write([]byte("WEBVTT\n\n01:18.579 --> 01:21.206\nhi\n"))
			return
		}
		w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
		_, _ = w.Write([]byte("WEBVTT\n\n01:18.579 --> 01:21.206\n<i>Hi.</i>\n"))
	}))
	defer upstream.Close()
	t.Chdir(t.TempDir())
	gin.SetMode(gin.TestMode)
	srv := &server{web: fakeWebFS(), tmdb: tmdb.New(), em: fakeEM(), res: resolve.New()}
	r := gin.New()
	r.GET("/s/:slug", srv.streamProxy)

	res := httptest.NewRecorder()
	r.ServeHTTP(res, httptest.NewRequest("GET", "/s/"+srv.res.Register(upstream.URL+"/sub.vtt", "sub"), nil))
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), "00:01:18.579 --> 00:01:21.206") {
		t.Fatalf("subtitle tidak dinormalisasi: %d %q", res.Code, res.Body.String())
	}
	if !strings.Contains(res.Header().Get("Content-Type"), "text/vtt") {
		t.Fatalf("content-type subtitle: %q", res.Header().Get("Content-Type"))
	}

	// Extension .vtt tanpa content-type tetap dikenali dan dinormalisasi.
	res = httptest.NewRecorder()
	r.ServeHTTP(res, httptest.NewRequest("GET", "/s/"+srv.res.Register(upstream.URL+"/plain.vtt", "plain"), nil))
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), "00:01:18.579") {
		t.Fatalf(".vtt tanpa content-type: %d %q", res.Code, res.Body.String())
	}
}

// vttCueRe adalah pola cue yang dipakai parser movi-player: wajib
// HH:MM:SS.mmm di kedua sisi "-->" (bukan MM:SS.mmm). Semua subtitle yang
// dikirim server harus lolos pola ini, sama seperti track yang sudah sehat
// (mis. WMovies), supaya tidak jadi track kosong.
var vttCueRe = regexp.MustCompile(`\d{2}:\d{2}:\d{2}[.,]\d{3}\s*-->\s*\d{2}:\d{2}:\d{2}[.,]\d{3}`)

// TestStreamProxySubtitleUniform: VTT dari tiap host (MovieJoy MM:SS.mmm,
// WMovies HH:MM:SS.mmm + nomor cue) dikirim dengan bentuk timestamp yang SAMA
// setelah lewat proxy, dan tiap cue bisa diparse parser movi-player.
func TestStreamProxySubtitleUniform(t *testing.T) {
	bodies := map[string]string{
		"moviejoy": "WEBVTT\n\n01:18.579 --> 01:21.206\n<i>Hi. My name is Peter Parker.</i>\n\n01:23.250 --> 01:26.294\nYou don't remember me,\nbut I have \"",
		"wmovies":  "WEBVTT\n\n1\n00:01:18.622 --> 00:01:21.250\nHi. My name is Peter Parker.\n\n2\n00:01:23.293 --> 00:01:26.255\nYou don't remember me",
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
		_, _ = w.Write([]byte(bodies[strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), ".vtt")]))
	}))
	defer upstream.Close()
	t.Chdir(t.TempDir())
	gin.SetMode(gin.TestMode)
	srv := &server{web: fakeWebFS(), tmdb: tmdb.New(), em: fakeEM(), res: resolve.New()}
	r := gin.New()
	r.GET("/s/:slug", srv.streamProxy)

	for name := range bodies {
		res := httptest.NewRecorder()
		r.ServeHTTP(res, httptest.NewRequest("GET", "/s/"+srv.res.Register(upstream.URL+"/"+name+".vtt", name), nil))
		got := res.Body.String()
		cues := vttCueRe.FindAllString(got, -1)
		if len(cues) != 2 {
			t.Fatalf("%s: cue terparse = %d, want 2 (%q)", name, len(cues), got)
		}
		// Tidak boleh ada timestamp 2-bagian yang tersisa (MM:SS.mmm).
		if regexp.MustCompile(`(^|[^\d:])\d{1,2}:\d{2}[.,]\d{3}\s*-->`).MatchString(got) {
			t.Fatalf("%s: masih ada timestamp MM:SS.mmm: %q", name, got)
		}
	}
}

// TestSPAFallback: path tidak dikenal → index.html; path /api/… dan /s/… tak
// dikenal → JSON 404 (bukan index) supaya client bisa bedakan error.
func TestSPAFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	srv := &server{web: fakeWebFS(), tmdb: tmdb.New(), em: fakeEM(), res: resolve.New()}
	r := gin.New()
	r.NoRoute(srv.spaFallback)

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

// TestStreamProxyLogs: tiap permintaan proxy menulis satu baris log (host,
// jumlah rendition/segmen, VOD) ke hub — dasar panel log di UI.
func TestStreamProxyLogs(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		_, _ = w.Write([]byte("#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1\nchild.m3u8\n#EXTINF:6,\nseg1.ts\n#EXT-X-ENDLIST\n"))
	}))
	defer upstream.Close()
	t.Chdir(t.TempDir())
	gin.SetMode(gin.TestMode)
	logs := logbus.New()
	ch, cancel := logs.Subscribe()
	defer cancel()
	srv := &server{web: fakeWebFS(), tmdb: tmdb.New(), em: fakeEM(), res: resolve.New(), logs: logs}
	r := gin.New()
	r.GET("/s/:slug", srv.streamProxy)

	res := httptest.NewRecorder()
	r.ServeHTTP(res, httptest.NewRequest("GET", "/s/"+srv.res.Register(upstream.URL+"/master.m3u8", "master"), nil))

	got := drainLog(ch)
	for _, want := range []string{"playlist", "1 rendition", "1 segmen", "VOD"} {
		if !strings.Contains(got, want) {
			t.Fatalf("log proxy tanpa %q: %q", want, got)
		}
	}
}

// TestAPILogsWebSocket: klien websocket menerima riwayat lalu baris baru.
func TestAPILogsWebSocket(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logs := logbus.New()
	srv := &server{web: fakeWebFS(), tmdb: tmdb.New(), em: fakeEM(), res: resolve.New(), logs: logs}
	r := gin.New()
	r.GET("/api/logs", srv.apiLogs)
	ts := httptest.NewServer(r)
	defer ts.Close()

	// Ditulis sebelum klien menyambung: harus dikirim sebagai riwayat.
	logs.Emit(logbus.Event{ID: "srv:X:verify", Level: logbus.LevelInfo, Text: "uji X…"})

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+"/api/logs", nil)
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
