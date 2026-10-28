package memstorage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
)

const pieceLen = 1024

func openTorrent(t *testing.T, c *Client, pieces int) (*metainfo.Info, storage.TorrentImpl) {
	t.Helper()
	info := &metainfo.Info{
		Name:        "test",
		Length:      int64(pieces) * pieceLen,
		PieceLength: pieceLen,
		Pieces:      make([]byte, pieces*20),
	}
	ti, err := c.OpenTorrent(context.Background(), info, metainfo.Hash{})
	if err != nil {
		t.Fatalf("OpenTorrent: %v", err)
	}
	return info, ti
}

func writePiece(t *testing.T, ti storage.TorrentImpl, info *metainfo.Info, index int, fill byte) {
	t.Helper()
	mp := info.Piece(index)
	p := ti.Piece(mp)
	data := bytes.Repeat([]byte{fill}, int(mp.Length()))
	if _, err := p.WriteAt(data, 0); err != nil {
		t.Fatalf("write piece %d: %v", index, err)
	}
	if err := p.MarkComplete(); err != nil {
		t.Fatalf("mark piece %d complete: %v", index, err)
	}
}

func readPiece(ti storage.TorrentImpl, info *metainfo.Info, index int, n int) error {
	_, err := ti.Piece(info.Piece(index)).ReadAt(make([]byte, n), 0)
	return err
}

func waitEvicted(t *testing.T, c *Client) int {
	t.Helper()
	select {
	case ev := <-c.Evicted():
		return ev.Index
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for an eviction")
		return -1
	}
}

func TestCache_EvictsLeastRecentlyUsedPiece(t *testing.T) {
	c := New(3 * pieceLen)
	info, ti := openTorrent(t, c, 6)

	writePiece(t, ti, info, 0, 'a')
	writePiece(t, ti, info, 1, 'b')
	writePiece(t, ti, info, 2, 'c')
	if err := readPiece(ti, info, 0, 16); err != nil { // touch 0, so 1 becomes LRU
		t.Fatalf("read piece 0: %v", err)
	}
	writePiece(t, ti, info, 3, 'd')

	if got := waitEvicted(t, c); got != 1 {
		t.Fatalf("evicted piece %d, want 1", got)
	}
	if used := c.Used(); used > c.Limit() {
		t.Fatalf("cache used %d bytes, over limit %d", used, c.Limit())
	}
	if c.Evictions() != 1 {
		t.Fatalf("evictions = %d, want 1", c.Evictions())
	}
	if ti.Piece(info.Piece(1)).Completion().Complete {
		t.Fatal("evicted piece 1 is still reported complete")
	}
	for _, index := range []int{0, 2, 3} {
		if !ti.Piece(info.Piece(index)).Completion().Complete {
			t.Fatalf("piece %d should still be complete", index)
		}
	}
}

func TestCache_ReadAfterEvictionReturnsEOF(t *testing.T) {
	c := New(2 * pieceLen)
	info, ti := openTorrent(t, c, 4)

	writePiece(t, ti, info, 0, 'a')
	writePiece(t, ti, info, 1, 'b')
	writePiece(t, ti, info, 2, 'c')

	if got := waitEvicted(t, c); got != 0 {
		t.Fatalf("evicted piece %d, want 0", got)
	}
	if err := readPiece(ti, info, 0, 16); !errors.Is(err, io.EOF) {
		t.Fatalf("read evicted piece error = %v, want io.EOF", err)
	}
}

func TestCache_WriteAfterEvictionRecreatesPiece(t *testing.T) {
	c := New(pieceLen)
	info, ti := openTorrent(t, c, 2)

	writePiece(t, ti, info, 0, 'a')
	writePiece(t, ti, info, 1, 'b')
	if got := waitEvicted(t, c); got != 0 {
		t.Fatalf("evicted piece %d, want 0", got)
	}

	writePiece(t, ti, info, 0, 'c')
	if !ti.Piece(info.Piece(0)).Completion().Complete {
		t.Fatal("refetched piece 0 should be complete")
	}
	if used := c.Used(); used > c.Limit() {
		t.Fatalf("cache used %d bytes, over limit %d", used, c.Limit())
	}
}

func TestCache_MarkNotCompleteClearsCompletion(t *testing.T) {
	c := New(4 * pieceLen)
	info, ti := openTorrent(t, c, 2)
	writePiece(t, ti, info, 0, 'a')

	p := ti.Piece(info.Piece(0))
	if !p.Completion().Complete {
		t.Fatal("piece should be complete after MarkComplete")
	}
	if err := p.MarkNotComplete(); err != nil {
		t.Fatalf("MarkNotComplete: %v", err)
	}
	if p.Completion().Complete {
		t.Fatal("piece should be incomplete after MarkNotComplete")
	}
}

func TestCache_StoresPieceLargerThanLimit(t *testing.T) {
	c := New(pieceLen / 2)
	info, ti := openTorrent(t, c, 1)

	writePiece(t, ti, info, 0, 'a')

	if used := c.Used(); used != pieceLen {
		t.Fatalf("cache used = %d, want %d (single piece must fit)", used, pieceLen)
	}
	if !ti.Piece(info.Piece(0)).Completion().Complete {
		t.Fatal("piece should be complete")
	}
}

// Reserve/Release let the remux buffer share the piece cache budget: the
// buffer counts as used memory, so pieces are evicted or the job is refused.
func TestReserveSharesBudget(t *testing.T) {
	c := New(10)
	if !c.Reserve(6) {
		t.Fatal("Reserve(6) dengan limit 10 harus berhasil")
	}
	if used := c.Used(); used != 6 {
		t.Fatalf("used = %d, want 6", used)
	}
	if c.Reserve(6) {
		t.Fatal("Reserve(6) kedua harus gagal: hanya 4 byte tersisa")
	}
	c.Release(6)
	if used := c.Used(); used != 0 {
		t.Fatalf("used setelah Release = %d, want 0", used)
	}
	if !c.Reserve(10) {
		t.Fatal("Reserve(10) harus berhasil setelah budget dikembalikan")
	}
	c.Release(10)
}
