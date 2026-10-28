package main

import (
	"bytes"
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
	"zanime/internal/memstorage"
	"zanime/internal/remux"
	"zanime/internal/s3store"
	"zanime/internal/stream"
)

//go:embed web
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
	cacheMB := flag.Int64("cache-mb", config.Int("MAX_MEMORY_MB", 200), "in-memory piece cache limit in MiB")
	flag.Parse()

	if *cacheMB <= 0 {
		log.Fatal("cache-mb must be positive")
	}

	mem := memstorage.New(*cacheMB << 20)

	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = "" // zero disk: every piece lives in memstorage
	cfg.DefaultStorage = mem
	cl, err := torrent.NewClient(cfg)
	if err != nil {
		log.Fatalf("torrent client: %v", err)
	}
	defer cl.Close()

	store, err := s3store.FromEnv(context.Background())
	if err != nil {
		log.Fatalf("s3: %v", err)
	}
	if store == nil {
		log.Printf("S3 nonaktif: S3_BUCKET kosong, video diputar langsung dari torrent")
	} else {
		log.Printf("S3 aktif: bucket %s/%s, batas %d MiB", store.Bucket(), store.Prefix(), store.Limit()>>20)
	}

	webRoot, err := fs.Sub(webFiles, "web")
	if err != nil {
		log.Fatalf("embedded web files: %v", err)
	}

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("listen %s: %v", *addr, err)
	}

	srv := newServer(cl, mem, store, webRoot, ln.Addr().(*net.TCPAddr).Port)
	srv.s3Direct = store != nil && config.String("S3_DIRECT", "1") != "0"
	go srv.syncEvictions()
	go srv.s3Watchdog()
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
	r.GET("/healthz", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	r.GET("/info", srv.info)
	r.GET("/api/state", srv.apiState) // untuk test/curl; UI memakai /api/ws
	r.GET("/api/ws", srv.apiWS)
	r.POST("/api/magnet", srv.apiMagnet)
	r.POST("/api/cancel", srv.apiCancel)
	r.POST("/api/select", srv.apiSelect)
	r.POST("/api/cache", srv.apiCache)
	r.GET("/fetch", srv.fetchProxy)
	r.HEAD("/fetch", srv.fetchProxy)
	r.GET("/stream", srv.stream)
	r.HEAD("/stream", srv.stream)
	r.GET("/stream/:index/:name", srv.stream)
	r.HEAD("/stream/:index/:name", srv.stream)
	r.GET("/remux/:index/:name", srv.remuxStream)
	r.HEAD("/remux/:index/:name", srv.remuxStream)

	log.Printf("listening on %s (memory cache %d MiB, zero disk)", ln.Addr(), *cacheMB)
	if err := r.RunListener(ln); err != nil {
		log.Fatal(err)
	}
}

type server struct {
	cl       *torrent.Client
	mem      *memstorage.Client
	store    *s3store.Store // nil: play straight from the torrent
	s3Direct bool           // redirect player ke presigned URL, bukan proxy
	web      fs.FS
	port     int // HTTP port ffmpeg reads /stream from; 0 = not listening yet
	remux    *remux.Manager

	addMu sync.Mutex // serializes magnet loads

	s3Mu      sync.Mutex
	s3PutMu   sync.Mutex           // one upload at a time; magnet loads must not wait
	s3Jobs    map[string]*s3Upload // key -> upload, reused by every range request
	s3Clients atomic.Int32         // tab yang sedang terhubung lewat /api/ws
	s3Seen    atomic.Int64         // Unix detik aktivitas user terakhir

	mu         sync.RWMutex
	t          *torrent.Torrent
	files      []*torrent.File // video files only, torrent order
	selected   int
	loading    bool
	loadCancel chan struct{} // ditutup oleh POST /api/cancel

	served atomic.Int64 // byte yang sudah dikirim ke klien (stream/remux/fetch)
}

func newServer(cl *torrent.Client, mem *memstorage.Client, store *s3store.Store, web fs.FS, port int) *server {
	return &server{cl: cl, mem: mem, store: store, web: web, port: port, remux: remux.NewManager(mem), selected: -1,
		s3Jobs: make(map[string]*s3Upload)}
}

