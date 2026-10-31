package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"zanime/internal/config"
	"zanime/internal/em"
	"zanime/internal/resolve"
	"zanime/internal/tmdb"
)

//go:embed web/dist
var webFiles embed.FS

// proxyClient streams remote media through RAM — no overall timeout, playback
// lasts as long as the player keeps reading.
//
// Transport di-tune untuk segmen HLS yang banyak dan sering paralel: HTTP/2
// diaktifkan (DialContext kustom mematikannya kalau ForceAttemptHTTP2 tidak
// diset) supaya banyak segmen di-multiplex lewat satu koneksi, dan batas idle
// per host dinaikkan supaya tidak membuka TLS handshake baru tiap segmen.
var proxyClient = &http.Client{
	Transport: &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		DialContext:         (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:   true,
		MaxIdleConns:        256,
		MaxIdleConnsPerHost: 64,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
		// Media tidak perlu dikompresi; hindari negosiasi gzip di jalur panas.
		DisableCompression: true,
	},
}

func main() {
	if err := config.LoadDotEnv(".env"); err != nil {
		log.Printf(".env: %v", err)
	}

	addr := flag.String("addr", "0.0.0.0:"+config.String("PORT", "8080"), "HTTP listen address")
	flag.Parse()

	webRoot, err := fs.Sub(webFiles, "web/dist")
	if err != nil {
		log.Fatalf("embedded web files: %v", err)
	}
	res := resolve.New()
	res.Tidy() // buang slug lama warisan run sebelumnya (./tmp/resolve rapi)
	srv := &server{web: webRoot, tmdb: tmdb.New(), em: em.New(), res: res}

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.GET("/", srv.index)
	r.GET("/assets/*filepath", srv.webFile) // aset Vite (JS/CSS)
	r.GET("/favicon.ico", srv.webFile)
	// SPA fallback: React Router punya route `/` dan `*` (404), jadi path
	// lain harus tetap dapat index.html; endpoint API yang salah tulis tetap
	// JSON 404 supaya error di sisi client mudah dibedakan.
	r.NoRoute(srv.spaFallback)
	r.GET("/healthz", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	r.GET("/api/search", srv.apiSearch)
	r.GET("/api/meta/:tab/:id", srv.apiMeta)
	r.GET("/api/streams", srv.apiStreams)
	r.GET("/s/:slug", srv.streamProxy)
	r.HEAD("/s/:slug", srv.streamProxy)

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("listen %s: %v", *addr, err)
	}
	log.Printf("listening on %s", ln.Addr())
	if err := r.RunListener(ln); err != nil {
		log.Fatal(err)
	}
}

// streamer adalah irisan em.Client yang dipakai server (mudah di-stub test).
type streamer interface {
	Streams(ctx context.Context, tab, id string, tv *em.TVEpisode) ([]em.Server, error)
}

type server struct {
	web  fs.FS
	tmdb *tmdb.Client
	em   streamer
	res  *resolve.Pool
}

// SPA fallback: path GET yang tak dikenal dapat index.html (React Router
// yang menampilkan 404), kecuali path API/fetch yang salah tulis —
// itu tetap JSON 404 supaya error di sisi client mudah dibedakan.
func (s *server) spaFallback(c *gin.Context) {
	p := c.Request.URL.Path
	if strings.Contains(p, "/api/") || p == "/healthz" || strings.HasPrefix(p, "/s/") {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	s.index(c)
}

// File statis hasil build Vite (aset di /assets/*, favicon).
func (s *server) webFile(c *gin.Context) {
	name := strings.TrimPrefix(c.Request.URL.Path, "/")
	if name == "" || name == "index.html" {
		s.index(c)
		return
	}
	data, err := fs.ReadFile(s.web, name)
	if err != nil {
		c.String(http.StatusNotFound, "tidak ada")
		return
	}
	switch {
	case strings.HasSuffix(name, ".js"):
		c.Data(http.StatusOK, "text/javascript; charset=utf-8", data)
	case strings.HasSuffix(name, ".css"):
		c.Data(http.StatusOK, "text/css; charset=utf-8", data)
	case strings.HasSuffix(name, ".svg"):
		c.Data(http.StatusOK, "image/svg+xml", data)
	default:
		c.Data(http.StatusOK, "application/octet-stream", data)
	}
}

func (s *server) index(c *gin.Context) {
	// Not FileFromFS: http.FileServer redirects *index.html to ".", which
	// loops forever on "/". Serve the bytes directly instead.
	data, err := fs.ReadFile(s.web, "index.html")
	if err != nil {
		c.String(http.StatusInternalServerError, "index.html tidak ditemukan")
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", data)
}

// apiSearch: pencarian judul. Backend yang menghubungi sumber catalog; hasil
// membawa id TMDB (moviedb_id) yang dibutuhkan embedmaster.
func (s *server) apiSearch(c *gin.Context) {
	q := strings.TrimSpace(c.Query("q"))
	tab := c.Query("tab")
	if q == "" || (tab != "movie" && tab != "tv") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "query harus ?q=<judul>&tab=movie|tv"})
		return
	}
	res, err := s.tmdb.Search(c.Request.Context(), tab, q)
	if err != nil {
		s.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"results": res})
}

