package main

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"path"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"zanime/internal/config"
	"zanime/internal/diskcache"
	"zanime/internal/hls"
	"zanime/internal/media"
	"zanime/internal/stream"
)

//go:embed web/dist
var webFiles embed.FS

const defaultMagnet = "magnet:?xt=urn:btih:3c5356a2834c80305e18479ad39d16a4f8b8682b&dn=%5BErai-raws%5D%20Gabriel%20DropOut%20-%2001%20~%2012%20%5B1080p%5D%5BMultiple%20Subtitle%5D&tr=http%3A%2F%2Fnyaa.tracker.wf%3A7777%2Fannounce&tr=udp%3A%2F%2Fopen.stealth.si%3A80%2Fannounce&tr=udp%3A%2F%2Ftracker.opentrackr.org%3A1337%2Fannounce&tr=udp%3A%2F%2Fexodus.desync.com%3A6969%2Fannounce&tr=udp%3A%2F%2Ftracker.torrent.eu.org%3A451%2Fannounce"

const metadataTimeout = 45 * time.Second

var (
	errMetadataTimeout = errors.New("metadata timeout")
	errLoadCanceled    = errors.New("pemuatan magnet dibatalkan")
)

// proxyClient streams remote media (direct MP4 addon) through RAM. No overall
// timeout: playback lasts as long as the player keeps reading.
var proxyClient = &http.Client{
	Transport: &http.Transport{
		DialContext:         (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
		TLSHandshakeTimeout: 10 * time.Second,
	},
}

func main() {
	if err := config.LoadDotEnv(".env"); err != nil {
		log.Printf(".env: %v", err)
	}

	magnet := flag.String("magnet", defaultMagnet, "torrent magnet URI loaded at startup (empty to start idle)")
	addr := flag.String("addr", "0.0.0.0:"+config.String("PORT", "8080"), "HTTP listen address")
	cacheDir := flag.String("cache-dir", config.String("CACHE_DIR", "./tmp/cache"), "folder cache piece torrent")
	cacheMB := flag.Int64("cache-mb", config.Int("MAX_CACHE_MB", 4096), "disk piece cache limit in MiB")
	flag.Parse()

	if *cacheMB <= 0 {
		log.Fatal("cache-mb must be positive")
	}

	cache, err := diskcache.New(*cacheDir, *cacheMB<<20)
	if err != nil {
		log.Fatalf("cache dir %s: %v", *cacheDir, err)
	}

	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = *cacheDir
	cfg.DefaultStorage = cache
	// Client-only: jangan buka port di router (produksi hanya membuka port web)
	// dan jangan mengunggah balik setelah file selesai.
	cfg.NoDefaultPortForwarding = true
	cfg.Seed = false
	cl, err := torrent.NewClient(cfg)
	if err != nil {
		log.Fatalf("torrent client: %v", err)
	}
	defer cl.Close()

	webRoot, err := fs.Sub(webFiles, "web/dist")
	if err != nil {
		log.Fatalf("embedded web files: %v", err)
	}

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("listen %s: %v", *addr, err)
	}

	srv := newServer(cl, cache, webRoot, ln.Addr().(*net.TCPAddr).Port)
	go func() {
		for range time.Tick(1 * time.Second) {
			srv.pruneCache()
		}
	}()
	if *magnet != "" {
		go func() {
			if err := srv.loadMagnet(*magnet); err != nil {
				log.Printf("magnet awal: %v", err)
			}
		}()
	}

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(srv.withUser)
	r.GET("/", srv.index)
	r.GET("/assets/*filepath", srv.webFile) // aset Vite (JS/CSS)
	r.GET("/favicon.ico", srv.webFile)
	// SPA fallback: React Router punya route `/` dan `*` (404), jadi path
	// lain harus tetap mendapat index.html biar page di-sisi browser;
	// endpoint API/HLS yang salah tulis tetap JSON 404.
	r.NoRoute(srv.spaFallback)
	r.GET("/healthz", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	r.GET("/info", srv.info)
	r.GET("/api/state", srv.apiState) // untuk test/curl; UI memakai /api/ws
	r.GET("/api/ws", srv.apiWS)
	r.POST("/api/magnet", srv.apiMagnet)
	r.POST("/api/cancel", srv.apiCancel)
	r.POST("/api/select", srv.apiSelect)
	r.GET("/api/addon", srv.addonProxy) // katalog/meta/streams: browser tidak menghubungi addon langsung
	r.GET("/fetch", srv.fetchProxy)
	r.HEAD("/fetch", srv.fetchProxy)
	r.GET("/stream", srv.stream)
	r.HEAD("/stream", srv.stream)
	r.GET("/stream/:index/:name", srv.stream)
	r.HEAD("/stream/:index/:name", srv.stream)
	r.GET("/hls/:index/*name", srv.hlsFile)
	r.HEAD("/hls/:index/*name", srv.hlsFile)
	r.GET("/cover/:index", srv.coverArt)
	r.HEAD("/cover/:index", srv.coverArt)

	log.Printf("listening on %s (disk cache %s, limit %d MiB)", ln.Addr(), *cacheDir, *cacheMB)
	if err := r.RunListener(ln); err != nil {
		log.Fatal(err)
	}
}

type server struct {
	cl      *torrent.Client
	cache   *diskcache.Client
	hls     *hls.Manager
	web     fs.FS
	port    int // HTTP port mkvgo reads /stream from; 0 = not listening yet
	started time.Time

	addMu   sync.Mutex // serializes magnet loads
	wsMu    sync.Mutex
	wsUsers map[string]int // user cookie -> jumlah tab terbuka

	stateMu   sync.Mutex
	stateSnap gin.H
	stateAt   time.Time

	mu         sync.RWMutex
	t          *torrent.Torrent
	files      []*torrent.File // video files only, torrent order
	selected   int
	loading    bool
	loadCancel chan struct{} // ditutup oleh POST /api/cancel

	mediaMu          sync.RWMutex
	mediaInfo        media.Info
	mediaIndex       int
	mediaTorrent     *torrent.Torrent
	mediaReady       bool
	mediaScanning    bool
	mediaScanIndex   int
	mediaScanTorrent *torrent.Torrent

	served atomic.Int64 // byte yang sudah dikirim ke klien (stream/hls/fetch)
}

func newServer(cl *torrent.Client, cache *diskcache.Client, web fs.FS, port int) *server {
	srv := &server{cl: cl, cache: cache, hls: hls.NewManager(), web: web, port: port, selected: -1,
		started: time.Now(), wsUsers: make(map[string]int)}
	cache.SetPieceRefresh(srv.refreshPieces)
	return srv
}

// refreshPieces tells the live client that punched-out pieces are missing.
// diskcache calls it immediately after the hole punch, before the next prune
// interval, so the client re-downloads a piece instead of trusting the hole.
func (s *server) refreshPieces(dir string, pieces []int) {
	s.mu.RLock()
	t := s.t
	s.mu.RUnlock()
	if t == nil || t.InfoHash().String() != dir {
		return
	}
	for _, pi := range pieces {
		t.Piece(pi).UpdateCompletion()
	}
}

// scanMedia reads title/tags/chapters/track languages/cover from the selected
// file. It runs beside HLS planning (both read the same cached pieces) and only
// publishes when that file is still selected, so a slow scan can never label the
// next episode with the previous one's metadata.
func (s *server) scanMedia(index int) {
	s.mu.RLock()
	t, f := s.t, (*torrent.File)(nil)
	if index >= 0 && index < len(s.files) {
		f = s.files[index]
	}
	s.mu.RUnlock()
	if t == nil || f == nil {
		return
	}

	s.mediaMu.Lock()
	if (s.mediaReady && s.mediaTorrent == t && s.mediaIndex == index) ||
		(s.mediaScanning && s.mediaScanTorrent == t && s.mediaScanIndex == index) {
		s.mediaMu.Unlock()
		return
	}
	s.mediaScanning, s.mediaScanIndex, s.mediaScanTorrent = true, index, t
	s.mediaMu.Unlock()

	info := media.Info{}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	reader := f.NewReader()
	reader.SetContext(ctx)
	defer func() { _ = reader.Close() }()
	reader.SetReadahead(2 << 20)
	got, err := media.Read(ctx, reader, path.Base(f.Path()))
	if err == nil {
		info = got
	} else {
		log.Printf("media index %d: %v", index, err)
	}

	s.mediaMu.Lock()
	s.mediaScanning = false
	s.mu.RLock()
	current := s.t == t && s.selected == index
	s.mu.RUnlock()
	if current {
		s.mediaInfo, s.mediaIndex, s.mediaTorrent, s.mediaReady = info, index, t, true
	}
	s.mediaMu.Unlock()
}

// startHLS prepares the selected file: mkvgo plans the HLS presentation over our
// own /stream endpoint (HTTP Range, so it can seek) and packages each segment
// when the player asks for it — no ffmpeg, no re-encode, nothing on disk. A
// session for the same index is kept.
func (s *server) startHLS(index int) {
	if s.port == 0 {
		return // server belum listen
	}
	f := s.fileAt(index)
	if f == nil {
		return
	}
	target := fmt.Sprintf("http://127.0.0.1:%d/stream/%d/%s",
		s.port, index, url.PathEscape(path.Base(f.Path())))
	s.hls.Start(context.Background(), index, target)
	// Ganti episode tidak menyentuh cache: piece tetangga dibiarkan di disk,
	// supaya kembali ke episode sebelumnya tidak mengunduh ulang. Ukuran tetap
	// dijaga Prune (LRU per torrent lalu per piece di torrent aktif).
	go s.scanMedia(index)
}

// loadMagnet replaces the active torrent and waits for its metadata. It is
// called both at startup and from POST /api/magnet.
func (s *server) loadMagnet(magnet string) error {
	magnet = strings.TrimSpace(magnet)
	if magnet == "" {
		return errors.New("magnet kosong")
	}
	spec, err := torrent.TorrentSpecFromMagnetUri(magnet)
	if err != nil {
		return fmt.Errorf("magnet tidak valid: %w", err)
	}

	s.addMu.Lock()
	defer s.addMu.Unlock()

	cancel := make(chan struct{})
	s.mu.Lock()
	s.loading = true
	s.loadCancel = cancel
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.loading = false
		if s.loadCancel == cancel {
			s.loadCancel = nil
		}
		s.mu.Unlock()
	}()

	t, _, err := s.cl.AddTorrentSpec(spec)
	if err != nil {
		return fmt.Errorf("tambah torrent: %w", err)
	}

	select {
	case <-t.GotInfo():
	case <-cancel:
		go t.Drop()
		return errLoadCanceled
	case <-time.After(metadataTimeout):
		go t.Drop()
		return fmt.Errorf("%w setelah %s", errMetadataTimeout, metadataTimeout)
	}

	files := videoFiles(t.Files())
	if len(files) == 0 {
		go t.Drop()
		return errors.New("torrent tidak berisi file video")
	}

	s.mu.Lock()
	old := s.t
	s.t = t
	s.files = files
	s.selected = 0
	s.mu.Unlock()

	s.mediaMu.Lock()
	s.mediaInfo, s.mediaIndex, s.mediaTorrent, s.mediaReady = media.Info{}, -1, nil, false
	s.mediaScanning, s.mediaScanIndex, s.mediaScanTorrent = false, -1, nil
	s.mediaMu.Unlock()

	s.cache.SetActive(t.InfoHash())
	if old != nil && old != t {
		// Drop the old torrent's client state, but leave its cached bytes on
		// disk: pruneCache evicts them gradually only once the budget is full.
		go old.Drop()
		s.hls.Stop() // segmen lama milik torrent lama
	}
	log.Printf("torrent siap: %s (%d file video)", t.Name(), len(files))
	s.startHLS(0)
	return nil
}

