package main

import (
	"context"
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
	"github.com/gorilla/websocket"

	"zanime/internal/config"
	"zanime/internal/memstorage"
	"zanime/internal/stream"
)

//go:embed web
var webFiles embed.FS

const defaultMagnet = "magnet:?xt=urn:btih:3c5356a2834c80305e18479ad39d16a4f8b8682b&dn=%5BErai-raws%5D%20Gabriel%20DropOut%20-%2001%20~%2012%20%5B1080p%5D%5BMultiple%20Subtitle%5D&tr=http%3A%2F%2Fnyaa.tracker.wf%3A7777%2Fannounce&tr=udp%3A%2F%2Fopen.stealth.si%3A80%2Fannounce&tr=udp%3A%2F%2Ftracker.opentrackr.org%3A1337%2Fannounce&tr=udp%3A%2F%2Fexodus.desync.com%3A6969%2Fannounce&tr=udp%3A%2F%2Ftracker.torrent.eu.org%3A451%2Fannounce"

const metadataTimeout = 45 * time.Second

// wsDelay is how often /api/ws pushes the state the UI used to poll for.
const wsDelay = 700 * time.Millisecond

var (
	errMetadataTimeout = errors.New("metadata timeout")
	errLoadCanceled    = errors.New("pemuatan magnet dibatalkan")
)

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
			if err := srv.loadMagnet(context.Background(), *magnet); err != nil {
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
	r.GET("/api/ws", srv.apiWS)
	r.POST("/api/magnet", srv.apiMagnet)
	r.POST("/api/cancel", srv.apiCancel)
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

	addMu sync.Mutex // serializes the download part of a magnet load

	jobMu sync.Mutex
	job   *loadJob // non-nil while a magnet load is in flight

	mu       sync.RWMutex
	t        *torrent.Torrent
	files    []*torrent.File // video files only, torrent order
	selected int
	loading  bool
}

func newServer(cl *torrent.Client, mem *memstorage.Client, web fs.FS) *server {
	return &server{cl: cl, mem: mem, web: web, selected: -1}
}

// loadJob is one in-flight magnet load. It is owned by the server, not by the
// request that started it, so it can be cancelled from anywhere (POST
// /api/cancel) and shared: every request that asks for the same magnet waits on
// the same job instead of starting a second download.
type loadJob struct {
	magnet string
	hash   string // hex info hash, for spam detection
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}

	// err is set before done is closed, so a waiter that sees done closed reads
	// a finished value.
	err error
}

// beginLoad registers a job for magnet. A job already running for the same
// magnet is handed back to be waited on, so spamming the button (or reloading
// the page mid-load) never starts a second download. A job for a different
// magnet is cancelled first: the newest request wins.
func (s *server) beginLoad(ctx context.Context, magnet, hash string) (*loadJob, bool) {
	s.jobMu.Lock()
	defer s.jobMu.Unlock()
	if s.job != nil {
		if s.job.hash == hash {
			return s.job, true
		}
		s.job.cancel()
	}
	// The job outlives the caller: a client that gives up only stops waiting, and
	// a page reload keeps watching the same load. It ends via /api/cancel or the
	// metadata timeout.
	jobCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	job := &loadJob{
		magnet: magnet,
		hash:   hash,
		ctx:    jobCtx,
		cancel: cancel,
		done:   make(chan struct{}),
	}
	s.job = job
	s.mu.Lock()
	s.loading = true
	s.mu.Unlock()
	return job, false
}

// finishLoad publishes the job result and clears it, unless a newer job already
// replaced it (that newer job then owns `loading`).
func (s *server) finishLoad(job *loadJob, err error) {
	job.cancel()
	job.err = err
	s.jobMu.Lock()
	if s.job == job {
		s.job = nil
		s.mu.Lock()
		s.loading = false
		s.mu.Unlock()
	}
	s.jobMu.Unlock()
	close(job.done)
}

// cancelLoad ends the in-flight load, if any. The active torrent is untouched.
func (s *server) cancelLoad() bool {
	s.jobMu.Lock()
	defer s.jobMu.Unlock()
	if s.job == nil {
		return false
	}
	s.job.cancel()
	return true
}