// apiMeta: detail satu judul (movie: metadata; tv: metadata + daftar episode).
func (s *server) apiMeta(c *gin.Context) {
	tab, id := c.Param("tab"), c.Param("id")
	if (tab != "movie" && tab != "tv") || !strings.HasPrefix(id, "tt") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "meta harus /api/meta/movie|tv/<imdb-id>"})
		return
	}
	res, err := s.tmdb.Meta(c.Request.Context(), tab, id)
	if err != nil {
		s.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

// apiStreams: daftar stream hasil scraping embedmaster untuk satu TMDB id
// (movie) atau satu episode tv. Fetch/dekripsi terjadi di Go (internal/em).
func (s *server) apiStreams(c *gin.Context) {
	tab := c.Query("tab")
	id := strings.TrimSpace(c.Query("id"))
	if (tab != "movie" && tab != "tv") || id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "streams harus ?tab=movie|tv&id=<tmdb-id>"})
		return
	}
	var tv *em.TVEpisode
	if tab == "tv" {
		ses, err1 := strconv.Atoi(c.Query("s"))
		epi, err2 := strconv.Atoi(c.Query("e"))
		if err1 != nil || err2 != nil || ses < 0 || epi < 1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "tv butuh ?s=<season>&e=<episode>"})
			return
		}
		tv = &em.TVEpisode{Season: ses, Episode: epi}
	}
	res, err := s.em.Streams(c.Request.Context(), tab, id, tv)
	if err != nil {
		s.fail(c, err)
		return
	}
	// Semua URL didaftarkan ke pool slug; player/subtitle hanya melihat
	// /s/<slug> pendek, tidak ada URL ter-encode panjang di network tab.
	for i := range res {
		srv := &res[i]
		hint := strings.ToLower(srv.Name)
		if srv.URL != "" {
			srv.URL = "/s/" + s.res.Register(srv.URL, hint)
		}
		for j := range srv.Qualities {
			q := &srv.Qualities[j]
			if q.URL != "" {
				q.URL = "/s/" + s.res.Register(q.URL, hint+"-"+q.Label)
			}
		}
		for j := range srv.Subs {
			sb := &srv.Subs[j]
			if sb.URL != "" {
				sb.URL = "/s/" + s.res.Register(sb.URL, hint+"-sub-"+sb.Lang)
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{"id": id, "tab": tab, "servers": res})
}

func (s *server) fail(c *gin.Context, err error) {
	status := http.StatusBadGateway
	if errors.Is(err, context.DeadlineExceeded) {
		status = http.StatusGatewayTimeout
	}
	c.JSON(status, gin.H{"error": err.Error()})
}

// streamProxy menyalurkan satu URL media terdaftar lewat RAM (tanpa disk),
// tetap meneruskan Range supaya player bisa seek. Playlist teks (m3u8)
// ditulis ulang: setiap URI (playlist turun / segmen / URI= di tag)
// didaftarkan jadi slug /s/<slug>, karena player tidak bisa meminta
// playlist turunan langsung ke host video tanpa Referer.
func (s *server) streamProxy(c *gin.Context) {
	raw, ok := s.res.Load(c.Param("slug"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "slug tidak ada"})
		return
	}
	target, err := url.Parse(raw)
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") {
		c.JSON(http.StatusNotFound, gin.H{"error": "slug tidak ada"})
		return
	}

	req, err := http.NewRequestWithContext(c.Request.Context(), c.Request.Method, target.String(), nil)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "URL tidak valid"})
		return
	}
	if rng := c.GetHeader("Range"); rng != "" {
		req.Header.Set("Range", rng)
	}
	// Host video embedmaster menolak request tanpa Referer halaman player
	// mereka; Referer ini juga membuka Access-Control-Allow-Origin.
	req.Header.Set("User-Agent", "Mozilla/5.0 zanime")
	req.Header.Set("Referer", "https://embdmstrplayer.com/")

	res, err := proxyClient.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "gagal menghubungi upstream: " + err.Error()})
		return
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode < 200 || res.StatusCode >= 400 {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("upstream HTTP %s", res.Status)})
		return
	}

	if isPlaylist(res.Header.Get("Content-Type"), target.Path) {
		body, err := io.ReadAll(res.Body)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
		// VOD (ada #EXT-X-ENDLIST) tidak berubah → biarkan browser meng-cache;
		// playlist live tetap no-store. Master playlist bisa 1+ MB, jadi
		// meng-cache menghindari fetch ulang besar tiap player membacanya.
		if strings.Contains(string(body), "#EXT-X-ENDLIST") {
			c.Header("Cache-Control", "public, max-age=600")
		} else {
			c.Header("Cache-Control", "no-store")
		}
		c.Data(http.StatusOK, "application/vnd.apple.mpegurl", []byte(s.rewritePlaylist(target, string(body))))
		return
	}

	// Subtitle: parser movi-player hanya mengenali timestamp HH:MM:SS.mmm,
	// padahal banyak CDN subtitle mengirim MM:SS.mmm (atau jam satu digit),
	// sehingga track kosong (0 cue) walau URL-nya sehat. Normalisasi di sini.
	if isSubtitle(res.Header.Get("Content-Type"), target.Path) {
		body, err := io.ReadAll(res.Body)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
		c.Data(http.StatusOK, "text/vtt; charset=utf-8", []byte(normalizeVTT(string(body))))
		return
	}

	for _, h := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges"} {
		if v := res.Header.Get(h); v != "" {
			c.Header(h, v)
		}
	}
	c.Status(res.StatusCode)
	if c.Request.Method != http.MethodHead {
		_, _ = io.Copy(c.Writer, res.Body)
	}
}

