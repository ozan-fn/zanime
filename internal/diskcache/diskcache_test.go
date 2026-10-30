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
		Pieces:      make([]byte, 20*3), // tiga piece; isinya tidak dibaca
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

// Satu piece disimpan utuh di file-nya sendiri (p/<index>), bukan dicicil ke
// file episode: tulis lalu baca balik, dan file piece-nya persis sepanjang
// isinya (tidak sparse sebesar episode).
func TestPieceWriteReadRoundTrip(t *testing.T) {
	_, dir := openTorrent(t, t.TempDir(), 1<<20, "a")

	p := &piece{torrent: &torrentImpl{dir: dir, info: fakeInfo(), files: fakeInfo().UpvertedFiles()},
		p: fakePiece{offset: 4, length: 4}}
	if _, err := p.WriteAt([]byte("ijkl"), 0); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	if err := p.MarkComplete(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, pieceDirName, "1"))
	if err != nil {
		t.Fatalf("file piece: %v", err)
	}
	if string(raw) != "ijkl" {
		t.Fatalf("isi file piece = %q, want ijkl", raw)
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
	if removed < 60 {
		t.Fatalf("removed = %d, want at least file size 60", removed)
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

// Piece yang sudah diverifikasi tetap dianggap lengkap setelah client dibuka
// ulang (restart): sidecar .complete yang menyimpannya, bukan rehash.
func TestCompletionSurvivesReopen(t *testing.T) {
	root := t.TempDir()
	info := fakeInfo()
	hash := hashOf("restart")

	c, err := New(root, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	impl, err := c.OpenTorrent(context.Background(), info, hash)
	if err != nil {
		t.Fatal(err)
	}
	p0 := impl.Piece(info.Piece(0))
	if _, err := p0.WriteAt([]byte("abcd"), 0); err != nil {
		t.Fatal(err)
	}
	if err := p0.MarkComplete(); err != nil {
		t.Fatal(err)
	}

	again, err := New(root, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	impl2, err := again.OpenTorrent(context.Background(), info, hash)
	if err != nil {
		t.Fatal(err)
	}
	if got := impl2.Piece(info.Piece(0)).Completion(); !got.Ok || !got.Complete {
		t.Fatalf("piece 0 setelah reopen = %+v, want complete", got)
	}
	if got := impl2.Piece(info.Piece(1)).Completion(); !got.Ok || got.Complete {
		t.Fatalf("piece 1 setelah reopen = %+v, want incomplete", got)
	}

	// MarkNotComplete menghapus bit, jadi open berikutnya juga menganggapnya
	// belum lengkap (mis. hash gagal setelah data lama dipakai).
	if err := impl2.Piece(info.Piece(0)).MarkNotComplete(); err != nil {
		t.Fatal(err)
	}
	third, err := New(root, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	impl3, err := third.OpenTorrent(context.Background(), info, hash)
	if err != nil {
		t.Fatal(err)
	}
	if got := impl3.Piece(info.Piece(0)).Completion(); got.Complete {
		t.Fatalf("piece 0 setelah MarkNotComplete = %+v", got)
	}
}

// Season pack: pindah episode membuang piece yang tak menyentuh episode
// terpilih; piece yang berbatasan dengannya tetap (memuat byte-nya).
func TestDropOtherFilesKeepsSelected(t *testing.T) {
	root := t.TempDir()
	c, err := New(root, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	hash := hashOf("pack")
	if _, err := c.OpenTorrent(context.Background(), fakeInfo(), hash); err != nil {
		t.Fatal(err)
	}
	tr := c.torrents[hash.String()]
	for _, span := range []fakePiece{{0, 4}, {4, 4}, {8, 4}} {
		p := &piece{torrent: tr, p: span}
		if _, err := p.WriteAt(make([]byte, span.length), 0); err != nil {
			t.Fatalf("WriteAt %+v: %v", span, err)
		}
		if err := p.MarkComplete(); err != nil {
			t.Fatal(err)
		}
	}
	piece0 := filepath.Join(root, hash.String(), pieceDirName, "0")
	piece2 := filepath.Join(root, hash.String(), pieceDirName, "2")

	// ep01 = [0,6): piece 0 dan 1 menyentuhnya, piece 2 (ep02) dibuang.
	removed, pieces := c.DropOtherFiles(hash, 0)
	if removed < 4 {
		t.Fatalf("removed = %d, want at least piece size 4", removed)
	}
	if _, err := os.Stat(piece0); err != nil {
		t.Fatalf("piece episode terpilih ikut terhapus: %v", err)
	}
	if _, err := os.Stat(piece2); !os.IsNotExist(err) {
		t.Fatalf("piece episode lama masih ada: %v", err)
	}
	if len(pieces) != 1 || pieces[0] != 2 {
		t.Fatalf("pieces = %v, want [2]", pieces)
	}
	if !tr.isComplete(0) || !tr.isComplete(4) || tr.isComplete(8) {
		t.Fatalf("completion setelah drop = %v", tr.complete)
	}
}

// File sparse tidak boleh dihitung sebesar ukuran logisnya: tail/Cues di akhir
// file 3 GiB hanya menulis beberapa KiB, jangan sampai cache terlihat 3 GiB.
func TestDirSizeCountsAllocatedBlocks(t *testing.T) {
	root := t.TempDir()
	f, err := os.Create(filepath.Join(root, "movie.mkv"))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(3 << 30); err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt([]byte("tail"), 3<<30-4); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if got := dirSize(root); got >= 1<<20 {
		t.Fatalf("dirSize = %d, want < 1 MiB untuk sparse file", got)
	}
}

// Torrent aktif juga dibatasi per piece: file yang sedang diputar tidak boleh
// membengkak sampai ber-gigabyte; piece lama di belakang playhead dipotong.
func TestPruneEvictsOldActivePieces(t *testing.T) {
	root := t.TempDir()
	info := &metainfo.Info{
		Name:        "show",
		PieceLength: 8192,
		Pieces:      make([]byte, 20*3),
		Files:       []metainfo.FileInfo{{Path: []string{"ep01.mkv"}, Length: 3 * 8192}},
	}
	c, err := New(root, 24<<10)
	if err != nil {
		t.Fatal(err)
	}
	hash := hashOf("active-pieces")
	impl, err := c.OpenTorrent(context.Background(), info, hash)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		p := impl.Piece(info.Piece(i))
		if _, err := p.WriteAt(make([]byte, 8192), 0); err != nil {
			t.Fatalf("WriteAt %d: %v", i, err)
		}
		if err := p.MarkComplete(); err != nil {
			t.Fatal(err)
		}
	}
	tr := c.torrents[hash.String()]
	tr.mu.Lock()
	tr.lastUse[0], tr.lastUse[8192], tr.lastUse[16384] = 1, 2, 3
	tr.mu.Unlock()
	c.SetActive(hash)
	var evicted []int
	c.SetPieceRefresh(func(_ string, pieces []int) { evicted = append(evicted, pieces...) })

	if removed := c.Prune(); removed < 4096 {
		t.Fatalf("removed = %d, want allocated blocks freed", removed)
	}
	if len(evicted) != 1 || evicted[0] != 0 {
		t.Fatalf("evicted = %v, want [0]", evicted)
	}
	if tr.isComplete(0) || !tr.isComplete(8192) || !tr.isComplete(16384) {
		t.Fatalf("completion = %v, want piece 0 dropped and 1/2 kept", tr.complete)
	}
}