// pruneCache runs every second and enforces the disk budget, and only when it is
// exceeded: the oldest cached pieces are deleted, least recently used torrent
// first, just enough to bring the cache back under the limit. Because it runs
// once a second, what it frees tracks what the download added since the last
// run - add two pieces, drop two - rather than wiping a torrent at once. A
// torrent directory goes away only when its last piece does, and the active
// torrent's newest pieces stay so playback does not stall at the playhead.
func (s *server) pruneCache() {
	if removed := s.cache.Prune(); removed > 0 {
		log.Printf("cache disk penuh: %d MiB dibuang (piece tertua)", removed>>20)
	}
}

// SPA fallback: path GET yang tak dikenal dapat index.html (React Router
// yang menampilkan 404), kecuali path API/HLS/stream yang salah tulis —
// itu tetap JSON 404 supaya error di sisi client mudah dibedakan.
func (s *server) spaFallback(c *gin.Context) {
	p := c.Request.URL.Path
	if strings.HasPrefix(p, "/api/") || strings.HasPrefix(p, "/hls/") ||
		strings.HasPrefix(p, "/stream") || strings.HasPrefix(p, "/fetch") {
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

func (s *server) info(c *gin.Context) {
	resp := gin.H{
		"ready":        false,
		"storage":      "disk",
		"cache_dir":    s.cache.Root(),
		"cache_limit":  s.cache.Limit(),
		"cache_used":   s.cache.Used(),
		"cache_pruned": s.cache.Pruned(),
	}
	if f := s.fileAt(s.currentIndex()); f != nil {
		resp["ready"] = true
		resp["name"] = f.Path()
		resp["size"] = f.Length()
	}
	c.JSON(http.StatusOK, resp)
}

func (s *server) apiState(c *gin.Context) {
	c.JSON(http.StatusOK, s.state())
}

func (s *server) state() gin.H {
	s.mu.RLock()
	t, files, selected, loading := s.t, append([]*torrent.File(nil), s.files...), s.selected, s.loading
	s.mu.RUnlock()

	list := fileList(files)

	// Jumlah piece dihitung per torrent aktif, supaya sisa torrent lama (yang
	// masih bisa ada beberapa saat setelah diganti) tidak ikut terhitung.
	cacheUsed := s.cache.Used()
	if t != nil {
		cacheUsed = s.cache.UsedByTorrent(t.InfoHash())
	}
	resp := gin.H{
		"ready":    len(files) > 0,
		"loading":  loading,
		"files":    list,
		"selected": selected,
		"cache": gin.H{
			"used":      cacheUsed,
			"limit":     s.cache.Limit(),
			"evictions": s.cache.Pruned(),
		},
	}
	// Byte kumulatif; UI menghitung Mbps dari selisih antar frame.
	fromPeers := int64(0)
	if t != nil {
		stats := t.Stats()
		complete, partial, busy := pieceCounts(t)
		pieceLen := int64(0)
		if info := t.Info(); info != nil {
			pieceLen = info.PieceLength
		}
		resp["name"] = t.Name()
		resp["info_hash"] = t.InfoHash().String()
		resp["length"] = t.Length()
		// Kumulatif dari peer, bukan isi cache: tetap naik walau torrent dibuang.
		resp["downloaded"] = stats.BytesReadUsefulData.Int64()
		resp["peers"] = stats.ActivePeers
		fromPeers = stats.BytesReadUsefulData.Int64()
		resp["torrent"] = gin.H{
			"pieces":          t.NumPieces(),
			"piece_length":    pieceLen,
			"ram_bytes":       t.BytesCompleted(),
			"complete_pieces": complete,
			"partial_pieces":  partial,
			"busy_pieces":     busy,
			"total_peers":     stats.TotalPeers,
			"seeders":         stats.ConnectedSeeders,
			"half_open":       stats.HalfOpenPeers,
			"pending_peers":   stats.PendingPeers,
			"chunks_read":     stats.ChunksRead.Int64(),
			"chunks_useful":   stats.ChunksReadUseful.Int64(),
			"chunks_wasted":   stats.ChunksReadWasted.Int64(),
			"wire_in":         stats.BytesRead.Int64(),
			"data_in":         stats.BytesReadData.Int64(),
			"hashed":          stats.BytesHashed.Int64(),
			"wasted_bytes":    max(int64(0), stats.BytesReadData.Int64()-stats.BytesReadUsefulIntendedData.Int64()),
		}
	}
	if f := fileAtIn(files, selected); f != nil {
		resp["size"] = f.Length()
	}

	s.mediaMu.RLock()
	mi := s.mediaInfo
	mediaReady := s.mediaReady && s.mediaTorrent == t && s.mediaIndex == selected
	s.mediaMu.RUnlock()
	if mediaReady {
		info := gin.H{
			"title":       mi.Title,
			"description": mi.Description,
			"rating":      mi.Rating,
			"genre":       mi.Genre,
			"date":        mi.Date,
			"duration":    mi.Duration,
			"chapters":    mi.Chapters,
			"video":       mi.Video,
			"audio":       mi.Audio,
			"subs":        mi.Subs,
		}
		if len(mi.Cover) > 0 {
			info["poster"] = fmt.Sprintf("/cover/%d", selected)
		}
		resp["media"] = info
	}

	resp["net"] = gin.H{"from_peers": fromPeers, "to_client": s.served.Load()}
	resp["users"] = gin.H{"active": s.activeUsers(), "tabs": s.wsTabs()}
	var heap runtime.MemStats
	runtime.ReadMemStats(&heap)
	resp["server"] = gin.H{
		"uptime":     int64(time.Since(s.started).Seconds()),
		"goroutines": runtime.NumGoroutine(),
		"heap":       heap.HeapAlloc,
		"sys":        heap.Sys,
	}
	if st, exists := s.hls.Status(); exists {
		hlsInfo := gin.H{
			"index":    st.Index,
			"ready":    st.Ready,
			"live":     st.Live,
			"playlist": st.Playlist,
		}
		if st.Error != "" {
			hlsInfo["error"] = st.Error
		}
		if len(st.Subs) > 0 {
			subs := make([]gin.H, len(st.Subs))
			for i, sub := range st.Subs {
				subs[i] = gin.H{
					"file":   sub.File,
					"name":   sub.Name,
					"lang":   sub.Lang,
					"format": sub.Format,
					"url":    fmt.Sprintf("/hls/%d/%s", st.Index, sub.File),
				}
			}
			hlsInfo["subs"] = subs
		}
		resp["hls"] = hlsInfo
	}
	return resp
}

// addonHosts are the metadata addons the search dialog reads. The browser only
// calls this server, so the page stays same-origin and the addons never see
// per-client CORS preflights or rate limits.
var addonHosts = map[string]bool{
	"v3-cinemeta.strem.io":  true,
	"torrentio.strem.fun":   true,
	"cinema.debrid.in":      true,
	"muvibox.inulpc.web.id": true,
}

// addonMaxBody caps a proxied addon response; catalog JSON is well below this.
const addonMaxBody = 8 << 20

// addonProxy fetches one addon URL server-side and returns its JSON verbatim.
// The host allowlist keeps this from becoming an open proxy.
func (s *server) addonProxy(c *gin.Context) {
	target, err := url.Parse(c.Query("url"))
	if err != nil || (target.Scheme != "https" && target.Scheme != "http") || !addonHosts[target.Host] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "url addon tidak diizinkan"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "url addon tidak valid"})
		return
	}
	req.Header.Set("Accept", "application/json")
	// Sebagian addon di belakang CDN menolak User-Agent non-browser.
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36")
	res, err := proxyClient.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "addon tidak bisa dihubungi: " + err.Error()})
		return
	}
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(res.Body, addonMaxBody))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "baca balasan addon: " + err.Error()})
		return
	}
	if res.StatusCode != http.StatusOK {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("addon HTTP %s", res.Status)})
		return
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", body)
}

