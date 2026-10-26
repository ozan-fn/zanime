package torrent

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	alog "github.com/anacrolix/log"
	"github.com/anacrolix/missinggo/v2/filecache"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/storage"
	"github.com/gorilla/websocket"

	"zanime/internal/ffmpeg"
	"zanime/internal/mkv"
	"zanime/internal/tmpdir"
)

// Tracker publik tambahan (standar umum) agar peer mudah ditemukan.
var defaultTrackers = []string{
	"udp://tracker.opentrackr.org:1337/announce",
	"udp://open.demonii.com:1337/announce",
	"udp://exodus.desync.com:6969/announce",
	"udp://tracker.torrent.eu.org:451/announce",
}

var hexHash = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

// wsUpgrader: status real-time via WebSocket. Origin dicek agar cocok dengan host
// (tetap menerima non-browser yang tidak mengirim Origin).
var wsUpgrader = websocket.Upgrader{
	WriteBufferSize: 64 << 10,
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		u, err := url.Parse(origin)
		return err == nil && u.Host == r.Host
	},
}

// lowDataMode: mode hemat kuota (ZANIME_TORRENT_NO_DHT) — tanpa DHT/PEX/tracker internet.
var lowDataMode bool

// readerReadahead: batas "minta ke depan" untuk reader video. Diatur dari batas cache
// di New(); tanpa batas, anacrolix meminta sejauh yang sudah dibaca sehingga data yang
// belum sempat dibaca bisa terbuang dari cache berbatas (reader macet).
var readerReadahead int64 = 8 << 20

// videoReader: reader sekuensial dengan readahead terbatas (lihat readerReadahead).
func videoReader(f *torrent.File) torrent.Reader {
	return videoReaderAhead(f, readerReadahead)
}

func videoReaderAhead(f *torrent.File, ahead int64) torrent.Reader {
	rd := f.NewReader()
	rd.SetReadahead(ahead)
	return rd
}

// byteCounter: hitung byte yang benar-benar dibaca (untuk log biaya probe).
type byteCounter struct {
	r io.Reader
	n int64
}

func (c *byteCounter) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

func toFFmpegTracks(ts []mkv.Track) []ffmpeg.Track {
	out := make([]ffmpeg.Track, 0, len(ts))
	for _, t := range ts {
		out = append(out, ffmpeg.Track{Index: t.Index, Lang: t.Lang, Title: t.Title, Codec: t.Codec})
	}
	return out
}

// shortID: potongan hash untuk log (tidak perlu panjang penuh).
func shortID(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

var videoExt = map[string]bool{
	".mkv": true, ".mp4": true, ".m4v": true, ".webm": true, ".mov": true,
	".avi": true, ".wmv": true, ".ts": true, ".m2ts": true, ".mpg": true,
	".mpeg": true, ".ogv": true, ".flv": true,
}

// Manager: client torrent + peta sesi.
type Manager struct {
	c  *torrent.Client
	mu sync.Mutex
	m  map[string]*Session
}

// Session: satu torrent yang sedang dipakai untuk play.
type Session struct {
	sid string
	t   *torrent.Torrent

	mu       sync.Mutex
	ready    bool
	errStr   string
	VideoIdx int
	Video    struct {
		Idx       int    `json:"idx"`
		Name      string `json:"name"`
		Size      int64  `json:"size"`
		Ext       string `json:"ext"`
		Container string `json:"container"` // "direct" | "remux"
	}
	extSubs []int // file index subtitle eksternal (.srt/.ass/.vtt)

	probeStarted bool
	probeDone    bool
	probeErr     string
	probeTries   int
	probeTryAt   time.Time
	audio        []ffmpeg.Track
	embSubs      []ffmpeg.Track
	duration     float64 // detik, dari ffprobe

	// ekstraksi subtitle di latar belakang (tidak terikat request/browser)
	subMu   sync.Mutex
	subJobs map[string]bool
	subErr  map[string]string

	remuxRunning bool   // sedang transmux MKV → mp4 untuk request yang aktif
	remuxErr     string // error transmux terakhir (ditampilkan di UI)

	// statistik kecepatan (sampel tiap status poll)
	streamed   atomic.Int64 // byte yang sudah dikirim server → client
	lastSample time.Time
	lastDown   int64
	lastUp     int64
	downBps    float64
	upBps      float64

	lastUsed  time.Time
	startedAt time.Time // untuk log: berapa lama metadata/metadata→probe
}

// cacheLimitMB: batas ukuran data torrent di disk. Potongan lama otomatis dibuang
// (LRU) supaya panel kecil tidak penuh — episode panjang tidak pernah menumpuk.
func cacheLimitMB() int64 {
	limit := int64(512)
	if v := strings.TrimSpace(os.Getenv("ZANIME_CACHE_LIMIT_MB")); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			limit = n
		}
	}
	if limit < 128 {
		limit = 128 // margin aman: reader/probe/subtitle butuh ruang lebih dari readahead
	}
	return limit
}

