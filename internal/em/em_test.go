package em

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// argvOf menyusun argv pipeline persis seperti Streams() melakukannya.
func argvOf(id string, tv *TVEpisode) []string {
	argv := []string{"-e", bunFreshScript}
	if tv != nil {
		argv = append(argv, id, "tv", fmt.Sprintf("%d-%d", tv.Season, tv.Episode))
	} else {
		argv = append(argv, id)
	}
	return argv
}

// TestStreamsParsesRows: output pipeline (baris JSON per server + baris
// done) di-parse jadi daftar Server; server tanpa data diberi Err, yang
// bisa diputar menulis kamus type hls/mp4 dan qual/subs.
func TestStreamsParsesRows(t *testing.T) {
	t.Chdir(t.TempDir())
	rows := []string{
		`{"server":"Premium WMovies","data":{"ok":true,"kind":"direct","url":"https://h/a.mp4","type":"progressive","quality":"FULL HD","qualities":[{"label":"480p","url":"https://h/480.mp4"}],"subtitles":[{"label":"Indonesia","lang":"id","url":"https://sub/id.vtt"}]}}`,
		`{"server":"Premium Nepu","data":{"ok":true,"url":"https://h/b.m3u8","type":"hls","quality":"1080p"}}`,
		`{"server":"Premium VidStore","data":null,"err":"badge atob"}`,
		`{"done":true,"resolved":2,"total":3,"meta":{}}`,
	}
	called := false
	c := New()
	c.Run = func(_ context.Context, argv []string) ([]byte, string, error) {
		called = true
		want := argvOf("557", nil)
		if fmt.Sprint(argv) != fmt.Sprint(want) {
			t.Fatalf("argv = %v, want %v", argv, want)
		}
		return []byte(strings.Join(rows, "\n")), "", nil
	}

	servers, err := c.Streams(context.Background(), "movie", "557", nil)
	if err != nil {
		t.Fatalf("Streams: %v", err)
	}
	if !called {
		t.Fatal("pipeline tidak dikenakan hit")
	}
	if len(servers) != 3 {
		t.Fatalf("malah %d server rewrite: %d", len(servers), len(servers))
	}
	if servers[0].Type != "mp4" || servers[0].URL != "https://h/a.mp4" {
		t.Fatalf("server 0 salah: %+v", servers[0])
	}
	if len(servers[0].Qualities) != 1 || servers[0].Qualities[0].Label != "480p" {
		t.Fatalf("qualities salah: %+v", servers[0].Qualities)
	}
	if len(servers[0].Subs) != 1 || servers[0].Subs[0].Lang != "id" {
		t.Fatalf("subs salah: %+v", servers[0].Subs)
	}
	if servers[1].Type != "hls" {
		t.Fatalf("server 1 type %q, want hls", servers[1].Type)
	}
	if servers[2].Err == "" || servers[2].URL != "" {
		t.Fatalf("server 2 harus gagal: %+v", servers[2])
	}
}

// TestStreamsTVArgs: untuk tv, season-episode dipeg di argv.
func TestStreamsTVArgs(t *testing.T) {
	t.Chdir(t.TempDir())
	var got []string
	c := New()
	c.Run = func(_ context.Context, argv []string) ([]byte, string, error) {
		got = argv
		return []byte(`{"server":"A","data":{"ok":true,"url":"https://h/a.mp4","type":"progressive"}}` + "\n" +
			`{"done":true,"resolved":1,"total":1,"meta":{}}` + "\n"), "", nil
	}
	if _, err := c.Streams(context.Background(), "tv", "283938", &TVEpisode{Season: 1, Episode: 4}); err != nil {
		t.Fatalf("Streams: %v", err)
	}
	if !strings.Contains(fmt.Sprint(got), "tv") || !strings.Contains(fmt.Sprint(got), "1-4") {
		t.Fatalf("argv tv salah: %v", got)
	}
}

