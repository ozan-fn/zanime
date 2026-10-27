package main

import (
	"path"
	"slices"
	"testing"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"

	"zanime/internal/memstorage"
)

func TestBrowserURL(t *testing.T) {
	cases := map[string]string{
		"0.0.0.0:8080":   "http://localhost:8080",
		":8080":          "http://localhost:8080",
		"[::]:8080":      "http://localhost:8080",
		"127.0.0.1:9000": "http://127.0.0.1:9000",
		"[::1]:9000":     "http://[::1]:9000",
		"tanpa-port":     "tanpa-port",
	}
	for addr, want := range cases {
		if got := browserURL(addr); got != want {
			t.Errorf("browserURL(%q) = %q, want %q", addr, got, want)
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