func New() (*Manager, error) {
	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = os.Getenv("ZANIME_TORRENT_DATA_DIR") // test: folder terpisah supaya tidak memakai unduhan lama
	if cfg.DataDir == "" {
		cfg.DataDir = filepath.Join(tmpdir.Base(), "zanime-data")
	}
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return nil, err
	}
	// Storage berbatas: potongan yang sudah lewat dibuang, jadi pemakaian disk tidak
	// melebihi batas walau episodenya 2+ GiB.
	fc, err := filecache.NewCache(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	limitMB := cacheLimitMB()
	capBytes := limitMB << 20
	fc.SetCapacity(capBytes)
	fc.TrimToCapacity() // langsung buang sisa unduhan versi lama yang melebihi batas
	// Capacity juga membatasi berapa banyak potongan yang diminta sekaligus; tanpa ini client
	// mengunduh jauh ke depan lalu memotong data yang belum sempat dibaca reader (macet).
	readerReadahead = capBytes / 8
	if readerReadahead > 16<<20 {
		readerReadahead = 16 << 20
	}
	if readerReadahead < 256<<10 {
		readerReadahead = 256 << 10
	}
	capFunc := func() (int64, bool) { return capBytes, true }
	cfg.DefaultStorage = storage.NewResourcePiecesOpts(fc.AsResourceProvider(),
		storage.ResourcePiecesOpts{LeaveIncompleteChunks: true, Capacity: &capFunc})
	log.Printf("cache torrent: %s (maks %d MB)", cfg.DataDir, limitMB)
	// Mode klien saja: tidak pernah upload data dan tidak menerima koneksi masuk,
	// jadi proses ini tidak berperan sebagai server/seed.
	cfg.NoUpload = true
	cfg.AcceptPeerConnections = false
	cfg.NoDefaultPortForwarding = true // tak perlu buka port di router (tidak melayani peer)
	// Buffer bawaan anacrolix besar (50 koneksi × 1 MiB peer buffer + 64 MiB chunk
	// belum terverifikasi) — itu yang membengkakkan RAM saat streaming.
	cfg.EstablishedConnsPerTorrent = 30
	cfg.MaxAllocPeerRequestDataPerConn = 256 << 10
	cfg.MaxUnverifiedBytes = 16 << 20
	if os.Getenv("ZANIME_TORRENT_DEBUG") != "" {
		cfg.Debug = true
		cfg.Logger = alog.Default
	} else {
		cfg.Logger = quietLogger()
	}
	if os.Getenv("ZANIME_TORRENT_NO_DHT") != "" {
		// mode hemat kuota: tanja DHT/PEX (hanya jaringan lokal/trackers)
		cfg.NoDHT = true
		cfg.DisablePEX = true
		lowDataMode = true
	}
	c, err := torrent.NewClient(cfg)
	if err != nil {
		// port default (42069) kemungkinan sudah dipakai instance lain → port acak
		cfg.ListenPort = 0
		c, err = torrent.NewClient(cfg)
		if err != nil {
			return nil, err
		}
	}
	return &Manager{c: c, m: map[string]*Session{}}, nil
}

func (m *Manager) Close() { m.c.Close() }

// quietLogger: log bawaan anacrolix, tapi tanpa error "read failed after reader reset".
// Pesan itu muncul saat reader ditutup bersamaan torrent ditutup — bukan gangguan nyata,
// hanya bising di log panel.
func quietLogger() alog.Logger {
	l := alog.Default
	l.SetHandlers(quietHandler{next: alog.DefaultHandler})
	return l
}

type quietHandler struct{ next alog.Handler }

func (h quietHandler) Handle(r alog.Record) {
	if strings.Contains(r.Msg.String(), "read failed after reader reset") {
		return
	}
	h.next.Handle(r)
}

func (m *Manager) get(sid string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.m[sid]
	if s != nil {
		s.mu.Lock()
		s.lastUsed = time.Now()
		s.mu.Unlock()
	}
	return s
}

// Start: tambah magnet bila belum ada; pengambilan info jalan async.
func (m *Manager) Start(hash string, trs []string, fileIdx int) (*Session, error) {
	hash = strings.ToLower(strings.TrimSpace(hash))
	hash = strings.TrimPrefix(hash, "urn:btih:")
	if !hexHash.MatchString(hash) {
		return nil, fmt.Errorf("hash tidak valid")
	}
	if s := m.get(hash); s != nil {
		return s, nil
	}
	var qb strings.Builder
	qb.WriteString("magnet:?xt=urn:btih:" + hash)
	allTrackers := trs
	if !lowDataMode {
		allTrackers = append(allTrackers, defaultTrackers...)
	}
	for _, tr := range allTrackers {
		qb.WriteString("&tr=")
		qb.WriteString(url.QueryEscape(tr))
	}
	t, err := m.c.AddMagnet(qb.String())
	if err != nil {
		// torrent kemungkinan sudah ada (session lain): pakai yang ada
		for _, t2 := range m.c.Torrents() {
			if t2.InfoHash().HexString() == hash {
				t = t2
				break
			}
		}
		if t == nil {
			return nil, err
		}
	}
	s := &Session{sid: hash, t: t, lastUsed: time.Now(), startedAt: time.Now()}
	m.mu.Lock()
	m.m[hash] = s
	m.mu.Unlock()
	go m.waitInfo(s, fileIdx)
	return s, nil
}

