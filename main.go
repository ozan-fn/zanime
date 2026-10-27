package main

import (
	"embed"
	"errors"
	"flag"
	"fmt"
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
	"time"

	"github.com/anacrolix/torrent"
	"github.com/gin-gonic/gin"

	"zanime/internal/config"
	"zanime/internal/memstorage"
	"zanime/internal/stream"
)

//go:embed web
var webFiles embed.FS

const defaultMagnet = "magnet:?xt=urn:btih:3c5356a2834c80305e18479ad39d16a4f8b8682b&dn=%5BErai-raws%5D%20Gabriel%20DropOut%20-%2001%20~%2012%20%5B1080p%5D%5BMultiple%20Subtitle%5D&tr=http%3A%2F%2Fnyaa.tracker.wf%3A7777%2Fannounce&tr=udp%3A%2F%2Fopen.stealth.si%3A80%2Fannounce&tr=udp%3A%2F%2Ftracker.opentrackr.org%3A1337%2Fannounce&tr=udp%3A%2F%2Fexodus.desync.com%3A6969%2Fannounce&tr=udp%3A%2F%2Ftracker.torrent.eu.org%3A451%2Fannounce"

const metadataTimeout = 45 * time.Second

var errMetadataTimeout = errors.New("metadata timeout")

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

	srv := newServer(cl, mem, webRoot)
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
	r.GET("/api/state", srv.apiState)
	r.POST("/api/magnet", srv.apiMagnet)
	r.POST("/api/select", srv.apiSelect)
	r.GET("/stream", srv.stream)
	r.HEAD("/stream", srv.stream)
	r.GET("/stream/:index/:name", srv.stream)
	r.HEAD("/stream/:index/:name", srv.stream)

	log.Printf("listening on %s (memory cache %d MiB, zero disk)", browserURL(*addr), *cacheMB)
	if err := r.Run(*addr); err != nil {
		log.Fatal(err)
	}
}

// browserURL turns a listen address into something clickable in a terminal:
// a wildcard host becomes localhost, since 0.0.0.0 is not a browsable target.
func browserURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "localhost"
	}
	return "http://" + net.JoinHostPort(host, port)
}

type server struct {
	cl  *torrent.Client
	mem *memstorage.Client
	web fs.FS

	addMu sync.Mutex // serializes magnet loads

	mu       sync.RWMutex
	t        *torrent.Torrent
	files    []*torrent.File // video files only, torrent order
	selected int
	loading  bool
}

func newServer(cl *torrent.Client, mem *memstorage.Client, web fs.FS) *server {
	return &server{cl: cl, mem: mem, web: web, selected: -1}
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

	s.mu.Lock()
	s.loading = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.loading = false
		s.mu.Unlock()
	}()

	t, _, err := s.cl.AddTorrentSpec(spec)
	if err != nil {
		return fmt.Errorf("tambah torrent: %w", err)
	}

	select {
	case <-t.GotInfo():
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
	}
	log.Printf("torrent siap: %s (%d file video)", t.Name(), len(files))
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
	return resp
}

func (s *server) apiMagnet(c *gin.Context) {
	var req struct {
		Magnet string `json:"magnet"`
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

	c.JSON(http.StatusOK, s.state())
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
