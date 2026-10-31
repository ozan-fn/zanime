package em

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestStreamsMovieHappyPath: gerbang → POST tr= → kredensial → feed → resolve.
// Server yang gagal resolve tetap terdaftar dengan Err, yang berhasil lengkap
// dengan kualitas + subtitle; header ticket ikut terkirim.
func TestStreamsMovieHappyPath(t *testing.T) {
	t.Chdir(t.TempDir())
	s := newSite(t)
	s.episodes = s.episodesJSON()
	s.setFeeds(
		feedRow{Type: "server", SourceName: "Premium WMovies", SourceURL: "https://src/one"},
		feedRow{Type: "server", SourceName: "Premium Nepu", SourceURL: "https://src/two"},
		feedRow{Type: "thumb", SourceName: "bukan server", SourceURL: "https://src/ignored"},
	)
	s.plain = func(src string) (string, bool) {
		if src == "https://src/one" {
			return `{"ok":true,"kind":"direct","url":"https://h/a.mp4","type":"progressive",
				"quality":"FULL HD","qualities":[{"label":"480p","url":"https://h/480.mp4"}],
				"subtitles":[{"label":"Indonesia","lang":"id","url":"https://s/id.vtt"}]}`, true
		}
		return "", false
	}

	servers, err := newClient(s).Streams(t.Context(), "movie", "557", nil)
	if err != nil {
		t.Fatalf("Streams: %v", err)
	}
	if len(servers) != 2 {
		t.Fatalf("server = %d, want 2", len(servers))
	}
	got := servers[0]
	if got.Name != "Premium WMovies" || got.Type != "mp4" || got.URL != "https://h/a.mp4" || got.Quality != "FULL HD" {
		t.Fatalf("server 0 salah: %+v", got)
	}
	if len(got.Qualities) != 1 || got.Qualities[0].Label != "480p" {
		t.Fatalf("qualities salah: %+v", got.Qualities)
	}
	if len(got.Subs) != 1 || got.Subs[0].Lang != "id" {
		t.Fatalf("subs salah: %+v", got.Subs)
	}
	if servers[1].URL != "" || servers[1].Err == "" {
		t.Fatalf("server gagal harus ber-Err: %+v", servers[1])
	}

	gate, posts, feed, resolves := s.counts()
	if gate == 0 {
		t.Fatal("gerbang welcome tidak pernah dilewati")
	}
	if posts != 1 {
		t.Fatalf("POST gerbang = %d, want 1", posts)
	}
	if feed != 1 {
		t.Fatalf("feed = %d, want 1", feed)
	}
	// Resolve jalan paralel → urutan tak dijamin; periksa jumlah per src:
	// server gagal dicoba 2x (retry), yang sukses 1x.
	counts := map[string]int{}
	for _, r := range resolves {
		counts[r]++
	}
	if counts["https://src/one"] != 1 || counts["https://src/two"] != 2 {
		t.Fatalf("resolve srcs tidak sesuai: %v", resolves)
	}
	if s.headers.Get("X-EM-Ticket") != "ticket-abc" || s.headers.Get("X-Em-Player") != "1" {
		t.Fatalf("header resolve salah: %v", s.headers)
	}
	if s.headers.Get("Referer") == "" {
		t.Fatal("resolve tanpa Referer halaman player")
	}
}

// TestStreamsTVPicksEpisode: halaman tv pertama memuat daftar episode; yang
// diminta S1E2 → URL episode itulah yang dibuka untuk kredensial + feed.
func TestStreamsTVPicksEpisode(t *testing.T) {
	t.Chdir(t.TempDir())
	s := newSite(t)
	s.episodes = s.episodesJSON()
	s.plain = func(string) (string, bool) {
		return `{"ok":true,"kind":"direct","url":"https://h/tv.m3u8","type":"hls"}`, true
	}

	servers, err := newClient(s).Streams(t.Context(), "tv", "209867", &TVEpisode{Season: 1, Episode: 2})
	if err != nil {
		t.Fatalf("Streams: %v", err)
	}
	if servers[0].Type != "hls" || servers[0].URL != "https://h/tv.m3u8" {
		t.Fatalf("server tv salah: %+v", servers[0])
	}
}