// cachedState shares one snapshot between WebSocket clients for 500 ms, so
// extra tabs do not multiply the per-frame work (torrent stats, piece runs,
// heap reads). Endpoints still call state() directly for a fresh answer.
func (s *server) cachedState() gin.H {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if s.stateSnap != nil && time.Since(s.stateAt) < 500*time.Millisecond {
		return s.stateSnap
	}
	s.stateSnap = s.state()
	s.stateAt = time.Now()
	return s.stateSnap
}

// pieceCounts summarizes the torrent's piece states in one pass over the
// state runs: complete, partially downloaded, and being hashed/marked.
func pieceCounts(t *torrent.Torrent) (complete, partial, busy int) {
	for _, run := range t.PieceStateRuns() {
		if run.Complete {
			complete += run.Length
			continue
		}
		if run.Partial {
			partial += run.Length
		}
		if run.Hashing || run.QueuedForHash || run.Marking {
			busy += run.Length
		}
	}
	return complete, partial, busy
}

// apiWS pushes the state as JSON every 700 ms; the UI never polls.
func (s *server) apiWS(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()

	user := userOf(c)
	s.addWSUser(user)
	defer s.removeWSUser(user)

	// Frame dari klien diabaikan; ReadMessage juga memproses ping/pong dan
	// memberi tahu saat klien pergi.
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				_ = conn.Close()
				return
			}
		}
	}()

	ticker := time.NewTicker(700 * time.Millisecond)
	defer ticker.Stop()
	for {
		_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if err := conn.WriteJSON(s.cachedState()); err != nil {
			return
		}
		<-ticker.C
	}
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(*http.Request) bool { return true },
}