// waitForJob blocks until the shared job finishes. Giving up (the caller's own
// request ending) does not stop the job.
func waitForJob(ctx context.Context, job *loadJob) error {
	select {
	case <-job.done:
		return job.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// currentHash is the hex info hash of the active torrent, or "" when there is
// none.
func (s *server) currentHash() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.t == nil {
		return ""
	}
	return s.t.InfoHash().String()
}

// loadMagnet replaces the active torrent and waits for its metadata. It is
// called both at startup and from POST /api/magnet, and returns once the job
// ends: success, metadata timeout, error, or cancellation.
func (s *server) loadMagnet(ctx context.Context, magnet string) (err error) {
	magnet = strings.TrimSpace(magnet)
	if magnet == "" {
		return errors.New("magnet kosong")
	}
	spec, err := torrent.TorrentSpecFromMagnetUri(magnet)
	if err != nil {
		return fmt.Errorf("magnet tidak valid: %w", err)
	}
	hash := spec.InfoHash.String()

	// Already the active torrent: pressing Muat again must not restart it.
	if s.currentHash() == hash {
		return nil
	}

	job, shared := s.beginLoad(ctx, magnet, hash)
	if shared {
		return waitForJob(ctx, job)
	}
	defer func() { s.finishLoad(job, err) }()

	s.addMu.Lock()
	defer s.addMu.Unlock()

	// Cancelled (or superseded by a newer magnet) while waiting for the lock.
	if job.ctx.Err() != nil {
		return errLoadCanceled
	}

	jobCtx, cancel := context.WithTimeout(job.ctx, metadataTimeout)
	defer cancel()

	t, _, err := s.cl.AddTorrentSpec(spec)
	if err != nil {
		return fmt.Errorf("tambah torrent: %w", err)
	}

	select {
	case <-t.GotInfo():
	case <-jobCtx.Done():
		go t.Drop()
		if errors.Is(jobCtx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("%w setelah %s", errMetadataTimeout, metadataTimeout)
		}
		return errLoadCanceled
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

	if err := s.loadMagnet(c.Request.Context(), req.Magnet); err != nil {
		// The browser is gone (aborted fetch, closed tab): nobody would read the
		// error, and the job itself keeps running on the server.
		if errors.Is(err, context.Canceled) {
			return
		}
		status := http.StatusBadGateway
		switch {
		case errors.Is(err, errMetadataTimeout):
			status = http.StatusGatewayTimeout
		case errors.Is(err, errLoadCanceled):
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}

	// fileIndex = urutan file di torrent (dari index Torrentio), bukan urutan
	// daftar video yang disortir; dipetakan lewat identitas *torrent.File.
	if req.FileIndex != nil {
		i, err := s.selectTorrentFile(*req.FileIndex)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		log.Printf("fileIndex %d -> video %d", *req.FileIndex, i)
	}
	c.JSON(http.StatusOK, s.state())
}

// apiCancel stops the in-flight magnet load, if any. The active torrent and its
// stream are left alone, so cancelling a bad magnet does not stop playback.
func (s *server) apiCancel(c *gin.Context) {
	if !s.cancelLoad() {
		c.JSON(http.StatusConflict, gin.H{"error": "tidak ada magnet yang sedang dimuat"})
		return
	}
	c.JSON(http.StatusOK, s.state())
}

// wsUpgrader accepts any origin: the UI is served by this same server, but a
// page opened from another host or from a file:// path should still be able to
// watch the status.
var wsUpgrader = websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}

// apiWS pushes the same payload as /api/state every wsDelay, so the UI does not
// have to poll. The browser may send anything; incoming frames are read only to
// notice the connection closing.
func (s *server) apiWS(c *gin.Context) {
	conn, err := wsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return // Upgrade already wrote the HTTP error
	}
	defer func() { _ = conn.Close() }()

	closed := make(chan struct{})
	go func() {
		defer close(closed)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	ticker := time.NewTicker(wsDelay)
	defer ticker.Stop()
	for {
		if err := writeWSState(conn, s.state()); err != nil {
			return
		}
		select {
		case <-closed:
			return
		case <-c.Request.Context().Done():
			return
		case <-ticker.C:
		}
	}
}

// writeWSState is only ever called from the API handler's own goroutine: the
// gorilla connection allows one concurrent writer.
func writeWSState(conn *websocket.Conn, state gin.H) error {
	if err := conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return err
	}
	return conn.WriteJSON(state)
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
	defer func() { _ = reader.Close() }()
	reader.SetReadahead(32 << 20) // supaya byte tidak "berhenti-henti": stalling pembacaan membuat clock player diam di 00:00
	stream.Serve(c, path.Base(f.Path()), reader)
}

func (s *server) currentIndex() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.selected
}

// selectTorrentFile selects file number idx in torrent order and returns its
// index in the sorted video list. Only video files map; anything else (sample,
// nfo) is rejected rather than silently playing the wrong episode.
func (s *server) selectTorrentFile(idx int) (int, error) {
	s.mu.RLock()
	t, files := s.t, s.files
	s.mu.RUnlock()
	if t == nil {
		return 0, errors.New("torrent belum siap")
	}
	all := t.Files()
	if idx < 0 || idx >= len(all) {
		return 0, fmt.Errorf("fileIndex %d di luar jangkauan torrent", idx)
	}
	f := all[idx]
	for i, vf := range files {
		if vf == f {
			s.mu.Lock()
			s.selected = i
			s.mu.Unlock()
			return i, nil
		}
	}
	return 0, fmt.Errorf("file %d bukan video", idx)
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
