package main

import (
	"bytes"
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

	webRoot, err := fs.Sub(webFiles, "web")
	if err != nil {
		log.Fatalf("embedded web files: %v", err)
	}

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("listen %s: %v", *addr, err)
	}

	srv := newServer(cl, mem, webRoot, ln.Addr().(*net.TCPAddr).Port)
	go srv.syncEvictions()
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
	r.GET("/", srv.index)
	r.GET("/healthz", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	r.GET("/info", srv.info)
	r.GET("/api/state", srv.apiState) // untuk test/curl; UI memakai /api/ws
	r.GET("/api/ws", srv.apiWS)
	r.POST("/api/magnet", srv.apiMagnet)
	r.POST("/api/cancel", srv.apiCancel)
	r.POST("/api/select", srv.apiSelect)
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
	cl    *torrent.Client
	mem   *memstorage.Client
	web   fs.FS
	port  int // HTTP port ffmpeg reads /stream from; 0 = not listening yet
	remux *remux.Manager

	addMu sync.Mutex // serializes magnet loads

	mu         sync.RWMutex
	t          *torrent.Torrent
	files      []*torrent.File // video files only, torrent order
	selected   int
	loading    bool
	loadCancel chan struct{} // ditutup oleh POST /api/cancel

	served atomic.Int64 // byte yang sudah dikirim ke klien (stream/remux/fetch)
}

func newServer(cl *torrent.Client, mem *memstorage.Client, web fs.FS, port int) *server {
	return &server{cl: cl, mem: mem, web: web, port: port, remux: remux.NewManager(mem), selected: -1}
}

// startRemux muxes the selected file in the background: ffmpeg reads our own
// /stream endpoint (so it can seek), copies every track without transcoding,
// and the result is buffered in RAM. A job for the same index is kept.
func (s *server) startRemux(index int) {
	if s.port == 0 {
		return // server belum listen
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
	return nil
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

	resp := gin.H{
		"ready":    len(files) > 0,
		"loading":  loading,
		"files":    fileList(files),
		"selected": selected,
		"cache": gin.H{
			"used":      s.mem.Used(),
			"limit":     s.mem.Limit(),
			"evictions": s.mem.Evictions(),
		},
	}
	if t != nil {
		resp["name"] = t.Name()
		resp["info_hash"] = t.InfoHash().String()
		resp["length"] = t.Length()
		resp["downloaded"] = t.BytesCompleted()
		resp["peers"] = t.Stats().ActivePeers
	}
	if f := fileAtIn(files, selected); f != nil {
		resp["size"] = f.Length()
	}
	// Byte kumulatif; UI menghitung Mbps dari selisih antar frame.
	fromPeers := int64(0)
	if t != nil {
		fromPeers = t.BytesCompleted()
	}
	resp["net"] = gin.H{"from_peers": fromPeers, "to_client": s.served.Load()}
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
	defer conn.Close()

	// Frame dari klien diabaikan; ReadMessage juga memproses ping/pong dan
	// memberi tahu saat klien pergi.
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				conn.Close()
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
	s.startRemux(req.Index)

	c.JSON(http.StatusOK, s.state())
}

// selectTorrentFile picks the video that Torrentio points at (its fileIdx is an
// index into the torrent's file list) and starts its remux.
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
			s.startRemux(i)
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
	defer res.Body.Close()
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

func (s *server) stream(c *gin.Context) {
	var f *torrent.File
	if rawIndex := c.Param("index"); rawIndex != "" {
		index, err := strconv.Atoi(rawIndex)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "index tidak valid"})
			return
		}
		f = s.fileAt(index)
	} else {
		f = s.fileAt(s.currentIndex())
	}
	if f == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "torrent metadata belum siap"})
		return
	}

	reader := f.NewReader()
	defer reader.Close()
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