// userCookie marks the browser so tabs of one user can be counted.
const userCookie = "zanime_uid"

func (s *server) withUser(c *gin.Context) {
	id, err := c.Cookie(userCookie)
	if err != nil || id == "" {
		id = newUserID()
		c.SetCookie(userCookie, id, 31536000, "/", "", false, true) // 1 year
	}
	c.Set(userCookie, id)
	c.Next()
}

func newUserID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(b[:])
}

func userOf(c *gin.Context) string {
	return c.GetString(userCookie)
}

// apiCancel stops the magnet load in flight; the active torrent is untouched.
func (s *server) apiCancel(c *gin.Context) {
	s.mu.Lock()
	cancel := s.loadCancel
	s.loadCancel = nil
	s.mu.Unlock()
	if cancel != nil {
		close(cancel)
	}
	c.JSON(http.StatusOK, s.state())
}

func (s *server) apiMagnet(c *gin.Context) {
	var req struct {
		Magnet    string `json:"magnet"`
		FileIndex *int   `json:"fileIndex"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "body harus {\"magnet\": \"...\"}"})
		return
	}
	if _, err := torrent.TorrentSpecFromMagnetUri(strings.TrimSpace(req.Magnet)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "magnet tidak valid"})
		return
	}

	if err := s.loadMagnet(req.Magnet); err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, errMetadataTimeout) {
			status = http.StatusGatewayTimeout
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	if req.FileIndex != nil {
		s.selectTorrentFile(*req.FileIndex)
	}
	c.JSON(http.StatusOK, s.state())
}

func (s *server) apiSelect(c *gin.Context) {
	var req struct {
		Index int `json:"index"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "body harus {\"index\": N}"})
		return
	}

	s.mu.Lock()
	if req.Index < 0 || req.Index >= len(s.files) {
		s.mu.Unlock()
		c.JSON(http.StatusBadRequest, gin.H{"error": "index file di luar jangkauan"})
		return
	}
	s.selected = req.Index
	s.mu.Unlock()
	s.startHLS(req.Index)

	c.JSON(http.StatusOK, s.state())
}