func (m *Manager) waitInfo(s *Session, fileIdx int) {
	select {
	case <-s.t.GotInfo():
	case <-time.After(90 * time.Second):
		s.mu.Lock()
		s.errStr = "timeout menunggu info torrent (peer tidak merespon)"
		s.mu.Unlock()
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.ready = true
	log.Printf("torrent %s siap: %d file, %s setelah start, peers=%d",
		shortID(s.sid), len(s.t.Files()), time.Since(s.startedAt).Round(time.Millisecond),
		s.t.Stats().TotalPeers)
	var largest *torrent.File
	s.VideoIdx = -1
	for i, f := range s.t.Files() {
		ext := strings.ToLower(filepath.Ext(f.Path()))
		switch ext {
		case ".srt", ".ass", ".ssa", ".vtt":
			s.extSubs = append(s.extSubs, i)
		}
		if videoExt[ext] && (largest == nil || f.Length() > largest.Length()) {
			largest = f
		}
	}
	if fileIdx >= 0 && fileIdx < len(s.t.Files()) &&
		videoExt[strings.ToLower(filepath.Ext(s.t.Files()[fileIdx].Path()))] {
		s.VideoIdx = fileIdx
	} else if largest != nil {
		for i, f := range s.t.Files() {
			if f == largest {
				s.VideoIdx = i
				break
			}
		}
	}
	if s.VideoIdx >= 0 {
		vf := s.t.Files()[s.VideoIdx]
		ext := strings.ToLower(filepath.Ext(vf.Path()))
		s.Video.Idx = s.VideoIdx
		s.Video.Name = vf.Path()
		s.Video.Size = vf.Length()
		s.Video.Ext = ext
		switch ext {
		case ".mp4", ".m4v", ".webm", ".ogv", ".mov":
			s.Video.Container = "direct"
		default:
			s.Video.Container = "remux"
		}
	} else {
		s.errStr = "tidak ada file video dalam torrent"
	}
}

func (m *Manager) startProbe(s *Session) {
	s.mu.Lock()
	// izinkan retry saat probe gagal (maks 3 percobaan, jeda 5 dtk)
	retry := s.probeErr != "" && s.probeTries < 3 && time.Since(s.probeTryAt) > 5*time.Second
	if !s.ready || s.VideoIdx < 0 || (s.probeStarted && !retry) {
		s.mu.Unlock()
		return
	}
	s.probeStarted = true
	s.probeTries++
	s.probeTryAt = time.Now()
	vIdx := s.VideoIdx
	s.mu.Unlock()
	go func() {
		probeBin, err := ffmpeg.ProbePath()
		if err != nil {
			s.mu.Lock()
			s.probeErr = err.Error()
			s.mu.Unlock()
			return
		}
		files := s.t.Files()
		if vIdx >= len(files) {
			s.mu.Lock()
			s.probeErr = "file video hilang"
			s.mu.Unlock()
			return
		}
		// MKV: baca header sendiri (EBML) — berhenti sebelum lampiran font, jadi hanya
		// puluhan KB dan durasi + daftar track keluar dalam hitungan detik.
		if strings.EqualFold(s.Video.Ext, ".mkv") {
			start := time.Now()
			cr := &byteCounter{r: videoReaderAhead(files[vIdx], 1<<20)}
			if info, merr := mkv.Probe(cr); merr == nil {
				s.mu.Lock()
				s.probeDone = true
				s.duration = info.Duration
				s.audio = toFFmpegTracks(info.Audio)
				s.embSubs = toFFmpegTracks(info.Subs)
				s.mu.Unlock()
				log.Printf("probe mkv %s: %d byte, %s, audio=%d sub=%d dur=%.0f dtk",
					shortID(s.sid), cr.n, time.Since(start).Round(time.Millisecond),
					len(info.Audio), len(info.Subs), info.Duration)
				return
			}
			log.Printf("probe mkv %s: header tidak terbaca, fallback ffprobe", shortID(s.sid))
		}
		// Tahap 1 sengaja kecil (≈1 MB, readahead 2 MB): header MKV/MP4 hampir selalu
		// ada di blok awal, jadi player bisa mulai cepat walau peer lambat.
		start := time.Now()
		audio, subs, dur, perr := ffmpeg.Probe(probeBin,
			videoReaderAhead(files[vIdx], 2<<20), "1M", "2000000")
		// Header tidak biasa (mis. banyak lampiran font): coba sekali lagi dengan jendela lebih besar.
		if perr == nil && dur == 0 && len(audio) == 0 && len(subs) == 0 {
			audio, subs, dur, perr = ffmpeg.Probe(probeBin,
				videoReaderAhead(files[vIdx], 16<<20), "5M", "5000000")
		}
		log.Printf("probe %s: %s, audio=%d sub=%d dur=%.0f dtk, unduh=%d byte, peers=%d, err=%v",
			shortID(s.sid), time.Since(start).Round(time.Millisecond),
			len(audio), len(subs), dur, s.t.BytesCompleted(), s.t.Stats().TotalPeers, perr)
		s.mu.Lock()
		s.probeDone = true
		if perr != nil {
			s.probeErr = perr.Error()
		} else {
			s.audio = audio
			s.embSubs = subs
			s.duration = dur
		}
		s.mu.Unlock()
		// subtitle tertanam tidak diekstrak sekarang: ikut pass video saat diputar
		// (satu kali baca file, tanpa unduh penuh lebih dulu).
	}()
}

// ---------- handler HTTP ----------

func (m *Manager) HandleStart(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	fileIdx := -1
	if fi := q.Get("fileIdx"); fi != "" {
		fileIdx, _ = strconv.Atoi(fi)
	}
	s, err := m.Start(q.Get("hash"), q["tr"], fileIdx)
	if err != nil {
		writeErr(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	writeJSON(w, map[string]any{
		"sid":   s.sid,
		"ready": s.ready,
		"err":   s.errStr,
	})
}

func (m *Manager) HandleStatus(w http.ResponseWriter, r *http.Request) {
	sid := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/torrent/status/"), "/")
	s := m.get(sid)
	if s == nil {
		writeErr(w, "sesi tidak ada (sudah berhenti?)", http.StatusNotFound)
		return
	}
	m.startProbe(s)
	// ?warm=f3 → siapkan subtitle file terpisah (kecil, aman diekstrak sendiri).
	// Subtitle tertanam hanya diekstrak terpisah kalau tidak ada pass video (container
	// direct) — selain itu ia mengalir dari pass video.
	if warm := strings.TrimSuffix(r.URL.Query().Get("warm"), ".vtt"); len(warm) >= 2 {
		s.mu.Lock()
		ok := s.ready && s.VideoIdx >= 0
		container := s.Video.Container
		s.mu.Unlock()
		if ok {
			if _, err := strconv.Atoi(warm[1:]); err == nil &&
				(warm[0] == 'f' || (warm[0] == 't' && container == "direct")) {
				m.subExtract(s, s.VideoIdx, warm)
			}
		}
	}
	writeJSON(w, s.payload())
}

// HandleWS: push status (kecepatan, unduhan, subtitle) tiap 100 ms lewat WebSocket
// supaya indikator alur di UI bergerak real-time tanpa polling.
func (m *Manager) HandleWS(w http.ResponseWriter, r *http.Request) {
	sid := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/torrent/ws/"), "/")
	s := m.get(sid)
	if s == nil {
		writeErr(w, "sesi tidak ada (sudah berhenti?)", http.StatusNotFound)
		return
	}
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return // upgrade sudah mengirim error
	}
	defer conn.Close()
	done := make(chan struct{})
	go func() { // deteksi client pergi / jaringan putus
		defer close(done)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()
	m.startProbe(s)
	ticker := time.NewTicker(100 * time.Millisecond) // push 10×/dtk → indikator bergerak halus
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := conn.WriteJSON(s.payload()); err != nil {
				return
			}
		case <-done:
			return
		}
	}
}

// payload: JSON status sesi — dipakai polling HTTP dan push WebSocket.
func (s *Session) payload() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastUsed = time.Now() // ada yang memantau → sesi tidak dianggap idle
	// nama file subtitle eksternal (untuk label di UI) — hanya setelah info ready
	var subFiles []map[string]any
	if s.ready {
		files := s.t.Files()
		for _, idx := range s.extSubs {
			if idx < len(files) {
				name := filepath.Base(files[idx].Path())
				subFiles = append(subFiles, map[string]any{"idx": idx, "name": strings.TrimSuffix(name, filepath.Ext(name))})
			}
		}
	}
	resp := map[string]any{
		"sid":      s.sid,
		"ready":    s.ready,
		"err":      s.errStr,
		"video":    s.Video,
		"subFiles": subFiles,
		"probe":    map[string]any{"started": s.probeStarted, "done": s.probeDone, "err": s.probeErr},
		"audio":    s.audio,
		"subsEmb":  s.embSubs,
		"duration": s.duration,
		// subtitle yang sudah siap dipakai player (ref: "t7" / "f3")
		"subsCached": s.cachedSubs(),
		// yang masih mengalir dari pass video: ref → ukuran byte (untuk cache-busting)
		"subsPartial": s.partialSubs(),
		"subsErr":     s.subErrors(),
	}
	// statistik koneksi — tersedia bahkan sebelum info ready (untuk debugging UI)
	stats := s.t.Stats()
	resp["seeders"] = stats.ConnectedSeeders + stats.HalfOpenPeers
	resp["peers"] = stats.TotalPeers
	if s.ready && s.errStr == "" {
		down := s.t.BytesCompleted()
		resp["downloaded"] = down
		resp["total"] = s.Video.Size
		// kecepatan: peer → server (download) dan server → client (upload/stream)
		up := s.streamed.Load()
		now := time.Now()
		if !s.lastSample.IsZero() {
			if dt := now.Sub(s.lastSample).Seconds(); dt >= 0.09 {
				// haluskan sedikit: sampel 100 ms sangat loncat-loncat
				s.downBps = smoothRate(s.downBps, float64(down-s.lastDown)/dt)
				s.upBps = smoothRate(s.upBps, float64(up-s.lastUp)/dt)
				s.lastSample, s.lastDown, s.lastUp = now, down, up
			}
		} else {
			s.lastSample, s.lastDown, s.lastUp = now, down, up
		}
		resp["downBps"] = s.downBps
		resp["upBps"] = s.upBps
		resp["served"] = up
	}
	// status transmux: hanya untuk pesan UI (tidak ada cache di disk)
	if s.remuxRunning || s.remuxErr != "" {
		resp["remux"] = map[string]any{"running": s.remuxRunning, "err": s.remuxErr}
	}
	return resp
}

