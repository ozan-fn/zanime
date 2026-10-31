package em

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// TestSniffMedia: playlist HLS, halaman HTML, JSON, dan byte MP4 dipetakan ke
// jenis yang benar; body diperiksa lebih dulu karena sebagian host salah kirim
// Content-Type untuk m3u8.
func TestSniffMedia(t *testing.T) {
	cases := []struct {
		name string
		ct   string
		body []byte
		want string
	}{
		{"hls-body-html-ct", "text/html", []byte("#EXTM3U\n#EXT-X-ENDLIST\n"), "hls"},
		{"hls-mpegurl", "application/vnd.apple.mpegurl", []byte("#EXTM3U\n"), "hls"},
		{"html", "text/html; charset=UTF-8", []byte("<!DOCTYPE html><html>"), ""},
		{"html-no-ct", "", []byte("<html><body>x</body></html>"), ""},
		{"json", "application/json", []byte(`{"ok":false}`), ""},
		{"mp4-ct", "video/mp4", []byte("ftypmp42"), "mp4"},
		{"mp4-magic", "application/octet-stream", append([]byte{0, 0, 0, 0x18}, []byte("ftypmp42")...), "mp4"},
		{"audio", "audio/mpeg", []byte{0xff, 0xfb, 0x90}, "mp4"},
		{"empty", "", nil, ""},
	}
	for _, tc := range cases {
		if got := sniffMedia(tc.ct, tc.body); got != tc.want {
			t.Fatalf("%s: sniffMedia = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestVerifyStream: URL halaman embed ditolak tanpa HTTP; media nyata lolos.
func TestVerifyStream(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/media.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = w.Write([]byte("#EXTM3U\n"))
		case "/media.mp4":
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte("ftypmp42"))
		case "/page":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<!DOCTYPE html><html>embed</html>"))
		default:
			w.WriteHeader(http.StatusForbidden)
		}
	}))
	defer up.Close()
	c := New()

	for path, want := range map[string]string{"/media.m3u8": "hls", "/media.mp4": "mp4"} {
		kind, ok := c.verifyStream(t.Context(), "test", up.URL+path)
		if !ok || kind != want {
			t.Fatalf("verifyStream(%s) = %q %v, want %q", path, kind, ok, want)
		}
	}
	for _, bad := range []string{up.URL + "/page", up.URL + "/gone", up.URL + "/e/abc123"} {
		if kind, ok := c.verifyStream(t.Context(), "test", bad); ok {
			t.Fatalf("verifyStream(%s) harus gagal, dapat %q", bad, kind)
		}
	}
	// URL halaman embed ditolak tanpa menyentuh jaringan.
	if kind, ok := c.verifyStream(t.Context(), "test", "https://mixdrop21.net/e/677mw81eu69z6l"); ok {
		t.Fatalf("URL embed harus ditolak, dapat %q", kind)
	}
}

// captureLog merekam baris log (id, level, teks) untuk asersi pipeline.
type captureLog struct {
	mu   sync.Mutex
	msgs []string
}

func (l *captureLog) Log(id, level, msg string) {
	l.mu.Lock()
	l.msgs = append(l.msgs, level+" "+msg)
	l.mu.Unlock()
}

func (l *captureLog) joined() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.msgs, "\n")
}

// TestVerifyStreamLogs: uji stream menulis satu baris "sedang" lalu hasilnya
// di id yang sama, dengan detail (host, HTTP, jenis, ukuran).
func TestVerifyStreamLogs(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		_, _ = w.Write([]byte("#EXTM3U\n#EXT-X-ENDLIST\nseg.ts\n"))
	}))
	defer up.Close()

	c := New()
	rec := &captureLog{}
	c.SetLogger(rec)
	kind, ok := c.verifyStream(t.Context(), "srv:X:verify", up.URL+"/master.m3u8")
	if !ok || kind != "hls" {
		t.Fatalf("verifyStream = %q %v", kind, ok)
	}
	got := rec.joined()
	for _, want := range []string{"uji ", "probe 128 KiB", "HTTP 200", "hls"} {
		if !strings.Contains(got, want) {
			t.Fatalf("log tanpa %q:\n%s", want, got)
		}
	}
}

// TestScrapeLogsSteps: pipeline penuh mengirim baris per tahap (halaman,
// kredensial, feed, resolve, verifikasi) dan hanya server yang lolos uji yang
// tercatat "siap diputar".
func TestScrapeLogsSteps(t *testing.T) {
	t.Chdir(t.TempDir())
	s := newSite(t)
	s.setFeeds(
		feedRow{Type: "server", SourceName: "Premium WMovies", SourceURL: "https://src/good"},
		feedRow{Type: "server", SourceName: "MixDrop", SourceURL: "https://src/embed"},
	)
	s.plain = func(src string) (string, bool) {
		switch src {
		case "https://src/good":
			return `{"ok":true,"url":"` + s.media("a.mp4") + `","type":"progressive"}`, true
		case "https://src/embed":
			return `{"ok":true,"url":"` + s.srv.URL + `/media/html/page","type":"progressive"}`, true
		}
		return "", false
	}

	c := newClient(s)
	rec := &captureLog{}
	c.SetLogger(rec)
	if _, err := c.Streams(t.Context(), "movie", "557", nil); err != nil {
		t.Fatalf("Streams: %v", err)
	}
	got := rec.joined()
	for _, want := range []string{"halaman player siap", "kredensial terdekode", "feed: 2 server", "Premium WMovies: resolve", "MixDrop disembunyikan", "Premium WMovies siap diputar"} {
		if !strings.Contains(got, want) {
			t.Fatalf("log tanpa %q:\n%s", want, got)
		}
	}
}

// TestStreamsHidesUnplayable: server yang resolve-nya menunjuk halaman embed
// (HTML) tidak ditampilkan (Err), yang medianya nyata tetap tampil; jenis
// hasil verifikasi menimpa tebakan resolve.
func TestStreamsHidesUnplayable(t *testing.T) {
	t.Chdir(t.TempDir())
	s := newSite(t)
	s.setFeeds(
		feedRow{Type: "server", SourceName: "MixDrop", SourceURL: "https://src/embed"},
		feedRow{Type: "server", SourceName: "Premium WMovies", SourceURL: "https://src/good"},
	)
	s.plain = func(src string) (string, bool) {
		switch src {
		case "https://src/embed":
			return `{"ok":true,"kind":"embed","url":"` + s.srv.URL + `/media/html/page","type":"progressive"}`, true
		case "https://src/good":
			// URL tanpa ekstensi, tapi isinya playlist → verifikasi mengoreksi ke hls.
			return `{"ok":true,"kind":"direct","url":"` + s.srv.URL + `/media/mislabeled","type":"progressive"}`, true
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
	if servers[0].Name != "MixDrop" || servers[0].Err == "" || servers[0].URL != "" {
		t.Fatalf("server embed harus disembunyikan: %+v", servers[0])
	}
	if servers[1].Err != "" || servers[1].URL == "" {
		t.Fatalf("server media harus tampil: %+v", servers[1])
	}
	if servers[1].Type != "hls" {
		t.Fatalf("verifikasi harus mengoreksi type ke hls: %+v", servers[1])
	}
}
