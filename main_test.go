package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path"
	"slices"
	"strings"
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
	for _, key := range []string{"ready", "loading", "files", "selected", "cache", "net"} {
		if _, ok := state[key]; !ok {
			t.Fatalf("state tanpa %q: %v", key, state)
		}
	}
	netCounters, _ := state["net"].(map[string]any)
	for _, key := range []string{"from_peers", "to_client"} {
		if _, ok := netCounters[key]; !ok {
			t.Fatalf("state.net tanpa %q: %v", key, netCounters)
		}
	}
	cache, _ := state["cache"].(map[string]any)
	for _, key := range []string{"used", "limit", "evictions"} {
		if _, ok := cache[key]; !ok {
			t.Fatalf("state.cache tanpa %q: %v", key, cache)
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
