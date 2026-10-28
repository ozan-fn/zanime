package main

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/json"
	"io"
	"net/http/httptest"
	"path"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"zanime/internal/memstorage"
)

// TestWSStatePayload memastikan frame /api/ws memuat semua field yang dibaca
// UI (index.html): files, selected, cache, peers/net, remux, loading.
func TestWSStatePayload(t *testing.T) {
	mem := memstorage.New(1 << 20)
	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = t.TempDir()
	cfg.DefaultStorage = mem
	cfg.NoDHT = true
	cfg.DisableTrackers = true
	cfg.DisablePEX = true
	cfg.NoDefaultPortForwarding = true
	cfg.ListenPort = 0
	cl, err := torrent.NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer cl.Close()

	srv := newServer(cl, mem, nil, nil, 0)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/ws", srv.apiWS)
	ts := httptest.NewServer(r)
	defer ts.Close()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+"/api/ws", nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("baca frame ws: %v", err)
	}

	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("frame bukan JSON: %v", err)
	}
	for _, key := range []string{"ready", "loading", "files", "selected", "cache", "net", "users", "server"} {
		if _, ok := state[key]; !ok {
			t.Fatalf("state tanpa %q: %v", key, state)
		}
	}
	netCounters, _ := state["net"].(map[string]any)
	for _, key := range []string{"from_peers", "to_client", "to_s3"} {
		if _, ok := netCounters[key]; !ok {
			t.Fatalf("state.net tanpa %q: %v", key, netCounters)
		}
	}
	cache, _ := state["cache"].(map[string]any)
	for _, key := range []string{"used", "limit", "evictions", "pieces"} {
		if _, ok := cache[key]; !ok {
			t.Fatalf("state.cache tanpa %q: %v", key, cache)
		}
	}
	srvInfo, _ := state["server"].(map[string]any)
	for _, key := range []string{"uptime", "goroutines", "heap", "sys", "jobs"} {
		if _, ok := srvInfo[key]; !ok {
			t.Fatalf("state.server tanpa %q: %v", key, srvInfo)
		}
	}
	users, _ := state["users"].(map[string]any)
	for _, key := range []string{"active", "tabs", "uploads", "upload_limit"} {
		if _, ok := users[key]; !ok {
			t.Fatalf("state.users tanpa %q: %v", key, users)
		}
	}
}

// Satu user maksimal 2 job: permintaan ketiga membatalkan yang tertua miliknya
// sendiri, tanpa mengusik user lain atau upload yang sudah selesai.
func TestCapUploadsPerUser(t *testing.T) {
	srv := newServer(nil, memstorage.New(1<<20), nil, nil, 0)
	newJob := func(owner string, at int64) (*s3Upload, context.Context) {
		ctx, cancel := context.WithCancel(context.Background())
		return &s3Upload{owner: owner, started: time.Unix(at, 0), done: make(chan struct{}), cancel: cancel}, ctx
	}

	first, firstCtx := newJob("a", 1)
	second, secondCtx := newJob("a", 2)
	other, otherCtx := newJob("b", 0)
	finished := make(chan struct{})
	close(finished)
	doneCtx, done := context.WithCancel(context.Background())

	srv.s3Jobs["1"] = first
	srv.s3Jobs["2"] = second
	srv.s3Jobs["3"] = other
	srv.s3Jobs["4"] = &s3Upload{owner: "a", started: time.Unix(0, 0), done: finished, cancel: done}

	srv.s3Mu.Lock()
	srv.capUploads("a")
	srv.s3Mu.Unlock()

	if firstCtx.Err() == nil {
		t.Error("upload tertua user a tidak dibatalkan")
	}
	if secondCtx.Err() != nil {
		t.Error("upload terbaru user a ikut dibatalkan")
	}
	if otherCtx.Err() != nil {
		t.Error("upload user lain ikut dibatalkan")
	}
	if doneCtx.Err() != nil {
		t.Error("upload yang sudah selesai tidak boleh dibatalkan")
	}
}

func TestCapUploadsLeavesSingleJobAlone(t *testing.T) {
	srv := newServer(nil, memstorage.New(1<<20), nil, nil, 0)
	ctx, cancel := context.WithCancel(context.Background())
	srv.s3Jobs["only"] = &s3Upload{owner: "a", started: time.Unix(1, 0), done: make(chan struct{}), cancel: cancel}

	srv.s3Mu.Lock()
	srv.capUploads("a")
	srv.s3Mu.Unlock()

	if ctx.Err() != nil {
		t.Error("satu-satunya upload tidak boleh dibatalkan")
	}
}

