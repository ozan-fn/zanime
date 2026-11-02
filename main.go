package main

import (
	"archive/zip"
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-resty/resty/v2"
	"github.com/gorilla/websocket"

	"zanime/internal/config"
	"zanime/internal/em"
	"zanime/internal/logbus"
	"zanime/internal/resolve"
	"zanime/internal/subdl"
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
var proxyClient = resty.NewWithClient(&http.Client{
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
})

// subHTTP mengambil zip subtitle subdl (ukuran kecil, bukan streaming).
var subHTTP = resty.New().SetTimeout(30 * time.Second)

func main() {
	if err := config.LoadDotEnv(".env"); err != nil {
		log.Printf(".env: %v", err)
	}
	// cPanel membatasi jumlah proses (LVE nproc, mis. 100), jadi paralelisme
	// runtime dipatok ke GOMAXPROCS (default 30) — tidak pernah di atasnya.
	cpus := runtime.NumCPU()
	limit := int(config.Int("GOMAXPROCS", 30))
	procs := min(cpus, limit)
	runtime.GOMAXPROCS(procs)
	log.Printf("GOMAXPROCS=%d dari %d CPU (batas env %d): paralelisme dibatasi supaya nproc cPanel/LVE tidak tembus", procs, cpus, limit)

	addr := flag.String("addr", "0.0.0.0:"+config.String("PORT", "8080"), "HTTP listen address")
	flag.Parse()

	webRoot, err := fs.Sub(webFiles, "web/dist")
	if err != nil {
		log.Fatalf("embedded web files: %v", err)
	}
	res := resolve.New()
	res.Tidy() // buang slug lama warisan run sebelumnya (./tmp/resolve rapi)
	logs := logbus.New()
	emc := em.New()
	emc.SetLogger(hubLogger{hub: logs})
	srv := &server{web: webRoot, tmdb: tmdb.New(), em: emc, res: res, logs: logs, sub: subdl.New()}

	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Get("/", srv.index)
	r.Get("/assets/*", srv.webFile) // aset Vite (JS/CSS)
	r.Get("/favicon.ico", srv.webFile)
	// SPA fallback: React Router punya route `/` dan `*` (404), jadi path
	// lain harus tetap dapat index.html; endpoint API yang salah tulis tetap
	// JSON 404 supaya error di sisi client mudah dibedakan.
	r.NotFound(srv.spaFallback)
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	r.Get("/api/search", srv.apiSearch)
	r.Get("/api/meta/{tab}/{id}", srv.apiMeta)
	r.Get("/api/streams", srv.apiStreams)
	r.Get("/api/subtitles", srv.apiSubtitles)
	r.Get("/api/subtitles/file", srv.apiSubtitleFile)
	r.Get("/api/logs", srv.apiLogs) // websocket: log realtime ke UI
	r.Get("/s/{slug}", srv.streamProxy)
	r.Head("/s/{slug}", srv.streamProxy)

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("listen %s: %v", *addr, err)
	}
	log.Printf("listening on %s", ln.Addr())
	if err := http.Serve(ln, r); err != nil {
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
	sub  *subdl.Client
	res  *resolve.Pool
	logs *logbus.Hub
}

// upgrader WebSocket: aplikasi lokal satu-origin, jadi semua Origin diterima
// (dev Vite juga boleh menyambung lewat proxy).
var upgrader = websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}

// writeJSON mengirim satu body JSON (pengganti c.JSON gin).
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeErr mengirim {"error": "..."} seperti gin.H{"error": ...} dulu.
func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// writeText mengirim teks polos.
func writeText(w http.ResponseWriter, status int, text string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(text))
}

// writeData mengirim byte dengan content-type tertentu.
func writeData(w http.ResponseWriter, status int, contentType string, data []byte) {
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(status)
	_, _ = w.Write(data)
}

// hubLogger menjembatani em.Logger (baris pipeline scrape) ke logbus.
type hubLogger struct{ hub *logbus.Hub }

func (l hubLogger) Log(id, level, msg string) {
	l.hub.Emit(logbus.Event{ID: id, Level: level, Text: msg})
}