// startRemux muxes the selected file in the background: ffmpeg reads our own
// /stream endpoint (so it can seek), copies every track without transcoding,
// and the result is buffered in RAM. A job for the same index is kept.
func (s *server) startRemux(index int) {
	if s.port == 0 {
		return // server belum listen
	}
	if s.store != nil {
		return // S3 sudah melayani Range; remux hanya menggandakan trafik
	}
	f := s.fileAt(index)
	if f == nil {
		return
	}
	target := fmt.Sprintf("http://127.0.0.1:%d/stream/%d/%s",
		s.port, index, url.PathEscape(path.Base(f.Path())))
	go func() {
		ctx := context.Background()
		ffmpeg, err := remux.Ensure(ctx)
		if err != nil {
			log.Printf("remux: %v", err)
			return
		}
		s.remux.Start(ctx, index, target, ffmpeg)
	}()
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

	if old != nil && old != t {
		go old.Drop()
		s.remux.Stop() // output lama milik torrent lama
	}
	log.Printf("torrent siap: %s (%d file video)", t.Name(), len(files))
	s.startRemux(0)
	if s.store != nil {
		go s.probeCached(t.InfoHash().String(), files)
	}
	return nil
}

// probeCached marks episodes that are already fully in S3, so after a restart
// the list shows "Siap" and picking one skips the upload.
func (s *server) probeCached(hash string, files []*torrent.File) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, f := range files {
		if ctx.Err() != nil {
			return
		}
		key := s.store.Key(hash, f.Path())
		if s.store.Has(ctx, key, f.Length()) {
			s.markReady(key, f.Length())
		}
	}
}

// markReady records an object that is already complete in S3.
func (s *server) markReady(key string, size int64) {
	s.s3Mu.Lock()
	defer s.s3Mu.Unlock()
	if s.s3Jobs[key] != nil {
		return
	}
	done := make(chan struct{})
	close(done)
	s.s3Jobs[key] = &s3Upload{key: key, size: size, started: time.Now(), done: done, cancel: func() {}}
}

// dropMarker forgets a finished job so a missing object is uploaded again.
func (s *server) dropMarker(key string) {
	s.s3Mu.Lock()
	defer s.s3Mu.Unlock()
	if job := s.s3Jobs[key]; job != nil {
		select {
		case <-job.done:
			delete(s.s3Jobs, key)
		default: // masih mengunggah: jangan dihapus
		}
	}
}

// syncEvictions marks pieces dropped from RAM as incomplete, so the torrent
// client fetches them again on a later seek. Only the active torrent is
// touched; evictions belonging to a dropped torrent are ignored.
func (s *server) syncEvictions() {
	for ev := range s.mem.Evicted() {
		s.mu.RLock()
		t := s.t
		s.mu.RUnlock()
		if t == nil || t.InfoHash() != ev.Hash {
			continue
		}
		if ev.Index >= 0 && ev.Index < t.NumPieces() {
			t.Piece(ev.Index).UpdateCompletion()
		}
	}
}

