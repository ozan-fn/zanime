package tests

import (
	"context"
	"crypto/sha1"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
	"github.com/gin-gonic/gin"

	"zanime/internal/diskcache"
	"zanime/internal/stream"
)

// Hermetic disk cache test: a seeder and a leecher run in-process over localhost,
// so the whole 8 MiB "torrent" is virtual and finishes in milliseconds. The
// leecher's storage is diskcache, so every piece is written to a real file and
// read back through the HTTP range path.

const (
	virtualPieceLen = 1 << 16
	virtualPieces   = 8
)

func TestDiskCache_ServesWholeVirtualTorrentFromDisk(t *testing.T) {
	data := virtualData(virtualPieces * virtualPieceLen)
	meta := buildMetaInfo(t, data)

	seedClient, _ := newVirtualClient(t, staticStorage{data: data}, true)
	if _, _, err := seedClient.AddTorrentSpec(torrent.TorrentSpecFromMetaInfo(meta)); err != nil {
		t.Fatalf("seed AddTorrentSpec: %v", err)
	}

	cacheRoot := t.TempDir()
	cache, err := diskcache.New(cacheRoot, virtualPieces*virtualPieceLen)
	if err != nil {
		t.Fatalf("diskcache: %v", err)
	}
	leechClient, _ := newVirtualClient(t, cache, false)
	leechTorrent, _, err := leechClient.AddTorrentSpec(torrent.TorrentSpecFromMetaInfo(meta))
	if err != nil {
		t.Fatalf("leech AddTorrentSpec: %v", err)
	}
	leechTorrent.AddClientPeer(seedClient)
	<-leechTorrent.GotInfo()
	t.Logf("info: %+v", leechTorrent.Info())

	// Debug: baca byte lewat torrent reader langsung, sebelum lewat HTTP.
	tryRead(t, leechTorrent, 17, "byte 17", cacheRoot)
	tryRead(t, leechTorrent, 16*virtualPieceLen, "byte last piece", cacheRoot)

	srv := httptest.NewServer(virtualHTTPHandler(leechTorrent))
	defer srv.Close()

	// Setiap piece lewat HTTP Range, jadi pembacaan mundur juga harus jalan.
	for _, i := range []int{0, 3, 7, 1, 5} {
		offset := int64(i*virtualPieceLen + 17)
		if got := readVirtualByte(t, srv.URL, offset); got != data[offset] {
			t.Fatalf("piece %d byte = %#x, want %#x", i, got, data[offset])
		}
	}

	if used := cache.Used(); used == 0 {
		t.Fatal("tidak ada byte yang tertulis ke cache disk")
	}
	if cache.Pruned() != 0 {
		t.Fatalf("cache di dalam budget tapi di-prune %d kali", cache.Pruned())
	}
}

func tryRead(t *testing.T, tr *torrent.Torrent, off int64, label string, dirs ...string) {
	t.Helper()
	dr := tr.NewReader()
	defer func() { _ = dr.Close() }()
	if _, err := dr.Seek(off, io.SeekStart); err != nil {
		t.Fatalf("seek: %v", err)
	}
	okc := make(chan byte, 1)
	go func() {
		var b [1]byte
		n, _ := io.ReadFull(dr, b[:])
		if n == 1 {
			okc <- b[0]
		}
	}()
	select {
	case b := <-okc:
		t.Logf("%s: %d", label, b)
	case <-time.After(3 * time.Second):
		matches, _ := filepath.Glob(filepath.Join(dirs[0], "*"))
		t.Logf("cache dir files: %v", matches)
		t.Logf("%s: timeout; complete=%v", label, func() []bool {
			out := make([]bool, tr.NumPieces())
			for i := range out {
				out[i] = tr.Piece(i).State().Complete
			}
			return out
		}())
	}
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
	t.Cleanup(func() { _ = cl.Close() })
	return cl, dataDir
}

func virtualHTTPHandler(tr *torrent.Torrent) http.Handler {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	handler := func(c *gin.Context) {
		f := tr.Files()[0]
		reader := f.NewReader()
		defer func() { _ = reader.Close() }()
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