// apiLogs menyiarkan log realtime ke browser lewat websocket: riwayat terakhir
// dulu, lalu baris baru. Dipakai panel log di depan player.
func (s *server) apiLogs(w http.ResponseWriter, r *http.Request) {
	if s.logs == nil {
		writeErr(w, http.StatusServiceUnavailable, "log tidak aktif")
		return
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return // Upgrade sudah menulis respons HTTP.
	}
	defer func() { _ = conn.Close() }()

	ch, cancel := s.logs.Subscribe()
	defer cancel()
	// Pembaca: mendeteksi klien putus (close/ping) supaya langganan dilepas.
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				cancel()
				return
			}
		}
	}()
	for ev := range ch {
		if err := conn.WriteJSON(ev); err != nil {
			return
		}
	}
}

// SPA fallback: path GET yang tak dikenal dapat index.html (React Router
// yang menampilkan 404), kecuali path API/fetch yang salah tulis —
// itu tetap JSON 404 supaya error di sisi client mudah dibedakan.
func (s *server) spaFallback(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	if strings.Contains(p, "/api/") || p == "/healthz" || strings.HasPrefix(p, "/s/") {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	s.index(w, r)
}

// File statis hasil build Vite (aset di /assets/*, favicon).
func (s *server) webFile(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/")
	if name == "" || name == "index.html" {
		s.index(w, r)
		return
	}
	data, err := fs.ReadFile(s.web, name)
	if err != nil {
		writeText(w, http.StatusNotFound, "tidak ada")
		return
	}
	switch {
	case strings.HasSuffix(name, ".js"):
		writeData(w, http.StatusOK, "text/javascript; charset=utf-8", data)
	case strings.HasSuffix(name, ".css"):
		writeData(w, http.StatusOK, "text/css; charset=utf-8", data)
	case strings.HasSuffix(name, ".svg"):
		writeData(w, http.StatusOK, "image/svg+xml", data)
	default:
		writeData(w, http.StatusOK, "application/octet-stream", data)
	}
}

func (s *server) index(w http.ResponseWriter, r *http.Request) {
	// Not FileFromFS: http.FileServer redirects *index.html to ".", which
	// loops forever on "/". Serve the bytes directly instead.
	data, err := fs.ReadFile(s.web, "index.html")
	if err != nil {
		writeText(w, http.StatusInternalServerError, "index.html tidak ditemukan")
		return
	}
	writeData(w, http.StatusOK, "text/html; charset=utf-8", data)
}

// apiSearch: pencarian judul. Backend yang menghubungi sumber catalog; hasil
// membawa id TMDB (moviedb_id) yang dibutuhkan embedmaster.
func (s *server) apiSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	tab := r.URL.Query().Get("tab")
	if q == "" || (tab != "movie" && tab != "tv") {
		writeErr(w, http.StatusBadRequest, "query harus ?q=<judul>&tab=movie|tv")
		return
	}
	res, err := s.tmdb.Search(r.Context(), tab, q)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": res})
}