func (s *server) index(c *gin.Context) {
	// Not FileFromFS: http.FileServer redirects *index.html to "./", which
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
		"ready":           false,
		"storage":         "memory",
		"cache_limit":     s.mem.Limit(),
		"cache_used":      s.mem.Used(),
		"cache_evictions": s.mem.Evictions(),
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
	if s.store != nil && t != nil {
		hash := t.InfoHash().String()
		s.s3Mu.Lock()
		for i, f := range files {
			if job := s.s3Jobs[s.store.Key(hash, f.Path())]; job != nil {
				list[i].Cache = cacheStateOf(job)
			}
		}
		s.s3Mu.Unlock()
	}

	resp := gin.H{
		"ready":    len(files) > 0,
		"loading":  loading,
		"files":    list,
		"selected": selected,
		"cache": gin.H{
			"used":      s.mem.Used(),
			"limit":     s.mem.Limit(),
			"evictions": s.mem.Evictions(),
		},
	}
	// Byte kumulatif; UI menghitung Mbps dari selisih antar frame.
	fromPeers := int64(0)
	if t != nil {
		stats := t.Stats()
		resp["name"] = t.Name()
		resp["info_hash"] = t.InfoHash().String()
		resp["length"] = t.Length()
		// Kumulatif dari peer, bukan isi RAM: tetap naik walau piece di-evict.
		resp["downloaded"] = stats.BytesReadUsefulData.Int64()
		resp["peers"] = stats.ActivePeers
		fromPeers = stats.BytesReadUsefulData.Int64()
	}
	if f := fileAtIn(files, selected); f != nil {
		resp["size"] = f.Length()
	}
	resp["net"] = gin.H{"from_peers": fromPeers, "to_client": s.served.Load()}
	if s.store != nil {
		info := gin.H{
			"enabled": true,
			"direct":  s.s3Direct,
			"bucket":  s.store.Bucket(),
			"prefix":  s.store.Prefix(),
			"used":    s.store.Used(),
			"limit":   s.store.Limit(),
		}
		if t != nil {
			if f := fileAtIn(files, selected); f != nil {
				key := s.store.Key(t.InfoHash().String(), f.Path())
				s.s3Mu.Lock()
				if job := s.s3Jobs[key]; job != nil {
					select {
					case <-job.done:
						info["ready"] = job.err == nil
						if job.err != nil {
							info["error"] = job.err.Error()
						}
					default:
						info["uploading"] = true
						info["uploaded"] = job.read.Load()
						info["size"] = job.size
						if job.size > 0 {
							info["progress"] = float64(job.read.Load()) / float64(job.size) * 100
						}
					}
				}
				s.s3Mu.Unlock()
			}
		}
		resp["s3"] = info
	}
	if index, ready, exists := s.remux.Status(); exists {
		resp["remux"] = gin.H{"index": index, "ready": ready}
	}
	return resp
}

// apiWS pushes the state as JSON every 700 ms; the UI never polls.
func (s *server) apiWS(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()

	s.s3Clients.Add(1)
	s.s3Seen.Store(time.Now().Unix())
	defer func() {
		s.s3Clients.Add(-1)
		s.s3Seen.Store(time.Now().Unix())
	}()

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
		if err := conn.WriteJSON(s.state()); err != nil {
			return
		}
		<-ticker.C
	}
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(*http.Request) bool { return true },
}

// userCookie marks the browser so upload jobs can be owned and limited per
// user. It is HttpOnly: only the server needs it.
const userCookie = "zanime_uid"

// maxJobsPerUser bounds how many cached episodes one browser may queue, so
// clicking through episodes cannot pile up workers.
const maxJobsPerUser = 2

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
		s.selectTorrentFile(*req.FileIndex, userOf(c))
	}
	s.prepareS3(s.currentIndex(), userOf(c))
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
	s.startRemux(req.Index)
	s.prepareS3(req.Index, userOf(c))

	c.JSON(http.StatusOK, s.state())
}