// Marker "Siap" diisi probe setelah restart, dan dibuang lagi kalau objeknya
// sudah tidak ada supaya bisa diunggah ulang.
// Ukuran buffer upload mengikuti cache RAM dibagi user yang sedang terhubung:
// 90 MiB untuk satu user, 45 MiB untuk dua, minimal 5 MiB (batas S3).
func TestUploadPartSizeFollowsCacheAndUsers(t *testing.T) {
	srv := newServer(nil, memstorage.New(90<<20), nil, nil, 0)
	if got := srv.uploadPartSize(); got != 90<<20 {
		t.Fatalf("1 user = %d, want %d", got, 90<<20)
	}

	srv.addWSUser("a")
	srv.addWSUser("a") // dua tab user yang sama tetap satu user
	if got := srv.uploadPartSize(); got != 90<<20 {
		t.Fatalf("2 tab 1 user = %d, want %d", got, 90<<20)
	}

	srv.addWSUser("b")
	if got := srv.uploadPartSize(); got != 45<<20 {
		t.Fatalf("2 user = %d, want %d", got, 45<<20)
	}

	srv.removeWSUser("b")
	if got := srv.uploadPartSize(); got != 90<<20 {
		t.Fatalf("setelah user b pergi = %d, want %d", got, 90<<20)
	}

	small := newServer(nil, memstorage.New(1<<20), nil, nil, 0)
	if got := small.uploadPartSize(); got != 5<<20 {
		t.Fatalf("cache kecil = %d, want batas minimum %d", got, 5<<20)
	}
}

func TestMarkReadyAndDropMarker(t *testing.T) {
	srv := newServer(nil, memstorage.New(1<<20), nil, nil, 0)

	srv.markReady("zanime/hash/ep01.mkv", 123)
	job := srv.s3Job("zanime/hash/ep01.mkv")
	if job == nil || cacheStateOf(job).State != "ready" {
		t.Fatalf("marker = %+v, want ready", job)
	}

	srv.markReady("zanime/hash/ep01.mkv", 123) // tidak boleh menimpa
	if job2 := srv.s3Job("zanime/hash/ep01.mkv"); job2 != job {
		t.Error("markReady menimpa job yang sudah ada")
	}

	srv.dropMarker("zanime/hash/ep01.mkv")
	if srv.s3Job("zanime/hash/ep01.mkv") != nil {
		t.Error("marker selesai tidak dibuang")
	}

	inflight := &s3Upload{done: make(chan struct{}), cancel: func() {}}
	srv.s3Mu.Lock()
	srv.s3Jobs["zanime/hash/ep02.mkv"] = inflight
	srv.s3Mu.Unlock()
	srv.dropMarker("zanime/hash/ep02.mkv")
	if srv.s3Job("zanime/hash/ep02.mkv") == nil {
		t.Error("upload yang masih jalan tidak boleh dihapus")
	}
}

func TestVideoFilesSortedByName(t *testing.T) {
	info := metainfo.Info{
		Name:        "pack",
		PieceLength: 1 << 20,
		Pieces:      make([]byte, 20), // one piece; contents are never read
		Files: []metainfo.FileInfo{
			{Path: []string{"ep02.mkv"}, Length: 1},
			{Path: []string{"ep10.mkv"}, Length: 1},
			{Path: []string{"ep01.mkv"}, Length: 1},
			{Path: []string{"notes.txt"}, Length: 1},
		},
	}
	infoBytes, err := bencode.Marshal(info)
	if err != nil {
		t.Fatalf("encode info: %v", err)
	}

	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = t.TempDir()
	cfg.DefaultStorage = memstorage.New(1 << 20)
	cfg.NoDHT = true
	cfg.DisableTrackers = true
	cfg.DisablePEX = true
	cfg.NoDefaultPortForwarding = true
	cfg.ListenPort = 0
	cl, err := torrent.NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer cl.Close()

	tor, _, err := cl.AddTorrentSpec(torrent.TorrentSpecFromMetaInfo(&metainfo.MetaInfo{InfoBytes: infoBytes}))
	if err != nil {
		t.Fatalf("AddTorrentSpec: %v", err)
	}
	<-tor.GotInfo()

	var got []string
	for _, f := range videoFiles(tor.Files()) {
		got = append(got, path.Base(f.Path()))
	}
	want := []string{"ep01.mkv", "ep02.mkv", "ep10.mkv"}
	if !slices.Equal(got, want) {
		t.Fatalf("videoFiles order = %v, want %v", got, want)
	}
}

// TestPurgeReaderDropsReadPieces memastikan piece dilepas tepat setelah
// byte-nya dibaca untuk S3, dan piece yang sedang dibaca tetap di RAM.
func TestPurgeReaderDropsReadPieces(t *testing.T) {
	var dropped [][2]int
	p := &purgeReader{
		r: bytes.NewReader(make([]byte, 10)), n: new(atomic.Int64),
		drop:     func(from, to int) { dropped = append(dropped, [2]int{from, to}) },
		pieceLen: 4, next: 0, end: 3,
	}
	if _, err := io.ReadFull(p, make([]byte, 6)); err != nil {
		t.Fatalf("read 6: %v", err)
	}
	if !slices.Equal(dropped, [][2]int{{0, 0}}) {
		t.Fatalf("dropped after 6 bytes = %v, want [[0 0]]", dropped)
	}
	if _, err := p.Read(make([]byte, 4)); err != nil {
		t.Fatalf("read 4: %v", err)
	}
	if !slices.Equal(dropped, [][2]int{{0, 0}, {1, 1}}) {
		t.Fatalf("dropped = %v, want piece 1 released next", dropped)
	}
	if p.next != 2 {
		t.Fatalf("next = %d, want 2 (piece 2 holds unread bytes)", p.next)
	}
}