// TestStreamsTVEpisodeMissing: episode yang tidak ada di daftar → error jelas.
func TestStreamsTVEpisodeMissing(t *testing.T) {
	t.Chdir(t.TempDir())
	s := newSite(t)
	s.episodes = s.episodesJSON()
	_, err := newClient(s).Streams(t.Context(), "tv", "209867", &TVEpisode{Season: 9, Episode: 9})
	if err == nil || !strings.Contains(err.Error(), "S9E9 tidak ada") {
		t.Fatalf("error = %v", err)
	}
}

// TestStreamsNoPlayable: semua server gagal → error, hasil tidak di-cache.
func TestStreamsNoPlayable(t *testing.T) {
	t.Chdir(t.TempDir())
	s := newSite(t)
	_, err := newClient(s).Streams(t.Context(), "movie", "557", nil)
	if err == nil || !strings.Contains(err.Error(), "tidak ada server yang bisa diputar") {
		t.Fatalf("error = %v", err)
	}
	if _, ok := readCache("movie-557"); ok {
		t.Fatal("cache diisi padahal tidak ada yang bisa diputar")
	}
}

// TestStreamsCacheRoundtrip: scrape pertama menembak jaringan, kedua dibaca
// dari ./tmp/em tanpa permintaan lagi; entri basi (mtime mundur > TTL) di-scrape
// ulang.
func TestStreamsCacheRoundtrip(t *testing.T) {
	t.Chdir(t.TempDir())
	s := newSite(t)
	s.plain = func(string) (string, bool) {
		return `{"ok":true,"kind":"direct","url":"https://h/a.mp4","type":"progressive"}`, true
	}
	c := newClient(s)
	if _, err := c.Streams(t.Context(), "movie", "557", nil); err != nil {
		t.Fatalf("Streams 1: %v", err)
	}
	_, _, feed1, _ := s.counts()
	if _, err := c.Streams(t.Context(), "movie", "557", nil); err != nil {
		t.Fatalf("Streams 2: %v", err)
	}
	_, _, feed2, _ := s.counts()
	if feed2 != feed1 {
		t.Fatalf("cache tidak dipakai: feed %d → %d", feed1, feed2)
	}

	stale := time.Now().Add(-cacheTTL - time.Minute)
	if err := os.Chtimes(cachePath("movie-557"), stale, stale); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Streams(t.Context(), "movie", "557", nil); err != nil {
		t.Fatalf("Streams 3: %v", err)
	}
	_, _, feed3, _ := s.counts()
	if feed3 != feed2+1 {
		t.Fatalf("cache basi tidak di-scrape ulang: feed %d", feed3)
	}
}