func isPlaylist(ct, p string) bool {
	return strings.Contains(ct, "mpegurl") || strings.HasSuffix(p, ".m3u8")
}

// isSubtitle: track teks VTT/SRT (host subtitle mengirim text/vtt, kadang
// extension .vtt/.srt tanpa content-type yang jelas).
func isSubtitle(ct, p string) bool {
	ct = strings.ToLower(ct)
	return strings.Contains(ct, "text/vtt") || strings.Contains(ct, "/vtt") ||
		strings.Contains(ct, "x-subrip") || strings.Contains(ct, "/srt") ||
		strings.HasSuffix(p, ".vtt") || strings.HasSuffix(p, ".srt")
}

var (
	// Timestamp berjam (jam boleh satu digit): 1:23:45.000.
	vttFullTS = regexp.MustCompile(`(\d{1,2}):(\d{2}):(\d{2})[.,](\d{1,3})`)
	// Timestamp tanpa jam: 01:18.579; awalan wajib bukan digit/kolon supaya
	// ekor timestamp berjam tidak ikut tertangkap.
	vttShortTS = regexp.MustCompile(`(^|[^\d:])(\d{1,2}):(\d{2})[.,](\d{1,3})`)
)

// normalizeVTT memaksa semua timestamp pada baris cue jadi HH:MM:SS.mmm —
// bentuk yang dikenali parser movi-player. Baris non-cue dibiarkan apa adanya.
func normalizeVTT(body string) string {
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		if !strings.Contains(line, "-->") {
			continue
		}
		line = vttFullTS.ReplaceAllStringFunc(line, func(m string) string {
			g := vttFullTS.FindStringSubmatch(m)
			return pad2(g[1]) + ":" + g[2] + ":" + g[3] + "." + padMillis(g[4])
		})
		line = vttShortTS.ReplaceAllStringFunc(line, func(m string) string {
			g := vttShortTS.FindStringSubmatch(m)
			return g[1] + "00:" + pad2(g[2]) + ":" + g[3] + "." + padMillis(g[4])
		})
		lines[i] = line
	}
	return strings.Join(lines, "\n")
}

func pad2(s string) string {
	if len(s) == 1 {
		return "0" + s
	}
	return s
}

// padMillis memadankan milidetik ke 3 digit dari kanan: ".5" = 500 ms (bukan
// 5 ms), karena itu pecahan detik.
func padMillis(s string) string {
	for len(s) < 3 {
		s += "0"
	}
	return s
}

// rewritePlaylist menulis ulang satu playlist m3u8 ke slug pool. Baris yang
// bukan komentar dan bukan tag (#) adalah URI; URI=\"...\" di dalam tag
// (#EXT-X-KEY, #EXT-X-MEDIA) juga ditulis ulang.
func (s *server) rewritePlaylist(base *url.URL, body string) string {
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			if idx := strings.Index(trimmed, "URI=\""); idx != -1 {
				rest := trimmed[idx+len("URI=\""):]
				if end := strings.Index(rest, "\""); end > 0 {
					abs := resolveRef(base, rest[:end])
					lines[i] = trimmed[:idx] + "URI=\"" + "/s/" + s.res.RegisterTemp(abs) + "\"" + rest[end+1:]
				}
			}
			continue
		}
		lines[i] = "/s/" + s.res.RegisterTemp(resolveRef(base, trimmed))
	}
	return strings.Join(lines, "\n")
}

func resolveRef(base *url.URL, ref string) string {
	if u, err := url.Parse(ref); err == nil {
		return base.ResolveReference(u).String()
	}
	return ref
}