// TestPurgeReaderDropsWholeFile memastikan file yang pas kelipatan piece
// melepas piece terakhirnya juga.
func TestPurgeReaderDropsWholeFile(t *testing.T) {
	var dropped [][2]int
	p := &purgeReader{
		r: bytes.NewReader(make([]byte, 8)), n: new(atomic.Int64),
		drop:     func(from, to int) { dropped = append(dropped, [2]int{from, to}) },
		pieceLen: 4, next: 0, end: 2,
	}
	if _, err := io.Copy(io.Discard, p); err != nil {
		t.Fatalf("copy: %v", err)
	}
	if !slices.Equal(dropped, [][2]int{{0, 1}}) {
		t.Fatalf("dropped = %v, want [[0 1]]", dropped)
	}
}

// TestPurgeAfterReadFreesRealTorrent memastikan pembungkus upload benar-benar
// melepas semua piece torrent setelah file terbaca utuh, tanpa merusak byte.
func TestPurgeAfterReadFreesRealTorrent(t *testing.T) {
	const pieceLen = 1024
	data := make([]byte, 3*pieceLen)
	for i := range data {
		data[i] = byte(i*7 + i/5)
	}
	info := &metainfo.Info{Name: "ep.mkv", Length: int64(len(data)), PieceLength: pieceLen,
		Pieces: make([]byte, 0, 60)}
	for i := 0; i < 3; i++ {
		sum := sha1.Sum(data[i*pieceLen : (i+1)*pieceLen])
		info.Pieces = append(info.Pieces, sum[:]...)
	}
	infoBytes, err := bencode.Marshal(info)
	if err != nil {
		t.Fatalf("encode info: %v", err)
	}
	hash := metainfo.HashBytes(infoBytes)

	mem := memstorage.New(int64(len(data)))
	pieceStore, err := mem.OpenTorrent(context.Background(), info, hash)
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	for i := 0; i < 3; i++ {
		p := pieceStore.Piece(info.Piece(i))
		if _, err := p.WriteAt(data[i*pieceLen:(i+1)*pieceLen], 0); err != nil {
			t.Fatalf("write piece %d: %v", i, err)
		}
		if err := p.MarkComplete(); err != nil {
			t.Fatalf("complete piece %d: %v", i, err)
		}
	}

	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = t.TempDir()
	cfg.DefaultStorage = mem
	cfg.NoDHT = true
	cfg.DisableTrackers = true
	cfg.DisablePEX = true
	cfg.NoDefaultPortForwarding = true
	cfg.ListenPort = 0
	cl, err := torrent.NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer cl.Close()
	tor, _, err := cl.AddTorrentSpec(torrent.TorrentSpecFromMetaInfo(&metainfo.MetaInfo{InfoBytes: infoBytes}))
	if err != nil {
		t.Fatalf("AddTorrentSpec: %v", err)
	}
	<-tor.GotInfo()
	if err := tor.VerifyDataContext(context.Background()); err != nil {
		t.Fatalf("verify: %v", err)
	}

	if complete, partial, busy := pieceCounts(tor); complete != 3 || partial != 0 || busy != 0 {
		t.Fatalf("pieceCounts = %d/%d/%d, want 3/0/0", complete, partial, busy)
	}

	f := tor.Files()[0]
	srv := newServer(cl, mem, nil, nil, 0)
	srv.mu.Lock()
	srv.t, srv.files, srv.selected = tor, []*torrent.File{f}, 0
	srv.mu.Unlock()
	if detail, _ := srv.state()["torrent"].(gin.H); detail["complete_pieces"] != 3 || detail["pieces"] != 3 {
		t.Fatalf("state torrent = %v, want 3/3 piece lengkap", detail)
	}

	reader := f.NewReader()
	defer func() { _ = reader.Close() }()
	var progress atomic.Int64
	got, err := io.ReadAll(srv.purgeAfterRead(f, reader, &progress))
	if err != nil {
		t.Fatalf("read upload stream: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("upload stream bytes differ from torrent data")
	}
	if progress.Load() != int64(len(data)) {
		t.Fatalf("progress = %d, want %d", progress.Load(), len(data))
	}
	if used := mem.Used(); used != 0 {
		t.Fatalf("cache used after upload = %d, want 0", used)
	}
	for i := 0; i < tor.NumPieces(); i++ {
		if tor.Piece(i).State().Complete {
			t.Fatalf("piece %d should be marked incomplete after purge", i)
		}
	}
	if detail, _ := srv.state()["torrent"].(gin.H); detail["complete_pieces"] != 0 {
		t.Fatalf("state torrent = %v, want no complete pieces after purge", detail)
	}
}