// selectTorrentFile picks the video that Torrentio points at (its fileIdx is an
// index into the torrent's file list) and starts its HLS session.
func (s *server) selectTorrentFile(torrentIndex int) {
	s.mu.RLock()
	t, files := s.t, s.files
	s.mu.RUnlock()
	if t == nil {
		return
	}
	all := t.Files()
	if torrentIndex < 0 || torrentIndex >= len(all) {
		return
	}
	// t.Files() mengembalikan slice yang sama setiap kali, jadi pointer (dan
	// path lengkap) bisa dipakai untuk mencocokkan walau ada basename kembar.
	want := all[torrentIndex]
	for i, f := range files {
		if f == want || f.Path() == want.Path() {
			s.mu.Lock()
			s.selected = i
			s.mu.Unlock()
			s.startHLS(i)
			return
		}
	}
}

// countWriter adds every byte sent to a client to the server total, so the UI
// can show the server → client speed.
func (s *server) countWriter(c *gin.Context) {
	c.Writer = countingWriter{ResponseWriter: c.Writer, served: &s.served}
}

type countingWriter struct {
	gin.ResponseWriter
	served *atomic.Int64
}

func (w countingWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.served.Add(int64(n))
	return n, err
}

// fetchProxy streams a direct media URL through RAM (no disk), keeping Range
// support so the player can seek.
func (s *server) fetchProxy(c *gin.Context) {
	target, err := url.Parse(c.Query("url"))
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "URL tidak valid"})
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
	req.Header.Set("User-Agent", "Mozilla/5.0 zanime")

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

	for _, h := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges"} {
		if v := res.Header.Get(h); v != "" {
			c.Header(h, v)
		}
	}
	if c.GetHeader("Content-Type") == "" {
		c.Header("Content-Type", "video/mp4")
	}
	s.countWriter(c)
	c.Status(res.StatusCode)
	if c.Request.Method != http.MethodHead {
		_, _ = io.Copy(c.Writer, res.Body)
	}
}

