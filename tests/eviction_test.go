package tests

import (
	"context"
	"crypto/sha1"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
	"github.com/gin-gonic/gin"

	"zanime/internal/memstorage"
	"zanime/internal/stream"
)

// Hermetic eviction test: a seeder and a leecher run in-process over
// localhost, so the whole 8 MiB "torrent" is virtual and finishes in
// milliseconds. The leecher caches only 2 pieces, forcing real LRU evictions
// and refetches through the HTTP range path.

const (
	virtualPieceLen = 1 << 20
	virtualPieces   = 8
)

func TestMemStorage_EvictsAndRefetchesUnderVirtualTorrent(t *testing.T) {
	data := virtualData(virtualPieces * virtualPieceLen)
	meta := buildMetaInfo(t, data)

	seedClient, seedDir := newVirtualClient(t, staticStorage{data: data}, true)
	if _, _, err := seedClient.AddTorrentSpec(torrent.TorrentSpecFromMetaInfo(meta)); err != nil {
		t.Fatalf("seed AddTorrentSpec: %v", err)
	}

	mem := memstorage.New(2 * virtualPieceLen)
	leechClient, leechDir := newVirtualClient(t, mem, false)
	leechTorrent, _, err := leechClient.AddTorrentSpec(torrent.TorrentSpecFromMetaInfo(meta))
	if err != nil {
		t.Fatalf("leech AddTorrentSpec: %v", err)
	}
	go func() {
		for ev := range mem.Evicted() {
			if ev.Index >= 0 && ev.Index < leechTorrent.NumPieces() {
				leechTorrent.Piece(ev.Index).UpdateCompletion()
			}
		}
	}()
	leechTorrent.AddClientPeer(seedClient)
	<-leechTorrent.GotInfo()

	srv := httptest.NewServer(virtualHTTPHandler(leechTorrent))
	defer srv.Close()

	for i := 0; i < virtualPieces; i++ {
		offset := int64(i*virtualPieceLen + 17)
		if got := readVirtualByte(t, srv.URL, offset); got != data[offset] {
			t.Fatalf("piece %d byte = %#x, want %#x", i, got, data[offset])
		}
	}

	if mem.Evictions() == 0 {
		t.Fatal("expected evictions with a 2-piece cache for an 8-piece torrent")
	}
	if used := mem.Used(); used > mem.Limit() {
		t.Fatalf("cache used %d bytes, over limit %d", used, mem.Limit())
	}

	// Piece 0 was evicted long ago: reading it again must refetch, not fail.
	if got := readVirtualByte(t, srv.URL, 17); got != data[17] {
		t.Fatalf("refetched byte = %#x, want %#x", got, data[17])
	}

	assertDirEmpty(t, seedDir)
	assertDirEmpty(t, leechDir)
}

func virtualData(n int) []byte {
	data := make([]byte, n)
	for i := range data {
		data[i] = byte(i*31 + i/7)
	}
	return data
}

func buildMetaInfo(t *testing.T, data []byte) *metainfo.MetaInfo {
	t.Helper()
	count := (len(data) + virtualPieceLen - 1) / virtualPieceLen
	info := metainfo.Info{
		Name:        "virtual.bin",
		Length:      int64(len(data)),
		PieceLength: virtualPieceLen,
		Pieces:      make([]byte, 0, count*20),
	}
	for i := 0; i < count; i++ {
		start := i * virtualPieceLen
		end := min(start+virtualPieceLen, len(data))
		sum := sha1.Sum(data[start:end])
		info.Pieces = append(info.Pieces, sum[:]...)
	}
	infoBytes, err := bencode.Marshal(info)
	if err != nil {
		t.Fatalf("encode info: %v", err)
	}
	return &metainfo.MetaInfo{InfoBytes: infoBytes}
}

func newVirtualClient(t *testing.T, st storage.ClientImpl, seed bool) (*torrent.Client, string) {
	t.Helper()
	dataDir := t.TempDir()
	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = dataDir // must stay empty: the test fails if anything lands here
	cfg.DefaultStorage = st
	cfg.NoDHT = true
	cfg.DisableTrackers = true
	cfg.DisablePEX = true
	cfg.NoDefaultPortForwarding = true
	cfg.Seed = seed
	cfg.ListenPort = 0
	cl, err := torrent.NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { cl.Close() })
	return cl, dataDir
}

func virtualHTTPHandler(tr *torrent.Torrent) http.Handler {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	handler := func(c *gin.Context) {
		f := tr.Files()[0]
		reader := f.NewReader()
		defer func() { _ = reader.Close() }()
		reader.SetReadahead(virtualPieceLen)
		stream.Serve(c, f.DisplayPath(), reader)
	}
	r.GET("/stream", handler)
	r.HEAD("/stream", handler)
	return r
}

func readVirtualByte(t *testing.T, base string, offset int64) byte {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		b, err := tryReadVirtualByte(base, offset)
		if err == nil {
			return b
		}
		lastErr = err
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("offset %d not served in time: %v", offset, lastErr)
	return 0
}

func tryReadVirtualByte(base string, offset int64) (byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/stream", nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", offset, offset))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}
	if resp.StatusCode != http.StatusPartialContent || len(body) != 1 {
		return 0, fmt.Errorf("status %d, %d body bytes", resp.StatusCode, len(body))
	}
	return body[0], nil
}

func assertDirEmpty(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	for _, entry := range entries {
		t.Fatalf("torrent wrote %q under %s", entry.Name(), filepath.Clean(dir))
	}
}

// staticStorage is a read-only, RAM-backed torrent storage used by the seeder.
type staticStorage struct {
	data []byte
}

func (s staticStorage) OpenTorrent(context.Context, *metainfo.Info, metainfo.Hash) (storage.TorrentImpl, error) {
	return storage.TorrentImpl{
		Piece: func(p metainfo.Piece) storage.PieceImpl {
			return staticPiece{data: s.data[p.Offset() : p.Offset()+p.Length()]}
		},
		Close: func() error { return nil },
	}, nil
}

type staticPiece struct {
	data []byte
}

func (p staticPiece) ReadAt(b []byte, off int64) (int, error) {
	if off >= int64(len(p.data)) {
		return 0, io.EOF
	}
	n := copy(b, p.data[off:])
	if n < len(b) {
		return n, io.EOF
	}
	return n, nil
}

func (staticPiece) WriteAt([]byte, int64) (int, error) { return 0, fmt.Errorf("read-only") }
func (staticPiece) MarkComplete() error                { return nil }
func (staticPiece) MarkNotComplete() error             { return nil }
func (staticPiece) Completion() storage.Completion {
	return storage.Completion{Ok: true, Complete: true}
}