// apiMeta: detail satu judul (movie: metadata; tv: metadata + daftar episode).
func (s *server) apiMeta(w http.ResponseWriter, r *http.Request) {
	tab, id := chi.URLParam(r, "tab"), chi.URLParam(r, "id")
	if (tab != "movie" && tab != "tv") || !strings.HasPrefix(id, "tt") {
		writeErr(w, http.StatusBadRequest, "meta harus /api/meta/movie|tv/<imdb-id>")
		return
	}
	res, err := s.tmdb.Meta(r.Context(), tab, id)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// apiStreams: daftar stream hasil scraping embedmaster untuk satu TMDB id
// (movie) atau satu episode tv. Fetch/dekripsi terjadi di Go (internal/em).
func (s *server) apiStreams(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	tab := query.Get("tab")
	id := strings.TrimSpace(query.Get("id"))
	if (tab != "movie" && tab != "tv") || id == "" {
		writeErr(w, http.StatusBadRequest, "streams harus ?tab=movie|tv&id=<tmdb-id>")
		return
	}
	var tv *em.TVEpisode
	if tab == "tv" {
		ses, err1 := strconv.Atoi(query.Get("s"))
		epi, err2 := strconv.Atoi(query.Get("e"))
		if err1 != nil || err2 != nil || ses < 0 || epi < 1 {
			writeErr(w, http.StatusBadRequest, "tv butuh ?s=<season>&e=<episode>")
			return
		}
		tv = &em.TVEpisode{Season: ses, Episode: epi}
	}
	res, err := s.em.Streams(r.Context(), tab, id, tv)
	if err != nil {
		s.fail(w, err)
		return
	}
	// Semua URL didaftarkan ke pool slug; player/subtitle hanya melihat
	// /s/<slug> pendek, tidak ada URL ter-encode panjang di network tab.
	// Ekstensi slug mengikuti jenis hasil verifikasi (hls→.m3u8) supaya player
	// memilih jalur adaptif walau URL stream tanpa ekstensi.
	for i := range res {
		srv := &res[i]
		hint := strings.ToLower(srv.Name)
		kind := resolve.ExtForKind(srv.Type)
		if srv.URL != "" {
			srv.URL = "/s/" + s.res.RegisterKind(srv.URL, hint, kind)
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
	playable := 0
	for i := range res {
		if res[i].URL != "" {
			playable++
		}
	}
	s.logs.OKf("Daftar stream siap · %d dari %d server bisa diputar", playable, len(res))
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "tab": tab, "servers": res})
}

// apiSubtitles: cari subtitle subdl untuk judul yang sedang dibuka. Nama+tahun
// diambil dari meta supaya autocomplete subdl dapat query yang tepat; tv ikut
// memilih musim + episode. Tiap permintaan fresh (tanpa cache): tautan unduh
// yang dikembalikan berasal dari halaman yang baru diambil.
func (s *server) apiSubtitles(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	tab, id := query.Get("tab"), strings.TrimSpace(query.Get("id"))
	if (tab != "movie" && tab != "tv") || !strings.HasPrefix(id, "tt") {
		writeErr(w, http.StatusBadRequest, "subtitles harus ?tab=movie|tv&id=<imdb-id>")
		return
	}
	season, episode := 0, 0
	if tab == "tv" {
		ses, err1 := strconv.Atoi(query.Get("s"))
		epi, err2 := strconv.Atoi(query.Get("e"))
		if err1 != nil || err2 != nil || ses < 0 || epi < 1 {
			writeErr(w, http.StatusBadRequest, "tv butuh ?s=<season>&e=<episode>")
			return
		}
		season, episode = ses, epi
	}
	if s.sub == nil {
		writeErr(w, http.StatusServiceUnavailable, "pencarian subtitle tidak aktif")
		return
	}
	meta, err := s.tmdb.Meta(r.Context(), tab, id)
	if err != nil {
		s.fail(w, err)
		return
	}
	search := s.sub.Search
	if query.Get("refresh") != "" {
		search = s.sub.SearchFresh
	}
	subs, err := search(r.Context(), tab, meta.Name, meta.Year, season, episode)
	if err != nil {
		s.logs.Errf("Subtitle gagal dimuat · %v", err)
		s.fail(w, err)
		return
	}
	// Zip didaftarkan ke pool slug: endpoint /api/subtitles/file hanya bisa
	// mengambil URL yang memang didaftarkan di sini (bukan sembarang alamat).
	// Baris tanpa zip dibuang — slug dengan URL kosong hanya menghasilkan 404/502
	// saat tombol "pakai" ditekan.
	out := subs[:0]
	for _, sub := range subs {
		if sub.Zip == "" {
			continue
		}
		sub.URL = "/api/subtitles/file?u=" + s.res.RegisterKind(sub.Zip, "sub-"+sub.Lang, ".zip")
		out = append(out, sub)
	}
	subs = out
	s.logs.OKf("Subtitle siap · %d pilihan", len(subs))
	writeJSON(w, http.StatusOK, map[string]any{"subtitles": subs})
}

// apiSubtitleFile mengubah zip subtitle subdl jadi VTT siap track: entri
// .srt/.vtt pertama, header WEBVTT, lalu timestamp dinormalkan.
func (s *server) apiSubtitleFile(w http.ResponseWriter, r *http.Request) {
	raw, ok := s.res.Load(r.URL.Query().Get("u"))
	if !ok {
		writeErr(w, http.StatusNotFound, "subtitle tidak ada")
		return
	}
	target, err := url.Parse(raw)
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") {
		writeErr(w, http.StatusBadRequest, "URL subtitle tidak valid")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	res, err := subHTTP.R().SetContext(ctx).SetDoNotParseResponse(true).
		SetHeader("User-Agent", "Mozilla/5.0 zanime").Get(raw)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "gagal menghubungi sumber subtitle")
		return
	}
	body := res.RawBody()
	defer func() { _ = body.Close() }()
	if res.StatusCode() != http.StatusOK {
		writeErr(w, http.StatusBadGateway, fmt.Sprintf("sumber subtitle HTTP %s", res.Status()))
		return
	}
	data, err := io.ReadAll(io.LimitReader(body, 20<<20))
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	text, err := firstSubFile(data)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	out := srtToVTT(text)
	s.logs.OKf("Subtitle subdl siap · %d cue", strings.Count(out, "-->"))
	writeData(w, http.StatusOK, "text/vtt; charset=utf-8", []byte(out))
}