// apiCache copies one episode into S3 without changing what is playing, so
// the next episode is ready before it is picked.
func (s *server) apiCache(c *gin.Context) {
	var req struct {
		Index int `json:"index"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "body harus {\"index\": N}"})
		return
	}
	s.mu.RLock()
	t, f := s.t, fileAtIn(s.files, req.Index)
	s.mu.RUnlock()
	if t == nil || f == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "index file di luar jangkauan"})
		return
	}
	if s.store != nil {
		s.startS3Upload(s.store.Key(t.InfoHash().String(), f.Path()), f, userOf(c), true)
	}
	c.JSON(http.StatusOK, s.state())
}

// selectTorrentFile picks the video that Torrentio points at (its fileIdx is an
// index into the torrent's file list) and starts its remux.
func (s *server) selectTorrentFile(torrentIndex int, owner string) {
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
			s.startRemux(i)
			s.prepareS3(i, owner)
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

// s3Upload copies one torrent file into the bucket. Every range request for
// that file waits on the same job, so the video is fully in S3 before it plays.
type s3Upload struct {
	key     string
	owner   string
	started time.Time
	size    int64
	read    atomic.Int64 // bytes pulled from the torrent so far
	done    chan struct{}
	cancel  context.CancelFunc
	err     error // guarded by server.s3Mu
}

// s3IdleTimeout aborts uploads once no tab has been connected for this long.
const s3IdleTimeout = 16 * time.Minute

// s3PresignTTL covers one long playback; a fresh URL is created per request.
const s3PresignTTL = 12 * time.Hour

// prepareS3 starts the copy for a selected file, so the UI can show progress
// before the player asks for any bytes. The per-user cap drops that user's
// oldest upload instead of queueing every episode they click.
func (s *server) prepareS3(index int, owner string) {
	s.s3Seen.Store(time.Now().Unix())
	if s.store == nil {
		return
	}
	s.mu.RLock()
	t, f := s.t, fileAtIn(s.files, index)
	s.mu.RUnlock()
	if t == nil || f == nil {
		return
	}
	s.startS3Upload(s.store.Key(t.InfoHash().String(), f.Path()), f, owner, true)
}

// s3Watchdog cancels uploads when nobody has had the web UI open for a while,
// so a closed tab cannot leave a transfer running forever.
func (s *server) s3Watchdog() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		if s.s3Clients.Load() > 0 {
			s.s3Seen.Store(time.Now().Unix())
			continue
		}
		if time.Since(time.Unix(s.s3Seen.Load(), 0)) >= s3IdleTimeout {
			s.cancelAllUploads()
		}
	}
}

func (s *server) cancelAllUploads() {
	s.s3Mu.Lock()
	defer s.s3Mu.Unlock()
	for _, job := range s.s3Jobs {
		select {
		case <-job.done: // sudah selesai, tidak ada yang perlu dibatalkan
		default:
			job.cancel()
		}
	}
}

// capUploads cancels owner's oldest running upload when they already have the
// maximum. Caller holds s3Mu.
func (s *server) capUploads(owner string) {
	var oldest *s3Upload
	n := 0
	for _, job := range s.s3Jobs {
		select {
		case <-job.done:
			continue
		default:
		}
		if job.owner != owner {
			continue
		}
		n++
		if oldest == nil || job.started.Before(oldest.started) {
			oldest = job
		}
	}
	if n >= maxJobsPerUser && oldest != nil {
		oldest.cancel()
	}
}

// keepKeys protects the episode being uploaded and the one currently selected
// from the 4 GiB eviction.
func (s *server) keepKeys(uploading string) map[string]bool {
	keep := map[string]bool{uploading: true}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.store != nil && s.t != nil {
		if f := fileAtIn(s.files, s.selected); f != nil {
			keep[s.store.Key(s.t.InfoHash().String(), f.Path())] = true
		}
	}
	return keep
}

func (s *server) s3Job(key string) *s3Upload {
	s.s3Mu.Lock()
	defer s.s3Mu.Unlock()
	return s.s3Jobs[key]
}

// startS3Upload returns the existing job for key, or starts one. A failed job
// is retried only when retry is set (an explicit file selection), never from
// /stream, which would otherwise re-upload in the request path.
func (s *server) startS3Upload(key string, f *torrent.File, owner string, retry bool) *s3Upload {
	s.s3Seen.Store(time.Now().Unix()) // upload aktif = user masih ada
	keep := s.keepKeys(key)
	s.s3Mu.Lock()
	defer s.s3Mu.Unlock()
	if job := s.s3Jobs[key]; job != nil {
		select {
		case <-job.done:
			if job.err == nil || !retry {
				return job
			}
		default:
			return job
		}
	}
	s.capUploads(owner)

	ctx, cancel := context.WithCancel(context.Background())
	job := &s3Upload{key: key, owner: owner, started: time.Now(), size: f.Length(),
		done: make(chan struct{}), cancel: cancel}
	s.s3Jobs[key] = job
	go func() {
		s.s3PutMu.Lock() // one upload at a time keeps the budget check exact
		defer s.s3PutMu.Unlock()

		var evicted []string
		var err error
		if ctx.Err() != nil {
			err = ctx.Err()
		} else if !s.store.Has(ctx, key, f.Length()) { // replay after restart: skip re-upload
			reader := f.NewReader()
			defer func() { _ = reader.Close() }()
			reader.SetReadahead(4 << 20)
			evicted, err = s.store.Put(ctx, key, f.Length(),
				progressReader{r: reader, n: &job.read}, keep)
		}

		s.s3Mu.Lock()
		for _, old := range evicted {
			delete(s.s3Jobs, old) // objeknya sudah dihapus, marker siap ikut hilang
		}
		job.err = err
		s.s3Mu.Unlock()
		cancel()
		close(job.done)
	}()
	return job
}

type progressReader struct {
	r io.Reader
	n *atomic.Int64
}

func (p progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.n.Add(int64(n))
	return n, err
}

// serveS3 proxies the object with Range support, so the player stays
// same-origin and can seek without a presigned URL. It reports false (before
// writing anything) when the object cannot be read, e.g. an evicted episode,
// so the caller can fall back to the torrent.
func (s *server) serveS3(c *gin.Context, key string) bool {
	res, err := s.store.Get(c.Request.Context(), key, c.GetHeader("Range"))
	if err != nil {
		log.Printf("s3 get %s: %v (fallback ke torrent)", key, err)
		s.dropMarker(key)
		return false
	}
	defer func() { _ = res.Body.Close() }()

	if res.ContentType != nil {
		c.Header("Content-Type", *res.ContentType)
	}
	if res.ContentLength != nil {
		c.Header("Content-Length", strconv.FormatInt(*res.ContentLength, 10))
	}
	if res.ContentRange != nil {
		c.Header("Content-Range", *res.ContentRange)
	}
	c.Header("Accept-Ranges", "bytes")
	status := http.StatusOK
	if res.ContentRange != nil {
		status = http.StatusPartialContent
	}
	s.countWriter(c)
	c.Status(status)
	if c.Request.Method != http.MethodHead {
		_, _ = io.Copy(c.Writer, res.Body)
	}
	return true
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

	if s.store != nil {
		s.mu.RLock()
		t := s.t
		s.mu.RUnlock()
		if t != nil {
			key := s.store.Key(t.InfoHash().String(), f.Path())
			job := s.s3Job(key)
			if job == nil {
				job = s.startS3Upload(key, f, userOf(c), false)
			}
			select {
			case <-job.done:
				s.s3Mu.Lock()
				err := job.err
				s.s3Mu.Unlock()
				if err == nil {
					if s.s3Direct {
						if target, perr := s.store.Presign(c.Request.Context(), key, s3PresignTTL); perr == nil {
							c.Header("Cache-Control", "no-store")
							c.Redirect(http.StatusFound, target)
							return
						}
					}
					if s.serveS3(c, key) {
						return
					}
				} else {
					log.Printf("s3 %s: %v (fallback ke torrent)", key, err)
				}
			case <-c.Request.Context().Done():
				return
			}
		}
	}

	reader := f.NewReader()
	defer func() { _ = reader.Close() }()
	reader.SetReadahead(8 << 20)
	s.countWriter(c)
	stream.Serve(c, path.Base(f.Path()), reader)
}

// remuxStream serves the muxed output from RAM with the same Range support as
// /stream, so the player can seek in the indexed copy.
func (s *server) remuxStream(c *gin.Context) {
	index, err := strconv.Atoi(c.Param("index"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "index tidak valid"})
		return
	}
	data, ready, exists := s.remux.Output(index)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "remux tidak tersedia untuk file ini"})
		return
	}
	if !ready {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "remux masih berjalan"})
		return
	}
	s.countWriter(c)
	stream.Serve(c, c.Param("name"), bytes.NewReader(data))
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
	Index int         `json:"index"`
	Name  string      `json:"name"`
	Path  string      `json:"path"`
	Size  int64       `json:"size"`
	URL   string      `json:"url"`
	Cache *cacheState `json:"cache,omitempty"`
}

// cacheState is the S3 copy status of one episode.
type cacheState struct {
	State    string  `json:"state"` // uploading | ready | error
	Progress float64 `json:"progress,omitempty"`
	Uploaded int64   `json:"uploaded,omitempty"`
}

func cacheStateOf(job *s3Upload) *cacheState {
	select {
	case <-job.done:
		if job.err != nil {
			return &cacheState{State: "error"}
		}
		return &cacheState{State: "ready"}
	default:
		st := &cacheState{State: "uploading", Uploaded: job.read.Load()}
		if job.size > 0 {
			st.Progress = float64(st.Uploaded) / float64(job.size) * 100
		}
		return st
	}
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
