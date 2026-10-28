package main

import (
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

	srv := newServer(cl, mem, nil, 0)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/ws", srv.apiWS)
	ts := httptest.NewServer(r)
	defer ts.Close()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+"/api/ws", nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	defer conn.Close()
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
