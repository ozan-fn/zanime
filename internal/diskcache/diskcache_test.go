package diskcache

import (
	"context"
	"crypto/sha1"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anacrolix/torrent/metainfo"
)

func hashOf(s string) metainfo.Hash {
	return metainfo.Hash(sha1.Sum([]byte(s)))
}

// fakePiece is the part of metainfo.Piece the storage backend uses.
type fakePiece struct {
	offset, length int64
}

func (f fakePiece) Offset() int64 { return f.offset }
func (f fakePiece) Length() int64 { return f.length }

// fakeInfo mimics what the torrent client hands to storage: UpvertedFiles and
// TorrentOffset set, but unexported state (piece count) left alone.
func fakeInfo() *metainfo.Info {
	return &metainfo.Info{
		Name:        "show",
		PieceLength: 4,
		Files: []metainfo.FileInfo{
			{Path: []string{"ep01.mkv"}, Length: 6, TorrentOffset: 0},
			{Path: []string{"ep02.mkv"}, Length: 6, TorrentOffset: 6},
		},
	}
}

// openTorrent returns the client plus the open torrent's cache directory.
func openTorrent(t *testing.T, root string, limit int64, hash string) (*Client, string) {
	t.Helper()
	c, err := New(root, limit)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.OpenTorrent(context.Background(), fakeInfo(), hashOf(hash)); err != nil {
		t.Fatal(err)
	}
	return c, filepath.Join(root, hashOf(hash).String())
}

// Piece besar menembus batas file: ditulis ke ekor ep01 dan awal ep02 (file
// kedua tidak dibuat utuh, hanya ekornya), lalu dibaca balik utuh.
func TestPieceSpansFiles(t *testing.T) {
	_, dir := openTorrent(t, t.TempDir(), 1<<20, "a")

	p := &piece{torrent: &torrentImpl{dir: dir, info: fakeInfo(), files: fakeInfo().UpvertedFiles()},
		p: fakePiece{offset: 4, length: 4}}
	if _, err := p.WriteAt([]byte("ijkl"), 0); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	ep01, err := os.Open(filepath.Join(dir, "show", "ep01.mkv"))
	if err != nil {
		t.Fatal(err)
	}
	tail := make([]byte, 2)
	if _, err := ep01.ReadAt(tail, 4); err != nil {
		t.Fatalf("ep01 tail: %v", err)
	}
	_ = ep01.Close()
	ep02, err := os.Open(filepath.Join(dir, "show", "ep02.mkv"))
	if err != nil {
		t.Fatal(err)
	}
	head := make([]byte, 8)
	n, _ := ep02.ReadAt(head, 0)
	_ = ep02.Close()
	if string(tail)+string(head[:n]) != "ijkl" {
		t.Fatalf("isi disk = %q%q, want ijkl", tail, head[:n])
	}

	back := make([]byte, 4)
	if _, err := p.ReadAt(back, 0); err != nil {
		t.Fatalf("ReadAt: %v", err)
	}
	if string(back) != "ijkl" {
		t.Fatalf("read = %q", back)
	}
}

func TestDropAllRemovesTorrentDir(t *testing.T) {
	root := t.TempDir()
	c, err := New(root, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	hash := hashOf("a")
	if _, err := c.OpenTorrent(context.Background(), fakeInfo(), hash); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, hash.String(), "show"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, hash.String(), "show", "ep01.mkv"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	c.DropAll(hash)
	if _, err := os.Stat(filepath.Join(root, hash.String())); !os.IsNotExist(err) {
		t.Fatalf("folder torrent masih ada: %v", err)
	}
}

// Budget penuh: torrent paling lama dihapus, torrent aktif tidak pernah.
func TestPruneKeepsActiveAndDropsOldest(t *testing.T) {
	root := t.TempDir()
	c, err := New(root, 100)
	if err != nil {
		t.Fatal(err)
	}
	mk := func(name string) metainfo.Hash {
		hash := hashOf(name)
		if _, err := c.OpenTorrent(context.Background(), fakeInfo(), hash); err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(root, hash.String(), "show")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".bin"), make([]byte, 60), 0o644); err != nil {
			t.Fatal(err)
		}
		return hash
	}
	oldest := mk("old")
	newest := mk("new")
	c.SetActive(oldest)

	removed := c.Prune()
	if removed != 60 {
		t.Fatalf("removed = %d, want 60", removed)
	}
	if _, err := os.Stat(filepath.Join(root, newest.String())); !os.IsNotExist(err) {
		t.Fatal("torrent terbaru harus yang dihapus (LRU), bukan yang aktif")
	}
	if _, err := os.Stat(filepath.Join(root, oldest.String())); err != nil {
		t.Fatalf("torrent aktif ikut dihapus: %v", err)
	}
	if c.Pruned() != 1 {
		t.Fatalf("Pruned() = %d, want 1", c.Pruned())
	}
}

func TestRegistrySurvivesRestart(t *testing.T) {
	root := t.TempDir()
	c, err := New(root, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	hash := hashOf("a")
	if _, err := c.OpenTorrent(context.Background(), fakeInfo(), hash); err != nil {
		t.Fatal(err)
	}
	again, err := New(root, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.order) != 1 || again.order[0] != hash.String() {
		t.Fatalf("registry setelah restart = %v", again.order)
	}
	data, err := os.ReadFile(filepath.Join(root, registryName))
	if err != nil || !strings.Contains(string(data), hash.String()) {
		t.Fatalf("file registry = %q (%v)", data, err)
	}
}