func (s *server) addWSUser(id string) {
	s.wsMu.Lock()
	defer s.wsMu.Unlock()
	s.wsUsers[id]++
}

func (s *server) removeWSUser(id string) {
	s.wsMu.Lock()
	defer s.wsMu.Unlock()
	if s.wsUsers[id]--; s.wsUsers[id] <= 0 {
		delete(s.wsUsers, id)
	}
}

func (s *server) activeUsers() int {
	s.wsMu.Lock()
	defer s.wsMu.Unlock()
	return len(s.wsUsers)
}

func (s *server) wsTabs() int {
	s.wsMu.Lock()
	defer s.wsMu.Unlock()
	tabs := 0
	for _, n := range s.wsUsers {
		tabs += n
	}
	return tabs
}

// coverArt serves the selected file's embedded MKV cover (cover.jpg/poster.png
// attachment), used as the player poster.
func (s *server) coverArt(c *gin.Context) {
	index, err := strconv.Atoi(c.Param("index"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "index tidak valid"})
		return
	}
	s.mediaMu.RLock()
	ok := s.mediaReady && s.mediaIndex == index && len(s.mediaInfo.Cover) > 0
	data, mime := s.mediaInfo.Cover, s.mediaInfo.CoverMIME
	s.mediaMu.RUnlock()
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "cover tidak tersedia"})
		return
	}
	c.Header("Cache-Control", "public, max-age=3600")
	c.Data(http.StatusOK, mime, data)
}