// firstSubFile mengambil berkas .srt/.vtt pertama dari zip subtitle.
func firstSubFile(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("zip subtitle rusak: %w", err)
	}
	for _, f := range zr.File {
		name := strings.ToLower(f.Name)
		if !strings.HasSuffix(name, ".srt") && !strings.HasSuffix(name, ".vtt") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			continue
		}
		body, err := io.ReadAll(io.LimitReader(rc, 8<<20))
		_ = rc.Close()
		if err != nil {
			continue
		}
		return string(body), nil
	}
	return "", fmt.Errorf("zip subtitle tanpa .srt/.vtt")
}

// srtToVTT memberi header WEBVTT dan memakai titik pada baris waktu (SRT
// memakai koma). Timestamp tidak digeser/dinormalkan.
func srtToVTT(text string) string {
	body := strings.TrimSpace(strings.TrimPrefix(text, "\uFEFF"))
	body = strings.ReplaceAll(body, "\r\n", "\n")
	if !strings.HasPrefix(body, "WEBVTT") {
		body = "WEBVTT\n\n" + body
	}
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		if strings.Contains(line, "-->") {
			lines[i] = strings.ReplaceAll(line, ",", ".")
		}
	}
	return strings.Join(lines, "\n")
}

func (s *server) fail(w http.ResponseWriter, err error) {
	status := http.StatusBadGateway
	if errors.Is(err, context.DeadlineExceeded) {
		status = http.StatusGatewayTimeout
	}
	writeErr(w, status, err.Error())
}