// TestStreamsContextCanceled: ctx sudah selesai → langsung balik, tanpa HTTP.
func TestStreamsContextCanceled(t *testing.T) {
	t.Chdir(t.TempDir())
	s := newSite(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := newClient(s).Streams(ctx, "movie", "557", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if gate, _, _, _ := s.counts(); gate != 0 {
		t.Fatalf("HTTP jalan padahal ctx batal (%d hit)", gate)
	}
}

// TestStreamsHTTPError: halaman embed 5xx → error memuat status, tanpa cache.
func TestStreamsHTTPError(t *testing.T) {
	t.Chdir(t.TempDir())
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer up.Close()
	c := New()
	c.Base = up.URL
	_, err := c.Streams(t.Context(), "movie", "557", nil)
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("error = %v", err)
	}
	if _, ok := readCache("movie-557"); ok {
		t.Fatal("cache diisi padahal gagal")
	}
}

// TestCredsErrors: halaman tanpa skrip kredensial, pola rusak, dan base64
// rusak semuanya jadi error, bukan panic.
func TestCredsErrors(t *testing.T) {
	if _, err := creds("<html>tanpa skrip</html>"); err == nil || !strings.Contains(err.Error(), "tanpa skrip kredensial") {
		t.Fatalf("error = %v", err)
	}
	bad := `<script>(0,eval)(decodeURIComponent(escape(atob(x)))); var a="@@@",b=[1,2],c=7;</script>`
	if _, err := creds(bad); err == nil {
		t.Fatal("base64 rusak harus error")
	}
	short := `<script>(0,eval)(decodeURIComponent(escape(atob(x))));</script>`
	if _, err := creds(short); err == nil {
		t.Fatal("pola tidak dikenal harus error")
	}
}

// TestDecodeCredMissingFields: JSON payload tanpa ticket/pk → error jelas.
func TestDecodeCredMissingFields(t *testing.T) {
	script := `<script>var a="` + b64XOR(`window.__EM_PLAYER__ = {"ticket":"","pk":"","feedUrl":"/f","resolveUrl":"/r"};`) +
		`",b=[7,200,13,99,4,255,61,3],c=59;(0,eval)(decodeURIComponent(escape(atob(a))));</script>`
	_, err := creds(script)
	if err == nil || !strings.Contains(err.Error(), "tidak lengkap") {
		t.Fatalf("error = %v", err)
	}
}

// b64XOR membalik XOR kredensial dengan kunci yang sama seperti credScript.
func b64XOR(payload string) string {
	key := []int{7, 200, 13, 99, 4, 255, 61, 3}
	raw := make([]byte, len(payload))
	for i := range payload {
		raw[i] = payload[i] ^ byte((59+key[i%len(key)]+i)&0xff)
	}
	return b64(raw)
}

// TestDecryptKnownVector: vektor nyata dari situs (blob resolve.php + pk +
// plaintext) — jaring pengaman kalau kripto ikut berubah.
func TestDecryptKnownVector(t *testing.T) {
	const (
		blob  = "MbarpHSl6S6tUNDEvNq05KgwJ42NMmvKGI-FHlNu4S41BFC5tBxFL7rmNqSo6OJz6e_coBCvU9t_tq5UhpgNLZ5sZqS-CzIAoE9ZzET2v3apiXC66g"
		pk    = "a99b1b825bfa12a2203050434ab40da55a833ed6f64c6ccfc433b6bc72c65851"
		plain = `{"ok":true,"kind":"embed","url":"https://luluvdo.com/e/i8gag31cxvmp"}`
	)
	got, err := decrypt(blob, pk)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if got != plain {
		t.Fatalf("plain =\n%s\nwant\n%s", got, plain)
	}
}

// TestDecryptRoundtrip: plaintext panjang (banyak blok 32 byte + sisa) bolak
// balik lewat encrypt/decrypt.
func TestDecryptRoundtrip(t *testing.T) {
	plain := strings.Repeat(`{"a":"b"}`, 40) + "sisa"
	blob, err := encrypt(plain, testPK)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := decrypt(blob, testPK); err != nil || got != plain {
		t.Fatalf("roundtrip: %q err=%v", got, err)
	}
}

// TestDecryptErrors: pk bukan hex, blob bukan base64url, dan blob terlalu
// pendek ditolak dengan pesan masing-masing.
func TestDecryptErrors(t *testing.T) {
	long, _ := encrypt("cukup panjang", testPK)
	cases := []struct{ name, blob, pk, want string }{
		{"pk", long, "zz", "pk bukan hex"},
		{"blob", "@@@", testPK, "base64url"},
		{"pendek", base64Raw([]byte("short")), testPK, "terlalu pendek"},
	}
	for _, tc := range cases {
		if _, err := decrypt(tc.blob, tc.pk); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: error = %v, want %q", tc.name, err, tc.want)
		}
	}
}

// TestTypeOf: pemetaan tipe untuk player.
func TestTypeOf(t *testing.T) {
	for got, want := range map[string]string{"hls": "hls", "m3u8": "hls", "progressive": "mp4", "": "mp4"} {
		if have := typeOf(got); have != want {
			t.Fatalf("typeOf(%q) = %q, want %q", got, have, want)
		}
	}
}

// TestEmbedURL: bentuk URL embed per tab.
func TestEmbedURL(t *testing.T) {
	c := New()
	if got := c.embedURL("movie", "557", nil); got != "https://embedmaster.link/movie/557?skin=onyx&welcome_page=on&autoplay=off" {
		t.Fatalf("movie = %q", got)
	}
	got := c.embedURL("tv", "209867", &TVEpisode{Season: 1, Episode: 2})
	if !strings.Contains(got, "/tv/209867/1/2?") {
		t.Fatalf("tv = %q", got)
	}
}

// TestFeedEmpty: feed tanpa baris server → error, bukan daftar kosong.
func TestFeedEmpty(t *testing.T) {
	s := newSite(t)
	s.setFeeds(feedRow{Type: "subtitle", SourceName: "x"})
	c := newClient(s)
	cfg := credCfg{Ticket: "t", PK: testPK, ScrapeToken: "-x", FeedURL: "/feed", ResolveURL: "/resolve.php"}
	if _, err := c.feed(t.Context(), cfg, s.srv.URL); err == nil || !strings.Contains(err.Error(), "feed embedmaster kosong") {
		t.Fatalf("error = %v", err)
	}
}