// TestStreamsErrorFromFatal: bun kelainan -> pesan error memuat FATAL dari
// stderr, error relacionarsi 500-able (BadGateway) via prefix pipeline scrape.
func TestStreamsErrorFromFatal(t *testing.T) {
	t.Chdir(t.TempDir())
	c := New()
	c.Run = func(context.Context, []string) ([]byte, string, error) {
		return nil, "FATAL no obfuscated credential script", errors.New("exit status 1")
	}
	_, err := c.Streams(context.Background(), "movie", "1434581", nil)
	if err == nil {
		t.Fatal("harus gagal")
	}
	if !strings.Contains(err.Error(), "no obfuscated credential script") {
		t.Fatalf("pesan error tidak membawa FATAL: %v", err)
	}
}

// TestStreamsAllServersDead: belum ada satu server pun yang mencapai play
// URL -> error, dan hasilnya tidak di-cache.
func TestStreamsAllServersDead(t *testing.T) {
	t.Chdir(t.TempDir())
	c := New()
	c.Run = func(context.Context, []string) ([]byte, string, error) {
		return []byte(`{"server":"A","data":null,"err":"x"}` + "\n"), "", nil
	}
	if _, err := c.Streams(context.Background(), "movie", "557", nil); err == nil || !strings.Contains(err.Error(), "tidak ada server yang bisa diputar") {
		t.Fatalf("error salah: %v", err)
	}
	if _, ok := readCache("movie-557"); ok {
		t.Fatal("cache diisi padahal gagal")
	}
}

// TestStreamsCacheRoundtrip: cache diisi bridge trip, expire (TTL) di-stok
// lewat waktu "time.Past", dan lain-lain bila ttl lewat + TTL panjang.
func TestStreamsCacheRoundtrip(t *testing.T) {
	t.Chdir(t.TempDir())
	want := []Server{{Name: "A", Type: "mp4", URL: "https://h/a.mp4"}}
	id := New()
	id.Run = func(context.Context, []string) ([]byte, string, error) { return nil, "", errors.New("boot") }

	key := "movie-557"
	writeCache(key, want)
	got, ok := readCache(key)
	if !ok || len(got) != 1 || got[0].Name != "A" {
		t.Fatalf("cache baca salah: ok=%v %+v", ok, got)
	}

	// Expire: mundurkan mtime lewat cacheTTL, alias tidak fresh.
	path := cachePath(key)
	future := time.Now().Add(-cacheTTL - time.Minute)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	if _, ok := readCache(key); ok {
		t.Fatal("cache matakan tidak expire")
	}
}

// TestStreamsContextCanceled: ctx selesai -> langsung balik, tanpa run
// pipeline.
func TestStreamsContextCanceled(t *testing.T) {
	t.Chdir(t.TempDir())
	c := New()
	c.Run = func(context.Context, []string) ([]byte, string, error) {
		t.Fatal("pipeline tidak boleh jalan saat ctx sudah dead")
		return nil, "", nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Streams(ctx, "movie", "557", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

// TestCacheJSONDecodable round-trip hanya via pkg json yang putih: kecocokan
// struct -> file -> struct persis.
func TestCacheJSONDecodable(t *testing.T) {
	t.Chdir(t.TempDir())
	want := []Server{{Name: "B", Type: "hls", Quality: "1080p",
		Qualities: []Quality{{Label: "480p", URL: "u"}}, Subs: []Sub{{Lang: "id", URL: "v"}}}}
	writeCache("t/1", want)
	raw, err := os.ReadFile(cachePath("t/1"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var got []Server
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("Unmarshal: %v (%s)", err, raw)
	}
	if got[0].Subs[0].Lang != "id" || got[0].Qualities[0].URL != "u" {
		t.Fatalf("field salah: %+v", got)
	}
	if want := filepath.Join(cacheDir); !strings.HasPrefix(cachePath("t/1"), want) {
		t.Fatalf("cache lewat dir: %s", cachePath("t/1"))
	}
}

// TestTypeOf: pemetaan tipe follower klien.
func TestTypeOf(t *testing.T) {
	for got, want := range map[string]string{"hls": "hls", "m3u8": "hls", "progressive": "mp4", "": "mp4"} {
		if have := typeOf(got); have != want {
			t.Fatalf("typeOf(%q) = %q, want %q", got, have, want)
		}
	}
}