// streamProxy menyalurkan satu URL media terdaftar lewat RAM (tanpa disk),
// tetap meneruskan Range supaya player bisa seek. Playlist teks (m3u8)
// ditulis ulang: setiap URI (playlist turun / segmen / URI= di tag)
// didaftarkan jadi slug /s/<slug>, karena player tidak bisa meminta
// playlist turunan langsung ke host video tanpa Referer.
func (s *server) streamProxy(w http.ResponseWriter, r *http.Request) {
	raw, ok := s.res.Load(chi.URLParam(r, "slug"))
	if !ok {
		writeErr(w, http.StatusNotFound, "slug tidak ada")
		return
	}
	target, err := url.Parse(raw)
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") {
		writeErr(w, http.StatusNotFound, "slug tidak ada")
		return
	}
	px := "px:" + chi.URLParam(r, "slug")

	// Host video embedmaster menolak request tanpa Referer halaman player
	// mereka; Referer ini juga membuka Access-Control-Allow-Origin.
	req := proxyClient.R().SetContext(r.Context()).SetDoNotParseResponse(true).
		SetHeader("User-Agent", "Mozilla/5.0 zanime").
		SetHeader("Referer", "https://embdmstrplayer.com/")
	if rng := r.Header.Get("Range"); rng != "" {
		req.SetHeader("Range", rng)
	}

	res, err := req.Execute(r.Method, target.String())
	if err != nil {
		// Player membatalkan permintaan (mis. ganti server) — bukan kegagalan.
		if errors.Is(err, context.Canceled) {
			s.logs.Emit(logbus.Event{ID: px, Level: logbus.LevelInfo, Text: "Permintaan dibatalkan browser"})
			return
		}
		s.logs.Emit(logbus.Event{ID: px, Level: logbus.LevelErr, Text: fmt.Sprintf("Media gagal dihubungi · %v", err)})
		writeErr(w, http.StatusBadGateway, "gagal menghubungi upstream: "+err.Error())
		return
	}
	body := res.RawBody()
	defer func() { _ = body.Close() }()
	if res.StatusCode() < 200 || res.StatusCode() >= 400 {
		s.logs.Emit(logbus.Event{ID: px, Level: logbus.LevelErr, Text: fmt.Sprintf("Media ditolak server sumber · HTTP %s", res.Status())})
		writeErr(w, http.StatusBadGateway, fmt.Sprintf("upstream HTTP %s", res.Status()))
		return
	}

	if isPlaylist(res.Header().Get("Content-Type"), target.Path) {
		data, err := io.ReadAll(body)
		if err != nil {
			writeErr(w, http.StatusBadGateway, err.Error())
			return
		}
		renditions, segments, vod := playlistStats(string(data))
		s.logs.Emit(logbus.Event{ID: px, Level: logbus.LevelOK, Text: fmt.Sprintf(
			"Playlist HLS dibaca · %d rendition · %d segmen · %s", renditions, segments, vodLabel(vod))})
		// VOD (ada #EXT-X-ENDLIST) tidak berubah → biarkan browser meng-cache;
		// playlist live tetap no-store. Master playlist bisa 1+ MB, jadi
		// meng-cache menghindari fetch ulang besar tiap player membacanya.
		if vod {
			w.Header().Set("Cache-Control", "public, max-age=600")
		} else {
			w.Header().Set("Cache-Control", "no-store")
		}
		writeData(w, http.StatusOK, "application/vnd.apple.mpegurl", []byte(s.rewritePlaylist(target, string(data))))
		return
	}

	// Subtitle diteruskan apa adanya: timestamp dari sumber dipakai langsung,
	// tidak ada normalisasi/geser di server.
	if isSubtitle(res.Header().Get("Content-Type"), target.Path) {
		data, err := io.ReadAll(body)
		if err != nil {
			writeErr(w, http.StatusBadGateway, err.Error())
			return
		}
		s.logs.Emit(logbus.Event{ID: px, Level: logbus.LevelOK, Text: fmt.Sprintf(
			"Subtitle VTT siap · %d cue", strings.Count(string(data), "-->"))})
		writeData(w, http.StatusOK, "text/vtt; charset=utf-8", data)
		return
	}

	for _, h := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges"} {
		if v := res.Header().Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	s.logs.Emit(logbus.Event{ID: px, Level: logbus.LevelOK, Text: fmt.Sprintf(
		"Media terkirim · HTTP %s · %s", res.Status(), sizeLabel(res.Header().Get("Content-Length")))})
	w.WriteHeader(res.StatusCode())
	if r.Method != http.MethodHead {
		_, _ = io.Copy(w, body)
	}
}

// playlistStats menghitung rendition (master), segmen (#EXTINF), dan apakah
// playlist sudah tuntas (VOD) — untuk baris log.
func playlistStats(body string) (renditions, segments int, vod bool) {
	for _, line := range strings.Split(body, "\n") {
		switch {
		case strings.HasPrefix(line, "#EXT-X-STREAM-INF"):
			renditions++
		case strings.HasPrefix(line, "#EXTINF"):
			segments++
		case strings.Contains(line, "#EXT-X-ENDLIST"):
			vod = true
		}
	}
	return
}

func vodLabel(vod bool) string {
	if vod {
		return "VOD"
	}
	return "live"
}

// sizeLabel merapikan Content-Length untuk teks log.
func sizeLabel(n string) string {
	v, err := strconv.ParseInt(n, 10, 64)
	if err != nil || v <= 0 {
		return "ukuran tidak disebut"
	}
	if v >= 1<<20 {
		return fmt.Sprintf("%.1f MB", float64(v)/(1<<20))
	}
	return fmt.Sprintf("%.0f KB", float64(v)/1024)
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