// TestParallelWorkerPool: fn dipanggil tepat sekali untuk tiap indeks, worker
// dibatasi, dan n == 0 tidak menjalankan apa pun.
func TestParallelWorkerPool(t *testing.T) {
	const n = 50
	var mu sync.Mutex
	seen := map[int]int{}
	parallel(n, 4, func(i int) {
		mu.Lock()
		seen[i]++
		mu.Unlock()
	})
	if len(seen) != n {
		t.Fatalf("indeks terlihat = %d, want %d", len(seen), n)
	}
	for i, c := range seen {
		if c != 1 {
			t.Fatalf("indeks %d dipanggil %d kali", i, c)
		}
	}
	parallel(0, 4, func(int) { t.Fatal("n=0 harus no-op") })
	parallel(2, 8, func(int) {}) // workers > n tidak masalah
}

// TestResolveRunsInParallel: resolve.php untuk beberapa server dijalankan
// serentak (goroutine) — server palsu menahan tiap resolve sampai beberapa
// di antaranya benar-benar masuk bersamaan, jadi test gagal kalau berurutan.
func TestResolveRunsInParallel(t *testing.T) {
	t.Chdir(t.TempDir())
	s := newSite(t)
	const want = 3
	s.entered = make(chan struct{}, 8)
	s.release = make(chan struct{})

	rows := make([]feedRow, 0, 6)
	for i := range 6 {
		rows = append(rows, feedRow{Type: "server", SourceName: fmt.Sprintf("S%d", i), SourceURL: fmt.Sprintf("https://src/%d", i)})
	}
	s.setFeeds(rows...)
	s.plain = func(string) (string, bool) {
		return `{"ok":true,"url":"https://h/a.mp4","type":"progressive"}`, true
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := newClient(s).Streams(context.Background(), "movie", "557", nil); err != nil {
			t.Errorf("Streams: %v", err)
		}
	}()

	// Harus ada `want` resolve yang masuk serentak sebelum dilepas.
	for i := range want {
		select {
		case <-s.entered:
		case <-time.After(3 * time.Second):
			close(s.release)
			t.Fatalf("resolve tidak paralel: hanya %d serentak", i)
		}
	}
	close(s.release)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Streams menggantung")
	}
	s.mu.Lock()
	max := s.maxInFlight
	s.mu.Unlock()
	if max < want {
		t.Fatalf("resolve serentak maks = %d, want >= %d", max, want)
	}
}

// TestResolveRetriesThenFails: resolve.php 502 dua kali → server ditandai
// gagal (Err), tidak menggagalkan seluruh permintaan.
func TestResolveRetriesThenFails(t *testing.T) {
	t.Chdir(t.TempDir())
	s := newSite(t)
	s.setFeeds(feedRow{Type: "server", SourceName: "A", SourceURL: "https://src/a"},
		feedRow{Type: "server", SourceName: "B", SourceURL: "https://src/b"})
	s.plain = func(src string) (string, bool) {
		if src == "https://src/b" {
			return `{"ok":true,"url":"https://h/b.mp4","type":"progressive"}`, true
		}
		return "", false
	}
	servers, err := newClient(s).Streams(t.Context(), "movie", "557", nil)
	if err != nil {
		t.Fatalf("Streams: %v", err)
	}
	if servers[0].Err == "" || servers[1].URL == "" {
		t.Fatalf("pemetaan gagal/sukses salah: %+v", servers)
	}
	_, _, _, resolves := s.counts()
	if len(resolves) != 3 { // A dicoba 2x, B 1x
		t.Fatalf("resolve attempts = %v, want 3", resolves)
	}
}

// TestCacheFileShape: cache JSON rapi (indent) dan bisa dibaca ulang.
func TestCacheFileShape(t *testing.T) {
	t.Chdir(t.TempDir())
	want := []Server{{Name: "A", Type: "hls", Quality: "1080p", Subs: []Sub{{Lang: "id", URL: "u"}}}}
	writeCache("tv-1-1-2", want)
	raw, err := os.ReadFile(cachePath("tv-1-1-2"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "\n ") {
		t.Fatalf("cache tidak ter-indent: %s", raw)
	}
	got, ok := readCache("tv-1-1-2")
	if !ok || len(got) != 1 || got[0].Subs[0].Lang != "id" {
		t.Fatalf("baca cache salah: %+v", got)
	}
	var back []Server
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("cache bukan JSON: %v", err)
	}
}
