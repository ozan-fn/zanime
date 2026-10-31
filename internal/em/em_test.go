package em

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

const testPK = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"

// credScript membalik obfuscation skrip kredensial halaman player:
// payload JSON → XOR (base + key[i%len] + i) → base64 → skrip.
func credScript(t *testing.T, cfg credCfg) string {
	t.Helper()
	buf, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	payload := "window.__EM_PLAYER__ = " + string(buf) + ";"
	key := []int{7, 200, 13, 99, 4, 255, 61, 3}
	raw := make([]byte, len(payload))
	for i := range payload {
		raw[i] = payload[i] ^ byte((59+key[i%len(key)]+i)&0xff)
	}
	parts := make([]string, len(key))
	for i, k := range key {
		parts[i] = strconv.Itoa(k)
	}
	return `<script>var _a="` + base64.StdEncoding.EncodeToString(raw) +
		`",_b=[` + strings.Join(parts, ",") + `],_c=59;(0,eval)(decodeURIComponent(escape(atob(_a))));</script>`
}

// encrypt membalik decrypt: keystream SHA256(key‖nonce‖counter) di-XOR ke
// plaintext, hasilnya nonce16‖ct dalam base64url.
func encrypt(plain, pk string) (string, error) {
	key, err := hex.DecodeString(pk)
	if err != nil {
		return "", err
	}
	nonce := []byte("0123456789abcdef")
	out := make([]byte, len(plain))
	for i := 0; i*32 < len(plain); i++ {
		var ctr [4]byte
		ctr[0], ctr[1], ctr[2], ctr[3] = byte(i>>24), byte(i>>16), byte(i>>8), byte(i)
		h := sha256.New()
		h.Write(key)
		h.Write(nonce)
		h.Write(ctr[:])
		block := h.Sum(nil)
		for j := 0; j < 32 && i*32+j < len(plain); j++ {
			out[i*32+j] = plain[i*32+j] ^ block[j]
		}
	}
	return base64.RawURLEncoding.EncodeToString(append(append([]byte{}, nonce...), out...)), nil
}

// site meniru embedmaster: gerbang welcome, halaman player ber-kredensial,
// feed NDJSON, dan resolve.php terenkripsi.
type site struct {
	mu       sync.Mutex
	gateHits int
	posts    int
	feedHits int
	resolves []string
	headers  http.Header
	episodes string // diisi test: daftar episode untuk halaman tv
	plain    func(src string) (string, bool)
	feeds    []feedRow
	srv      *httptest.Server

	// Rendezvous untuk test paralelisme: resolve masuk menandai `entered`
	// lalu menunggu `release` ditutup (kalau keduanya non-nil).
	entered     chan struct{}
	release     chan struct{}
	inFlight    int
	maxInFlight int
}