// hlsFile serves one playlist, segment, init or subtitle of the session for a
// file index. Everything is packaged on demand by mkvgo.
func (s *server) hlsFile(c *gin.Context) {
	index, err := strconv.Atoi(c.Param("index"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "index tidak valid"})
		return
	}
	name := strings.TrimPrefix(c.Param("name"), "/")
	switch {
	case name == "master.m3u8", strings.HasSuffix(name, ".m3u8"):
		data, ok := s.hls.Playlist(c.Request.Context(), index, name)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "sesi hls tidak tersedia untuk file ini"})
			return
		}
		c.Header("Cache-Control", "no-store")
		c.Data(http.StatusOK, "application/vnd.apple.mpegurl", data)
	case strings.HasSuffix(name, ".vtt"):
		data, err := s.hls.Subtitle(c.Request.Context(), index, name)
		if errors.Is(err, hls.ErrNoSuchFile) {
			c.JSON(http.StatusNotFound, gin.H{"error": "subtitle tidak tersedia"})
			return
		}
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		c.Header("Cache-Control", "no-store")
		s.countWriter(c)
		c.Data(http.StatusOK, "text/vtt", data)
	case strings.HasSuffix(name, ".m4s"), strings.HasSuffix(name, ".mp4"):
		data, contentType, err := s.hls.Segment(c.Request.Context(), index, name)
		if errors.Is(err, hls.ErrNoSuchFile) {
			c.JSON(http.StatusNotFound, gin.H{"error": "segmen tidak tersedia"})
			return
		}
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		c.Header("Cache-Control", "no-store")
		s.countWriter(c)
		c.Data(http.StatusOK, contentType, data)
	default:
		c.JSON(http.StatusNotFound, gin.H{"error": "sesi hls tidak tersedia untuk file ini"})
	}
}

