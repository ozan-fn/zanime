package main

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"zanime/internal/memstorage"
)

// TestWSStatePayload memastikan frame /api/ws memuat semua field yang dibaca
// UI (index.html): files, selected, cache, peers/net, transcode, loading.
func TestWSStatePayload(t *testing.T) {
	mem := memstorage.New(1 << 20)
	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = t.TempDir()
	cfg.DefaultStorage = mem
	cfg.NoDHT = true
	cfg.DisableTrackers = true
	cfg.DisablePEX = true
	cfg.NoDefaultPortForwarding = true
	cfg.ListenPort = 0
	cl, err := torrent.NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer cl.Close()

	srv := newServer(cl, mem, nil, 0)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/ws", srv.apiWS)
	ts := httptest.NewServer(r)
	defer ts.Close()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+"/api/ws", nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("baca frame ws: %v", err)
	}

	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("frame bukan JSON: %v", err)
	}
	for _, key := range []string{"ready", "loading", "files", "selected", "cache", "transcode", "net", "users", "server"} {
		if _, ok := state[key]; !ok {
			t.Fatalf("state tanpa %q: %v", key, state)
		}
	}
	netCounters, _ := state["net"].(map[string]any)
	for _, key := range []string{"from_peers", "to_client"} {
		if _, ok := netCounters[key]; !ok {
			t.Fatalf("state.net tanpa %q: %v", key, netCounters)
		}
	}
	cache, _ := state["cache"].(map[string]any)
	for _, key := range []string{"used", "limit", "evictions", "pieces"} {
		if _, ok := cache[key]; !ok {
			t.Fatalf("state.cache tanpa %q: %v", key, cache)
		}
	}
	srvInfo, _ := state["server"].(map[string]any)
	for _, key := range []string{"uptime", "goroutines", "heap", "sys"} {
		if _, ok := srvInfo[key]; !ok {
			t.Fatalf("state.server tanpa %q: %v", key, srvInfo)
		}
	}
	users, _ := state["users"].(map[string]any)
	for _, key := range []string{"active", "tabs"} {
		if _, ok := users[key]; !ok {
			t.Fatalf("state.users tanpa %q: %v", key, users)
		}
	}
}

// Toggle transcode tersimpan di server dan ikut di state, jadi tab lain dan
// halaman yang di-reload melihat mode yang sama.
func TestAPITranscodeToggle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	srv := newServer(nil, memstorage.New(1<<20), nil, 0)
	r := gin.New()
	r.POST("/api/transcode", srv.apiTranscode)

	toggle := func(body string) (int, map[string]any) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/transcode", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(rec, req)
		var state map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &state)
		return rec.Code, state
	}

	if code, state := toggle(`{"enabled":true}`); code != http.StatusOK || state["transcode"] != true {
		t.Fatalf("enable = %d, transcode %v", code, state["transcode"])
	}
	if !srv.transcodeOn.Load() {
		t.Fatal("mode transcode tidak tersimpan di server")
	}
	if code, state := toggle(`{"enabled":false}`); code != http.StatusOK || state["transcode"] != false {
		t.Fatalf("disable = %d, transcode %v", code, state["transcode"])
	}
	if code, _ := toggle(`bukan json`); code != http.StatusBadRequest {
		t.Fatalf("body rusak = %d, want 400", code)
	}
}

func TestVideoFilesSortedByName(t *testing.T) {
	info := metainfo.Info{
		Name:        "pack",
		PieceLength: 1 << 20,
		Pieces:      make([]byte, 20), // one piece; contents are never read
		Files: []metainfo.FileInfo{
			{Path: []string{"ep02.mkv"}, Length: 1},
			{Path: []string{"ep10.mkv"}, Length: 1},
			{Path: []string{"ep01.mkv"}, Length: 1},
			{Path: []string{"notes.txt"}, Length: 1},
		},
	}
	infoBytes, err := bencode.Marshal(info)
	if err != nil {
		t.Fatalf("encode info: %v", err)
	}

	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = t.TempDir()
	cfg.DefaultStorage = memstorage.New(1 << 20)
	cfg.NoDHT = true
	cfg.DisableTrackers = true
	cfg.DisablePEX = true
	cfg.NoDefaultPortForwarding = true
	cfg.ListenPort = 0
	cl, err := torrent.NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer cl.Close()

	tor, _, err := cl.AddTorrentSpec(torrent.TorrentSpecFromMetaInfo(&metainfo.MetaInfo{InfoBytes: infoBytes}))
	if err != nil {
		t.Fatalf("AddTorrentSpec: %v", err)
	}
	<-tor.GotInfo()

	var got []string
	for _, f := range videoFiles(tor.Files()) {
		got = append(got, path.Base(f.Path()))
	}
	want := []string{"ep01.mkv", "ep02.mkv", "ep10.mkv"}
	if !slices.Equal(got, want) {
		t.Fatalf("videoFiles order = %v, want %v", got, want)
	}
}