func newSite(t *testing.T) *site {
	t.Helper()
	s := &site{feeds: []feedRow{{Type: "server", SourceName: "Premium A", SourceURL: "https://src/a"}}}
	s.plain = func(string) (string, bool) { return "", false }
	mux := http.NewServeMux()
	mux.HandleFunc("/feed", func(w http.ResponseWriter, _ *http.Request) {
		s.mu.Lock()
		s.feedHits++
		rows := append([]feedRow{}, s.feeds...)
		s.mu.Unlock()
		for _, row := range rows {
			b, _ := json.Marshal(row)
			_, _ = w.Write(append(b, '\n'))
		}
	})
	mux.HandleFunc("/resolve.php", func(w http.ResponseWriter, r *http.Request) {
		src := strings.TrimPrefix(r.URL.RawQuery, "src=")
		s.mu.Lock()
		s.resolves = append(s.resolves, src)
		s.headers = r.Header.Clone()
		s.inFlight++
		if s.inFlight > s.maxInFlight {
			s.maxInFlight = s.inFlight
		}
		entered, release := s.entered, s.release
		s.mu.Unlock()
		defer func() { s.mu.Lock(); s.inFlight--; s.mu.Unlock() }()
		if entered != nil {
			select {
			case entered <- struct{}{}:
			default:
			}
			if release != nil {
				<-release
			}
		}
		plain, ok := s.plain(src)
		if !ok {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		blob, err := encrypt(plain, testPK)
		if err != nil {
			t.Errorf("encrypt: %v", err)
		}
		_, _ = w.Write([]byte(blob))
	})
	// Halaman episode terpilih (dilakukan setelah tv memilih episode).
	mux.HandleFunc("/play/", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.gateHits++
		s.mu.Unlock()
		_, _ = w.Write([]byte(s.playerHTML(t, false)))
	})
	// URL media ter-resolve: dipakai verifikasi stream. Ekstensi .m3u8 →
	// playlist; selain itu byte MP4 (kotak ftyp). Path /media/html/ → halaman
	// embed (bukan media) untuk menguji penyaringan.
	mux.HandleFunc("/media/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, ".m3u8"):
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = w.Write([]byte("#EXTM3U\n#EXT-X-ENDLIST\nseg1.ts\n"))
		case strings.Contains(r.URL.Path, "/media/html/"):
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<!DOCTYPE html><html><body>embed</body></html>"))
		case strings.Contains(r.URL.Path, "/media/mislabeled"):
			// Playlist sah tapi Content-Type-nya text/html (seperti cdn embedmaster).
			w.Header().Set("Content-Type", "text/html; charset=UTF-8")
			_, _ = w.Write([]byte("#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=4000\nchild.m3u8\n"))
		default:
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write(append([]byte{0, 0, 0, 0x18}, []byte("ftypmp42")...))
		}
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		isTV := strings.HasPrefix(r.URL.Path, "/tv/")
		// Gerbang: tanpa cookie → halaman welcome (tanpa kredensial).
		if _, err := r.Cookie("g"); err != nil {
			s.mu.Lock()
			s.gateHits++
			s.mu.Unlock()
			http.SetCookie(w, &http.Cookie{Name: "g", Value: "1", Path: "/"})
			_, _ = w.Write([]byte("<html>welcome gate v2/assets</html>"))
			return
		}
		if r.Method == http.MethodPost {
			s.mu.Lock()
			s.posts++
			s.mu.Unlock()
			if r.FormValue("tr") == "" {
				t.Errorf("POST gerbang tanpa tr=: %v", r.Form)
			}
			if r.Header.Get("Origin") == "" {
				t.Errorf("POST gerbang tanpa Origin")
			}
		}
		_, _ = w.Write([]byte(s.playerHTML(t, isTV)))
	})
	s.srv = httptest.NewServer(mux)
	t.Cleanup(s.srv.Close)
	return s
}

// playerHTML: halaman player lengkap (kredensial + opsional daftar episode).
func (s *site) playerHTML(t *testing.T, withEpisodes bool) string {
	t.Helper()
	eps := ""
	if withEpisodes {
		eps = s.episodes
	}
	return `<html>v2/assets` + eps + credScript(t, credCfg{
		Ticket:      "ticket-abc",
		PK:          testPK,
		ScrapeToken: "-tok",
		FeedURL:     "/feed?",
		ResolveURL:  "/resolve.php",
	}) + `</html>`
}

// episodesJSON menyusun __EM_EPISODES__ dengan dua episode di server palsu.
func (s *site) episodesJSON() string {
	return `<script>window.__EM_EPISODES__ = {"showTitle":"Frieren","seasons":[{"season":1,` +
		`"episodes":[{"episode":1,"title":"End","url":"` + s.srv.URL + `/play/1/1"},` +
		`{"episode":2,"title":"Magic","url":"` + s.srv.URL + `/play/1/2"}]}]};</script>`
}

func newClient(s *site) *Client {
	c := New()
	c.Base = s.srv.URL
	return c
}

func (s *site) counts() (gate, posts, feed int, resolves []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gateHits, s.posts, s.feedHits, append([]string{}, s.resolves...)
}

// setFeeds mengganti baris feed yang disajikan server palsu.
func (s *site) setFeeds(rows ...feedRow) {
	s.mu.Lock()
	s.feeds = rows
	s.mu.Unlock()
}

// media adalah URL media di server palsu (bisa diverifikasi verifyStream).
func (s *site) media(name string) string { return s.srv.URL + "/media/" + name }

// b64 dan base64Raw dipakai test untuk membangun/memeriksa blob.
func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func base64Raw(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