func (m *Manager) HandleStop(w http.ResponseWriter, r *http.Request) {
	sid := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/torrent/stop/"), "/")
	m.mu.Lock()
	s := m.m[sid]
	if s != nil {
		delete(m.m, sid)
	}
	m.mu.Unlock()
	if s != nil {
		_ = os.RemoveAll(s.cacheDir())
		s.t.Drop()
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (m *Manager) HandleStream(w http.ResponseWriter, r *http.Request) {
	// /stream/{sid}/{fileIdx}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 3 || parts[0] != "stream" {
		http.NotFound(w, r)
		return
	}
	s := m.get(parts[1])
	if s == nil {
		writeErr(w, "sesi tidak ada (sudah berhenti?)", http.StatusNotFound)
		return
	}
	fIdx, err := strconv.Atoi(parts[2])
	if err != nil {
		writeErr(w, "fileIdx tidak valid", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	ready := s.ready
	perr := s.errStr
	videoIdx := s.VideoIdx
	container := s.Video.Container
	s.mu.Unlock()
	if !ready {
		writeErr(w, "torrent belum siap", http.StatusPreconditionFailed)
		return
	}
	if perr != "" {
		writeErr(w, perr, http.StatusPreconditionFailed)
		return
	}
	if fIdx != videoIdx {
		writeErr(w, "bukan file video sesi ini", http.StatusBadRequest)
		return
	}
	files := s.t.Files()
	if fIdx >= len(files) {
		writeErr(w, "file tidak ada di torrent", http.StatusBadRequest)
		return
	}

	// param audio = index stream global (dari probe) → ubah ke urutan ke-N stream audio
	audioIdx := 0
	if a := r.URL.Query().Get("audio"); a != "" {
		req, _ := strconv.Atoi(a)
		s.mu.Lock()
		tracks := append([]ffmpeg.Track(nil), s.audio...)
		s.mu.Unlock()
		audioIdx = -1
		for i, tr := range tracks {
			if tr.Index == req {
				audioIdx = i
				break
			}
		}
		if audioIdx < 0 {
			audioIdx = req // belum terprobe / tidak cocok → anggap posisi
		}
	}

	// hitung byte yang benar-benar dikirim ke client (untuk indikator kecepatan)
	sw := &streamCounter{ResponseWriter: w, n: &s.streamed}

	if container == "direct" {
		rd := videoReader(files[fIdx])
		defer func() { _ = rd.Close() }()
		http.ServeContent(sw, r, filepath.Base(files[fIdx].Path()), time.Time{}, rd)
		return
	}

	// MKV/TS/AVI dll: transmux ke mp4 progresif LANGSUNG ke client.
	// Tidak ada cache video di disk (penyimpanan terbatas); audio di-copy dulu dan
	// hanya di-encode AAC kalau ffmpeg menolak copy (AC3/E-AC3/DTS) — CPU rendah.
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	m.streamRemux(w, r, s, fIdx, audioIdx)
}

// streamRemux: satu pass ffmpeg → response HTTP. Percobaan pertama selalu copy
// (CPU nyaris nol). Kalau header ditolak (AC3/E-AC3 tidak bisa masuk fragmented mp4),
// diulang sekali dengan audio AAC selama belum ada byte yang terkirim.
func (m *Manager) streamRemux(w http.ResponseWriter, r *http.Request, s *Session, fIdx, audioIdx int) {
	bin, err := ffmpeg.Path()
	if err != nil {
		writeErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.mu.Lock()
	s.remuxRunning, s.remuxErr = true, ""
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.remuxRunning = false
		s.mu.Unlock()
	}()

	sw := &streamCounter{ResponseWriter: w, n: &s.streamed}
	// Subtitle tertanam diekstrak dalam pass ffmpeg yang SAMA dengan video:
	// file dibaca sekali, jadi subtitle tidak perlu unduh ulang seluruh episode.
	m.waitProbe(s, 5*time.Second)
	s.mu.Lock()
	dur := s.duration
	s.mu.Unlock()
	subFiles := s.embeddedSubFiles()
	if len(subFiles) > 0 {
		log.Printf("subtitle %s ikut pass video (%d track)", shortID(s.sid), len(subFiles))
	}
	attempt := func(aac bool) (bool, error) {
		s.mu.Lock()
		files := s.t.Files()
		if fIdx >= len(files) {
			s.mu.Unlock()
			return false, fmt.Errorf("file video hilang")
		}
		reader := videoReader(files[fIdx])
		s.mu.Unlock()
		defer func() { _ = reader.Close() }()

		hw := &holdWriter{w: sw, flusher: sw, limit: 64 << 10}
		rerr := ffmpeg.RemuxProgressive(bin, reader, ffmpeg.RemuxOpts{
			AudioIdx: audioIdx, AudioAAC: aac, SubFiles: subFiles, Duration: dur,
		}, hw)
		if rerr == nil {
			rerr = hw.Flush()
		}
		return hw.sent, rerr
	}

	sent, rerr := attempt(false)
	if rerr != nil && !sent && ffmpeg.IsHeaderErr(rerr) {
		sent, rerr = attempt(true) // darurat: audio wajib AAC
	}
	if rerr == nil && len(subFiles) > 0 {
		s.finishSubFiles(subFiles) // baru dianggap siap kalau pass selesai utuh
	}
	if rerr != nil {
		s.mu.Lock()
		s.remuxErr = rerr.Error()
		s.mu.Unlock()
		if !sent { // sudah terkirim byte → status HTTP tak bisa diubah lagi
			writeErr(w, rerr.Error(), http.StatusInternalServerError)
		}
	}
}

// waitProbe: beri kesempatan probe header selesai sebelum pass video dimulai, karena
// daftar subtitle tertanam dibaca dari situ (probe mkv hanya ~32 KB, jadi singkat).
func (m *Manager) waitProbe(s *Session, max time.Duration) {
	m.startProbe(s)
	deadline := time.Now().Add(max)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		done := s.probeDone || !s.probeStarted
		s.mu.Unlock()
		if done {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// embeddedSubFiles: subtitle tertanam yang belum siap → jalur file sementara untuk
// ditulis ffmpeg bersamaan dengan transmux (tanpa baca ulang file video).
func (s *Session) embeddedSubFiles() map[int]string {
	s.mu.Lock()
	emb := append([]ffmpeg.Track(nil), s.embSubs...)
	s.mu.Unlock()
	if len(emb) == 0 {
		return nil
	}
	out := make(map[int]string, len(emb))
	for _, t := range emb {
		ref := fmt.Sprintf("t%d", t.Index)
		if st, err := os.Stat(s.subCachePath(ref)); err == nil && st.Size() > 10 {
			continue // sudah lengkap dari pass sebelumnya
		}
		p := s.subCachePath(ref) + ".part"
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			continue
		}
		out[t.Index] = p
	}
	return out
}

// finishSubFiles: pass transmux selesai → .part jadi subtitle siap pakai.
func (s *Session) finishSubFiles(files map[int]string) {
	for idx, part := range files {
		data, err := os.ReadFile(part)
		if err != nil || len(data) <= 10 {
			continue
		}
		s.saveSub(fmt.Sprintf("t%d", idx), []byte(srtToVTT(string(data))))
	}
}

// holdWriter: tahan byte pertama sampai cukup banyak, supaya percobaan ffmpeg
// yang gagal di header bisa diulang tanpa mengirim data sampah ke player.
type holdWriter struct {
	w       io.Writer
	flusher http.Flusher
	limit   int
	buf     []byte
	sent    bool
}

func (h *holdWriter) Write(p []byte) (int, error) {
	if h.sent {
		n, err := h.w.Write(p)
		h.flush()
		return n, err
	}
	h.buf = append(h.buf, p...)
	if len(h.buf) < h.limit {
		return len(p), nil
	}
	h.sent = true
	_, err := h.w.Write(h.buf)
	h.buf = nil
	h.flush()
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

// Flush: keluarkan sisa buffer saat ffmpeg selesai normal.
func (h *holdWriter) Flush() error {
	if h.sent || len(h.buf) == 0 {
		return nil
	}
	h.sent = true
	_, err := h.w.Write(h.buf)
	h.buf = nil
	h.flush()
	return err
}

func (h *holdWriter) flush() {
	if h.flusher != nil {
		h.flusher.Flush()
	}
}

// streamCounter: hitung byte terkirim ke client + teruskan Flush.
type streamCounter struct {
	http.ResponseWriter
	n *atomic.Int64
}

func (c *streamCounter) Write(p []byte) (int, error) {
	n, err := c.ResponseWriter.Write(p)
	c.n.Add(int64(n))
	return n, err
}

func (c *streamCounter) Flush() {
	if f, ok := c.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (m *Manager) HandleSubs(w http.ResponseWriter, r *http.Request) {
	// /subs/{sid}/{videoIdx}/{ref} ; ref = "f{fileIdx}" (file sub) atau "t{streamIdx}" (embedded)
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 4 || parts[0] != "subs" {
		http.NotFound(w, r)
		return
	}
	s := m.get(parts[1])
	if s == nil {
		writeErr(w, "sesi tidak ada (sudah berhenti?)", http.StatusNotFound)
		return
	}
	vIdx, err := strconv.Atoi(parts[2])
	if err != nil {
		writeErr(w, "videoIdx tidak valid", http.StatusBadRequest)
		return
	}
	ref := strings.TrimSuffix(parts[3], ".vtt")
	if len(ref) < 2 || (ref[0] != 'f' && ref[0] != 't') {
		http.NotFound(w, r)
		return
	}
	n, err := strconv.Atoi(ref[1:])
	if err != nil {
		writeErr(w, "ref tidak valid", http.StatusBadRequest)
		return
	}
	// sudah pernah diekstrak → sajikan dari cache (tidak baca file video lagi)
	if data, rerr := os.ReadFile(s.subCachePath(ref)); rerr == nil && len(data) > 10 {
		serveVTT(w, data)
		return
	}
	files := s.t.Files()
	if vIdx < 0 || vIdx >= len(files) {
		writeErr(w, "videoIdx tidak valid", http.StatusBadRequest)
		return
	}

	switch ref[0] {
	case 'f': // file subtitle terpisah: kecil → ekstrak langsung
		if n < 0 || n >= len(files) {
			writeErr(w, "file sub tidak ditemukan", http.StatusNotFound)
			return
		}
		bin, err := ffmpeg.Path()
		if err != nil {
			writeErr(w, err.Error(), http.StatusInternalServerError)
			return
		}
		data, err := ffmpeg.ExtractToVTT(bin, files[n].NewReader(), "-i", "pipe:0")
		if err != nil {
			writeErr(w, err.Error(), http.StatusInternalServerError)
			return
		}
		vtt := []byte(srtToVTT(string(data)))
		s.saveSub(ref, vtt)
		serveVTT(w, vtt)
	case 't': // sub tertanam: mengalir dari pass video (tidak ada baca ulang)
		if data, ok := s.partialSub(ref); ok {
			// sebagian cue sudah tersedia → player langsung bisa menampilkan
			serveVTT(w, data)
			return
		}
		s.mu.Lock()
		emb := s.embSubs
		s.mu.Unlock()
		found := false
		for _, e := range emb {
			if e.Index == n {
				found = true
				break
			}
		}
		if !found {
			m.startProbe(s) // probe mungkin belum selesai / gagal → coba lagi
			writeErr(w, "subtitle belum terdeteksi — tunggu probe selesai", http.StatusNotFound)
			return
		}
		// container direct tidak lewat ffmpeg → ekstrak sendiri (file tetap harus
		// dibaca utuh, tapi ini jarang: mp4/webm umumnya tanpa subtitle tertanam).
		if s.Video.Container == "direct" {
			m.subExtract(s, vIdx, ref)
		}
		// belum ada data: balas VTT kosong yang valid + status pending, player akan
		// memuat ulang setelah pass video mulai menulis subtitle ini.
		w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
		w.Header().Set("X-Sub-Status", "pending")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("WEBVTT\n\n"))
	}
}

func serveVTT(w http.ResponseWriter, data []byte) {
	w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(data)
}

// srtToVTT: ubah keluaran SRT/WebVTT ffmpeg jadi WebVTT bersih — header tunggal,
// waktu koma→titik, tanpa nomor cue SRT, dan tanpa baris kosong beruntun
// (baris kosong ganda membuat sebagian browser mengabaikan cue).
func srtToVTT(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	out := &strings.Builder{}
	out.WriteString("WEBVTT\n\n")
	blank := true // sudah ada satu baris kosong setelah header
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "WEBVTT") {
			continue
		}
		if t == "" {
			if !blank {
				out.WriteString("\n")
				blank = true
			}
			continue
		}
		if strings.Contains(t, "-->") {
			out.WriteString(strings.ReplaceAll(t, ",", ".") + "\n")
			blank = false
			continue
		}
		// nomor urut cue SRT: baris angka tepat sebelum timestamp (bukan dialog)
		if _, err := strconv.Atoi(t); err == nil {
			j := i + 1
			for j < len(lines) && strings.TrimSpace(lines[j]) == "" {
				j++
			}
			if j < len(lines) && strings.Contains(lines[j], "-->") {
				continue
			}
		}
		out.WriteString(strings.TrimRight(line, " \t") + "\n")
		blank = false
	}
	return strings.TrimRight(out.String(), "\n") + "\n"
}

// cacheRoot: folder cache remux/subtitle untuk semua sesi.
func cacheRoot() string {
	if d := os.Getenv("ZANIME_CACHE_DIR"); d != "" {
		return d
	}
	return filepath.Join(tmpdir.Base(), "zanime-cache")
}

func (s *Session) cacheDir() string {
	return filepath.Join(cacheRoot(), s.sid)
}

func (s *Session) subCachePath(ref string) string {
	return filepath.Join(s.cacheDir(), "subs", ref+".vtt")
}

// cachedSubs: ref subtitle yang sudah selesai diekstrak (siap dipasang di player).
func (s *Session) cachedSubs() []string {
	ents, err := os.ReadDir(filepath.Join(s.cacheDir(), "subs"))
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(ents))
	for _, e := range ents {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".vtt") {
			continue
		}
		if st, err := e.Info(); err != nil || st.Size() <= 10 {
			continue
		}
		out = append(out, strings.TrimSuffix(name, ".vtt"))
	}
	sort.Strings(out)
	return out
}

// partialSubs: ref subtitle tertanam yang sedang ditulis pass video + ukurannya.
// Ukuran dipakai player sebagai versi URL supaya cue baru ikut termuat.
func (s *Session) partialSubs() map[string]int64 {
	ents, err := os.ReadDir(filepath.Join(s.cacheDir(), "subs"))
	if err != nil {
		return nil
	}
	out := map[string]int64{}
	for _, e := range ents {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".vtt.part") {
			continue
		}
		ref := strings.TrimSuffix(name, ".vtt.part")
		if _, err := os.Stat(s.subCachePath(ref)); err == nil {
			continue // sudah lengkap
		}
		if st, err := e.Info(); err == nil && st.Size() > 10 {
			out[ref] = st.Size()
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// partialSub: potongan subtitle yang sudah ditulis pass video, dipotong di cue
// terakhir yang utuh (data yang masih setengah ditulis tidak boleh dikirim).
func (s *Session) partialSub(ref string) ([]byte, bool) {
	data, err := os.ReadFile(s.subCachePath(ref) + ".part")
	if err != nil || len(data) <= 10 {
		return nil, false
	}
	txt := string(data)
	if !strings.HasPrefix(strings.TrimSpace(txt), "WEBVTT") {
		return nil, false
	}
	i := strings.LastIndex(txt, "\n\n")
	if i < 0 {
		return nil, false
	}
	vtt := srtToVTT(txt[:i+2])
	if len(vtt) <= 10 {
		return nil, false
	}
	return []byte(vtt), true
}

func (s *Session) saveSub(ref string, vtt []byte) {
	if len(vtt) <= 10 {
		return
	}
	path := s.subCachePath(ref)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	tmp := path + ".part"
	if err := os.WriteFile(tmp, vtt, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

// subExtract: ekstrak subtitle tertanam di latar belakang — tahan reload browser,
// dan tidak memblokir streaming video.
func (m *Manager) subExtract(s *Session, vIdx int, ref string) {
	s.subMu.Lock()
	if s.subJobs == nil {
		s.subJobs = map[string]bool{}
	}
	running := s.subJobs[ref]
	s.subMu.Unlock()
	if running {
		return
	}
	if _, err := os.Stat(s.subCachePath(ref)); err == nil {
		return
	}
	s.subMu.Lock()
	s.subJobs[ref] = true
	delete(s.subErr, ref)
	s.subMu.Unlock()

	fail := func(msg string) {
		s.subMu.Lock()
		if s.subErr == nil {
			s.subErr = map[string]string{}
		}
		s.subErr[ref] = msg
		s.subMu.Unlock()
	}

	go func() {
		defer func() {
			s.subMu.Lock()
			delete(s.subJobs, ref)
			s.subMu.Unlock()
		}()
		n, err := strconv.Atoi(ref[1:])
		if err != nil {
			fail("ref subtitle tidak valid")
			return
		}
		bin, err := ffmpeg.Path()
		if err != nil {
			fail(err.Error())
			return
		}
		files := s.t.Files()
		if vIdx < 0 || vIdx >= len(files) {
			fail("file video hilang")
			return
		}
		var data []byte
		if ref[0] == 'f' {
			if n < 0 || n >= len(files) {
				fail("file subtitle tidak ada di torrent")
				return
			}
			data, err = ffmpeg.ExtractToVTT(bin, files[n].NewReader(), "-i", "pipe:0")
		} else {
			data, err = ffmpeg.ExtractToVTT(bin, videoReader(files[vIdx]),
				"-i", "pipe:0", "-map", fmt.Sprintf("0:%d", n))
		}
		if err != nil {
			fail(err.Error())
			return
		}
		vtt := []byte(srtToVTT(string(data)))
		if len(vtt) <= 10 {
			fail("subtitle kosong")
			return
		}
		s.saveSub(ref, vtt)
	}()
}

// subErrors: ref subtitle yang gagal diekstrak + alasannya (ditampilkan di UI).
func (s *Session) subErrors() map[string]string {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	if len(s.subErr) == 0 {
		return nil
	}
	out := make(map[string]string, len(s.subErr))
	for k, v := range s.subErr {
		out[k] = v
	}
	return out
}

// smoothRate: peredam sampel kecepatan 100 ms supaya angka tidak berkedip.
func smoothRate(prev, cur float64) float64 {
	if prev <= 0 {
		return cur
	}
	return prev*0.6 + cur*0.4
}

// Prune: drop sesi yang lebih lama dari 30 menit tanpa dipakai.
func (m *Manager) Prune() {
	cut := time.Now().Add(-30 * time.Minute)
	m.mu.Lock()
	var gone []*Session
	for sid, s := range m.m {
		s.mu.Lock()
		idle := s.lastUsed.Before(cut) && !s.remuxRunning
		s.mu.Unlock()
		if idle {
			gone = append(gone, s)
			delete(m.m, sid)
		}
	}
	m.mu.Unlock()
	for _, s := range gone {
		_ = os.RemoveAll(s.cacheDir())
		s.t.Drop()
	}
}

// RunPruneLoop: bersih-bersih periodik; panggil sekali saat start.
func (m *Manager) RunPruneLoop(stop <-chan struct{}) {
	go func() {
		t := time.NewTicker(5 * time.Minute)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				m.Prune()
			case <-stop:
				return
			}
		}
	}()
}

func jsonBytes(v any) []byte { b, _ := json.Marshal(v); return b }

var _ = jsonBytes

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