// fakeFFmpeg menulis skrip yang berperan sebagai ffmpeg: menulis playlist
// (argumen terakhir) + satu segmen di folder yang sama.
func fakeFFmpeg(t *testing.T) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "ffmpeg")
	script := strings.Join([]string{
		"#!/bin/sh",
		// Invokasi probe (`-i` tanpa output) langsung keluar.
		`case "$*" in *"-f hls"*) ;; *) exit 0 ;; esac`,
		`for a in "$@"; do last=$a; done`,
		`dir=$(dirname "$last")`,
		`printf '#EXTM3U\n#EXTINF:4.0,\nseg00000.ts\n' > "$last"`,
		`printf 'segment-bytes' > "$dir/seg00000.ts"`,
		"",
	}, "\n")
	if err := os.WriteFile(file, []byte(script), 0o755); err != nil {
		t.Fatalf("tulis ffmpeg palsu: %v", err)
	}
	return file
}

// TestHLSFileServesSession memastikan route /hls mengirim playlist dan segmen
// milik sesi file terpilih, dengan Content-Type yang benar, dan 404 untuk file
// lain.
func TestHLSFileServesSession(t *testing.T) {
	srv := newServer(nil, memstorage.New(1<<20), nil, 0)
	t.Cleanup(srv.hls.Stop)
	srv.hls.Start(context.Background(), 0, "http://127.0.0.1:1/stream/0/x.mkv", fakeFFmpeg(t), false)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/hls/:index/*name", srv.hlsFile)
	fetch := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		if rec := fetch("/hls/0/index.m3u8"); rec.Code == http.StatusOK {
			if ct := rec.Header().Get("Content-Type"); ct != "application/vnd.apple.mpegurl" {
				t.Fatalf("content-type playlist = %q", ct)
			}
			if !strings.Contains(rec.Body.String(), "seg00000.ts") {
				t.Fatalf("playlist = %q", rec.Body.String())
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("playlist tidak pernah tersedia")
		}
		time.Sleep(20 * time.Millisecond)
	}

	segment := fetch("/hls/0/seg00000.ts")
	if segment.Code != http.StatusOK || segment.Body.String() != "segment-bytes" {
		t.Fatalf("segmen = %d %q", segment.Code, segment.Body.String())
	}
	if ct := segment.Header().Get("Content-Type"); ct != "video/mp2t" {
		t.Fatalf("content-type segmen = %q", ct)
	}
	if rec := fetch("/hls/1/index.m3u8"); rec.Code != http.StatusNotFound {
		t.Fatalf("index lain = %d, want 404", rec.Code)
	}
}

// TestAddonProxy memastikan katalog/meta/streams diambil server: host yang
// diizinkan diteruskan apa adanya, host/skema lain ditolak.
func TestAddonProxy(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/catalog/series/top/search=a.json" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"metas":[{"id":"tt1","name":"Contoh"}]}`))
	}))
	defer upstream.Close()
	host := strings.TrimPrefix(upstream.URL, "http://")
	addonHosts[host] = true
	defer delete(addonHosts, host)

	gin.SetMode(gin.TestMode)
	srv := newServer(nil, memstorage.New(1<<20), nil, 0)
	r := gin.New()
	r.GET("/api/addon", srv.addonProxy)
	call := func(raw string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/addon?url="+url.QueryEscape(raw), nil))
		return rec
	}

	ok := call(upstream.URL + "/catalog/series/top/search=a.json")
	if ok.Code != http.StatusOK || !strings.Contains(ok.Body.String(), `"tt1"`) {
		t.Fatalf("addon diteruskan = %d %s", ok.Code, ok.Body.String())
	}
	if ct := ok.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("content-type = %q", ct)
	}
	for _, raw := range []string{
		"https://example.com/catalog/series/top/search=a.json",
		"file:///etc/passwd",
		upstream.URL + "/catalog/movie/top/search=b.json", // 404 dari addon
	} {
		if rec := call(raw); rec.Code != http.StatusBadRequest && rec.Code != http.StatusBadGateway {
			t.Fatalf("%s = %d, want 400/502", raw, rec.Code)
		}
	}
}

// TestCachedStateShared memastikan semua tab WS memakai satu snapshot 500 ms,
// jadi jumlah tab tidak mengalikan kerja per frame.
func TestCachedStateShared(t *testing.T) {
	srv := newServer(nil, memstorage.New(1<<20), nil, 0)
	srv.cachedState()
	srv.addWSUser("u1")
	if got := srv.cachedState()["users"].(gin.H)["active"]; got != 0 {
		t.Fatalf("snapshot cache: active = %v, want 0 (masih snapshot lama)", got)
	}
	srv.stateMu.Lock()
	srv.stateAt = time.Now().Add(-time.Second)
	srv.stateMu.Unlock()
	if got := srv.cachedState()["users"].(gin.H)["active"]; got != 1 {
		t.Fatalf("state segar: active = %v, want 1", got)
	}
}

// TestIndexHTMLBalanced menjaga markup embedded tetap seimbang: satu </div>
// berlebih pernah menutup <main> lebih awal sehingga player turun ke bawah
// semua kartu (browser mengabaikan tag penutup yatim dengan cara berbeda-beda).
func TestIndexHTMLBalanced(t *testing.T) {
	data, err := fs.ReadFile(webFiles, "web/index.html")
	if err != nil {
		t.Fatalf("baca index.html: %v", err)
	}
	html := string(data)
	opens := strings.Count(html, "<div") - strings.Count(html, "</div>") // bisa negatif
	if opens != 0 {
		t.Fatalf("div tidak seimbang: selisih %d", opens)
	}
	if got := strings.Count(html, "<section"); got != 2 {
		t.Fatalf("jumlah section = %d, want 2 (sidebar + player)", got)
	}
}