func (s *server) stream(c *gin.Context) {
	index := s.currentIndex()
	if rawIndex := c.Param("index"); rawIndex != "" {
		parsed, err := strconv.Atoi(rawIndex)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "index tidak valid"})
			return
		}
		index = parsed
	}
	f := s.fileAt(index)
	if f == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "torrent metadata belum siap"})
		return
	}

	reader := f.NewReader()
	defer func() { _ = reader.Close() }()
	// Saat mkvgo menyusun plan, bacaannya adalah probe kecil di offset acak
	// (peek head/tail/Cues). Readahead penuh di sini menarik beberapa MiB per
	// probe: swarm dingin jadi mengunduh ratusan MiB untuk beberapa MiB yang
	// benar-benar dibaca, dan plan timeout sebelum durasi sempat terbit.
	// Playback sekuensial (plan sudah siap) tetap dapat readahead penuh.
	if !s.hls.Planning(index) {
		reader.SetReadahead(8 << 20)
	}
	s.countWriter(c)
	stream.Serve(c, path.Base(f.Path()), reader)
}

func (s *server) currentIndex() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.selected
}

func (s *server) fileAt(index int) *torrent.File {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return fileAtIn(s.files, index)
}

func fileAtIn(files []*torrent.File, index int) *torrent.File {
	if index < 0 || index >= len(files) {
		return nil
	}
	return files[index]
}

type fileResponse struct {
	Index int    `json:"index"`
	Name  string `json:"name"`
	Path  string `json:"path"`
	Size  int64  `json:"size"`
	URL   string `json:"url"`
}

func fileList(files []*torrent.File) []fileResponse {
	out := make([]fileResponse, len(files))
	for i, f := range files {
		name := path.Base(f.Path())
		out[i] = fileResponse{
			Index: i,
			Name:  name,
			Path:  f.Path(),
			Size:  f.Length(),
			URL:   "/stream/" + strconv.Itoa(i) + "/" + url.PathEscape(name),
		}
	}
	return out
}

var videoExts = map[string]bool{
	".mkv": true, ".mp4": true, ".m4v": true, ".webm": true,
	".avi": true, ".mov": true, ".ts": true, ".m2ts": true,
}

func videoFiles(all []*torrent.File) []*torrent.File {
	out := make([]*torrent.File, 0, len(all))
	for _, f := range all {
		if videoExts[strings.ToLower(path.Ext(f.Path()))] {
			out = append(out, f)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Path()) < strings.ToLower(out[j].Path())
	})
	return out
}
