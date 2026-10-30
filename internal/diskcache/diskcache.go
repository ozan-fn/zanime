// Package diskcache is a torrent storage backend that keeps each downloaded
// piece in its own small file.
//
// Layout: a torrent gets its own directory under the root (its info hash, or
// "metainfo-<n>" before the hash is known), and inside it one file per piece
// under p/, named by the piece index. A piece file holds at most one piece
// (usually a few MiB), so nothing here is a sparse file the size of an episode:
// the cache's size is the bytes actually downloaded, and eviction is a plain
// file delete that works on any filesystem - no hole punching required.
//
// The budget is enforced by an LRU over directories, not pieces: when the cache
// exceeds the limit, whole torrents are deleted least recently used first. The
// active torrent is never evicted, but its unselected pieces are dropped when
// the viewer switches episodes, so a season pack does not keep every watched
// episode on disk. The order is persisted in root/lru.json so it survives a
// restart.
package diskcache

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
)

const registryName = "lru.json"

// completionName is the per-torrent sidecar bitset of verified pieces. It lives
// inside the torrent directory, so DropAll and LRU eviction remove it together
// with the cached bytes.
const completionName = ".complete"

// pieceDirName is the subdirectory holding one file per piece.
const pieceDirName = "p"

// activeCacheLimit keeps one playing torrent from filling a large global
// budget. Pieces behind the playhead are cheap to fetch again; a 4 GiB cache
// limit should not become 4 GiB of one episode.
const activeCacheLimit = 512 << 20

// Client is a disk-backed storage.ClientImpl.
type Client struct {
	root  string
	limit int64

	mu       sync.Mutex
	active   string // dir torrent yang sedang diputar: tidak pernah di-evict
	order    []string
	torrents map[string]*torrentImpl
	pruned   int64 // torrent dir yang sudah dihapus karena budget
	lastSave time.Time

	// refresh tells the live torrent client that the listed pieces are no
	// longer on disk. It runs while the torrent's data lock is held, before a
	// reader can observe the missing file.
	refresh func(dir string, pieces []int)
}

// New returns a client storing pieces under root, keeping the cache at or below
// limit bytes by dropping the least recently used torrent directories.
//
// A cache is a cache, not a library: every zanime torrent is reachable from its
// magnet and is fetched again on demand, so deleting a directory only costs a
// re-download. A file placed in root by hand is not a registered torrent and is
// never deleted.
func New(root string, limit int64) (*Client, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	c := &Client{root: root, limit: limit, torrents: make(map[string]*torrentImpl)}
	c.load()
	return c, nil
}

func (c *Client) Root() string { return c.root }
func (c *Client) Limit() int64 { return c.limit }
func (c *Client) Used() int64  { return dirSize(c.root) }

// Pruned is how many torrent directories the budget has deleted.
func (c *Client) Pruned() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pruned
}

// UsedByTorrent is the size of one torrent's cache directory.
func (c *Client) UsedByTorrent(hash metainfo.Hash) int64 {
	return dirSize(filepath.Join(c.root, hash.String()))
}

// SetActive names the torrent playback is using: eviction skips its directory
// but may still drop old pieces inside it.
func (c *Client) SetActive(hash metainfo.Hash) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.active = hash.String()
}

// SetPieceRefresh installs the callback used after an active-torrent piece is
// removed. The callback must only refresh completion bits; it must not call
// back into Client or it will deadlock.
func (c *Client) SetPieceRefresh(fn func(dir string, pieces []int)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.refresh = fn
}

// DropOtherFiles removes every cached piece of a torrent that does not touch
// the file at keepOffset, so a season pack keeps only the episode being watched
// on disk. Pieces that straddle the kept file's boundary are kept too - they
// hold its bytes. The removed piece indexes are returned; the live torrent must
// call Piece(i).UpdateCompletion() for each one, or it will keep believing the
// deleted bytes are still there.
func (c *Client) DropOtherFiles(hash metainfo.Hash, keepOffset int64) (removed int64, pieces []int) {
	c.mu.Lock()
	t := c.torrents[hash.String()]
	c.mu.Unlock()
	if t == nil {
		return 0, nil
	}
	return t.dropOtherFiles(keepOffset)
}

// DropAll removes one torrent's cache directory, e.g. when the user switches to
// a new magnet. A torrent still open keeps its files open: the bytes go to the
// unlinked inode until the torrent is dropped.
func (c *Client) DropAll(hash metainfo.Hash) {
	dir := filepath.Join(c.root, hash.String())
	if _, err := os.Stat(dir); err == nil {
		c.mu.Lock()
		c.pruned++ // folder memang terhapus dari disk: ikut dihitung evict
		c.order = slices.DeleteFunc(c.order, func(d string) bool { return d == hash.String() })
		delete(c.torrents, hash.String())
		c.saveLocked()
		c.mu.Unlock()
	}
	_ = os.RemoveAll(dir)
}

// Prune enforces the budget: the least recently used directories are deleted
// until the cache fits. The active torrent is always kept.
func (c *Client) Prune() (removed int64) {
	c.mu.Lock()
	sizes := make(map[string]int64, len(c.order))
	for _, dir := range c.order {
		sizes[dir] = dirSize(filepath.Join(c.root, dir))
	}
	used := func() (n int64) {
		for _, size := range sizes {
			n += size
		}
		return n
	}
	for used() > c.limit {
		i := slices.IndexFunc(reverse(c.order), func(dir string) bool { return dir != c.active })
		if i < 0 {
			break // hanya torrent aktif yang tersisa
		}
		i = len(c.order) - 1 - i
		dir := c.order[i]
		removed += sizes[dir]
		delete(sizes, dir)
		c.pruned++
		_ = os.RemoveAll(filepath.Join(c.root, dir))
		delete(c.torrents, dir)
		c.order = slices.Delete(c.order, i, i+1)
	}
	// Even the active torrent is not allowed to grow without bound. Keep the
	// few most recently used pieces (the playhead and readahead) and delete the
	// older ones. Do this after releasing c.mu: refreshing the live client's
	// completion takes the client lock, and that lock may be held while the
	// client calls back into OpenTorrent.
	var activeTorrent *torrentImpl
	activeDir := c.active
	activeTarget := c.limit
	if activeTarget > activeCacheLimit {
		activeTarget = activeCacheLimit
	}
	if c.Used() > activeTarget {
		activeTorrent = c.torrents[c.active]
	}
	refresh := c.refresh
	c.saveLocked()
	c.mu.Unlock()

	if activeTorrent != nil {
		pieces, freed := activeTorrent.evictTo(activeTarget)
		removed += freed
		if len(pieces) > 0 && refresh != nil {
			refresh(activeDir, pieces)
		}
	}
	return removed
}

// touch marks dir as the most recently used. Caller holds mu.
func (c *Client) touch(dir string) {
	c.order = slices.DeleteFunc(c.order, func(d string) bool { return d == dir })
	c.order = append([]string{dir}, c.order...)
	if time.Since(c.lastSave) > time.Second {
		c.saveLocked()
	}
}

// load reads the persisted LRU order and forgets directories that are gone.
func (c *Client) load() {
	data, err := os.ReadFile(filepath.Join(c.root, registryName))
	if err != nil {
		return
	}
	var dirs []string
	known := map[string]bool{}
	if json.Unmarshal(data, &dirs) == nil {
		for _, dir := range dirs {
			if st, err := os.Stat(filepath.Join(c.root, dir)); err == nil && st.IsDir() {
				c.order = append(c.order, dir)
				known[dir] = true
			}
		}
	}
	// Folder cache yang tidak tercatat (registry hilang/rusak) tetap ikut
	// dihitung dan boleh dihapus, jadi ikut masuk urutan paling lama.
	entries, _ := os.ReadDir(c.root)
	for _, entry := range entries {
		if entry.IsDir() && !known[entry.Name()] {
			c.order = append(c.order, entry.Name())
		}
	}
}

// saveLocked writes the registry through a temp file, so a crash never leaves
// half a JSON document behind. Caller holds mu.
func (c *Client) saveLocked() {
	c.lastSave = time.Now()
	data, err := json.Marshal(c.order)
	if err != nil {
		return
	}
	tmp := filepath.Join(c.root, registryName+".tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, filepath.Join(c.root, registryName))
}

// dirSize sums the disk space actually allocated below dir. A piece file is
// small and non-sparse once complete, but a half-written one may still have
// holes, so allocated blocks are the honest measure.
func dirSize(dir string) int64 {
	var total int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			total += diskUsage(info)
		}
		return nil
	})
	return total
}

// reverse iterates in reverse order through a helper, so Prune can look for the
// LRU entry that is not the active torrent.
func reverse(dirs []string) []string {
	out := make([]string, len(dirs))
	for i, d := range dirs {
		out[len(dirs)-1-i] = d
	}
	return out
}

func (c *Client) OpenTorrent(_ context.Context, info *metainfo.Info, infoHash metainfo.Hash) (storage.TorrentImpl, error) {
	name := infoHash.String()
	if name == (metainfo.Hash{}).String() {
		name = "metainfo-"
	}
	dir := filepath.Join(c.root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return storage.TorrentImpl{}, err
	}
	t := &torrentImpl{dir: dir, info: info, files: info.UpvertedFiles(),
		complete: map[int64]bool{}, lastUse: map[int64]int64{}}
	t.loadCompletion()
	c.mu.Lock()
	c.torrents[name] = t
	c.touch(name)
	c.mu.Unlock()

	return storage.TorrentImpl{
		Piece: func(p metainfo.Piece) storage.PieceImpl {
			return &piece{torrent: t, p: p}
		},
		Close: func() error { return nil },
	}, nil
}

type torrentImpl struct {
	dir   string
	info  *metainfo.Info
	files []metainfo.FileInfo

	dataMu sync.RWMutex // file reads/writes vs. removing a piece

	mu       sync.Mutex
	complete map[int64]bool  // piece complete, seperti PieceCompletion upstream (in-memory)
	lastUse  map[int64]int64 // piece offset -> UnixNano, untuk eviction per piece
}

func (t *torrentImpl) setComplete(off int64, ok bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.complete == nil {
		t.complete = map[int64]bool{}
	}
	t.complete[off] = ok
	t.persistLocked(off, ok)
}

// touch records that a piece was just read or written. Eviction uses this to
// keep the playhead and readahead, and drop the tail behind it.
func (t *torrentImpl) touch(off int64) {
	t.mu.Lock()
	if t.lastUse == nil {
		t.lastUse = map[int64]int64{}
	}
	t.lastUse[off] = time.Now().UnixNano()
	t.mu.Unlock()
}

// piecePath is where one piece's bytes live under the torrent directory.
func (t *torrentImpl) piecePath(index int) string {
	return filepath.Join(t.dir, pieceDirName, strconv.Itoa(index))
}

// pieceOffset converts a piece index to its byte offset in the torrent.
func (t *torrentImpl) pieceOffset(index int) int64 {
	return int64(index) * t.info.PieceLength
}

// fileAt returns the torrent file whose offset is off.
func (t *torrentImpl) fileAt(off int64) (metainfo.FileInfo, bool) {
	for _, fi := range t.files {
		if fi.TorrentOffset == off {
			return fi, true
		}
	}
	return metainfo.FileInfo{}, false
}

// evictTo deletes old complete pieces until the whole cache fits the budget.
// dataMu serializes the delete against reads and writes; the caller refreshes
// the client's completion bits afterwards.
func (t *torrentImpl) evictTo(limit int64) (pieces []int, freed int64) {
	if t.info == nil || t.info.PieceLength <= 0 {
		return nil, 0
	}
	candidates := t.evictionOrder()
	if len(candidates) == 0 {
		return nil, 0
	}
	t.dataMu.Lock()
	defer t.dataMu.Unlock()
	size := dirSize(t.dir)
	for _, pi := range candidates {
		if size <= limit {
			break
		}
		if !t.isComplete(t.pieceOffset(pi)) {
			continue
		}
		n, ok := t.removePieceLocked(pi)
		if !ok {
			continue
		}
		pieces = append(pieces, pi)
		freed += n
		size -= n
	}
	return pieces, freed
}

// evictionOrder returns complete piece indexes oldest-access first, keeping the
// two most recent pieces (the current read and its immediate readahead) hot.
func (t *torrentImpl) evictionOrder() []int {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.info == nil || t.info.PieceLength <= 0 {
		return nil
	}
	type cand struct {
		pi  int
		use int64
	}
	var all []cand
	for i := 0; i < t.info.NumPieces(); i++ {
		off := t.pieceOffset(i)
		if !t.complete[off] {
			continue
		}
		all = append(all, cand{pi: i, use: t.lastUse[off]})
	}
	slices.SortFunc(all, func(a, b cand) int {
		if a.use != b.use {
			if a.use < b.use {
				return -1
			}
			return 1
		}
		return a.pi - b.pi
	})
	if len(all) <= 2 {
		return nil
	}
	out := make([]int, 0, len(all)-2)
	for _, c := range all[:len(all)-2] {
		out = append(out, c.pi)
	}
	return out
}

// removePieceLocked deletes one piece's file and clears its completion bit. It
// reports the bytes freed (false when there was no file and no bit to clear).
// Caller holds dataMu for writing.
func (t *torrentImpl) removePieceLocked(pi int) (int64, bool) {
	off := t.pieceOffset(pi)
	info, err := os.Stat(t.piecePath(pi))
	if err != nil {
		if !t.isComplete(off) {
			return 0, false
		}
		t.setComplete(off, false)
		return 0, true
	}
	size := diskUsage(info)
	if err := os.Remove(t.piecePath(pi)); err != nil {
		return 0, false
	}
	t.setComplete(off, false)
	return size, true
}

// dropOtherFiles deletes every cached piece that does not hold bytes of the
// file at keepOffset. Pieces straddling the kept file's boundary are kept.
func (t *torrentImpl) dropOtherFiles(keepOffset int64) (removed int64, pieces []int) {
	if t.info == nil || t.info.PieceLength <= 0 {
		return 0, nil
	}
	keep, ok := t.fileAt(keepOffset)
	if !ok {
		return 0, nil
	}
	keepStart := keep.TorrentOffset
	keepEnd := keep.TorrentOffset + keep.Length

	t.dataMu.Lock()
	defer t.dataMu.Unlock()
	total := t.info.TotalLength()
	plen := t.info.PieceLength
	for pi := 0; pi < t.info.NumPieces(); pi++ {
		start := t.pieceOffset(pi)
		end := start + plen
		if end > total {
			end = total
		}
		if start < keepEnd && end > keepStart {
			continue // this piece holds bytes of the kept file
		}
		if n, ok := t.removePieceLocked(pi); ok {
			removed += n
			pieces = append(pieces, pi)
		}
	}
	return removed, pieces
}

// completionPath is the sidecar bitset for this torrent.
func (t *torrentImpl) completionPath() string {
	return filepath.Join(t.dir, completionName)
}

// loadCompletion marks pieces recorded as verified by an earlier run, but only
// when their piece file is actually still on disk. A missing or short sidecar
// simply leaves the rest incomplete; the bytes are then fetched again instead
// of being trusted.
func (t *torrentImpl) loadCompletion() {
	if t.info == nil || t.info.PieceLength <= 0 {
		return
	}
	data, err := os.ReadFile(t.completionPath())
	if err != nil {
		return
	}
	n := t.info.NumPieces()
	for i := 0; i < n; i++ {
		if i/8 >= len(data) {
			break
		}
		if data[i/8]&(1<<(uint(i)%8)) == 0 {
			continue
		}
		if _, err := os.Stat(t.piecePath(i)); err == nil {
			t.complete[t.pieceOffset(i)] = true
		}
	}
}

// persistLocked writes one bit of the sidecar. The bitmap is small (one bit per
// piece), so a one-byte WriteAt per verified piece is cheap and survives a
// restart without rehashing; a crash between the data write and this bit can
// only cost one re-download, never a false "complete".
func (t *torrentImpl) persistLocked(off int64, ok bool) {
	if t.info == nil || t.info.PieceLength <= 0 {
		return
	}
	i := off / t.info.PieceLength
	if i < 0 || int(i) >= t.info.NumPieces() {
		return
	}
	path := t.completionPath()
	if !ok {
		if _, err := os.Stat(path); err != nil {
			return // tidak ada yang perlu dibersihkan
		}
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	byteOff := i / 8
	bit := byte(1 << (uint(i) % 8))
	var buf [1]byte
	if _, err := f.ReadAt(buf[:], byteOff); err != nil && !errors.Is(err, io.EOF) {
		return
	}
	if ok {
		buf[0] |= bit
	} else {
		buf[0] &^= bit
	}
	_, _ = f.WriteAt(buf[:], byteOff)
}

func (t *torrentImpl) isComplete(off int64) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.complete[off]
}

// piece is one torrent piece, stored whole in its own file.
type piece struct {
	torrent *torrentImpl
	p       pieceSpan
}

// pieceSpan is what the backend needs from a torrent piece: where it sits and
// how long it is. That is all the file mapping uses, so a test can drive it
// without a live torrent.
type pieceSpan interface {
	Offset() int64
	Length() int64
}

// index is the piece's index in the torrent, the name of its file.
func (p *piece) index() int {
	if p.torrent.info == nil || p.torrent.info.PieceLength <= 0 {
		return 0
	}
	return int(p.p.Offset() / p.torrent.info.PieceLength)
}

func (p *piece) ReadAt(b []byte, off int64) (int, error) {
	p.torrent.dataMu.RLock()
	defer p.torrent.dataMu.RUnlock()
	f, err := os.Open(p.torrent.piecePath(p.index()))
	if err != nil {
		return 0, err
	}
	n, err := f.ReadAt(b, off)
	_ = f.Close()
	if n > 0 {
		p.torrent.touch(p.p.Offset())
	}
	return n, err
}

func (p *piece) WriteAt(b []byte, off int64) (int, error) {
	p.torrent.dataMu.RLock()
	defer p.torrent.dataMu.RUnlock()
	path := p.torrent.piecePath(p.index())
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return 0, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return 0, err
	}
	n, err := f.WriteAt(b, off)
	_ = f.Close()
	if err != nil {
		return n, err
	}
	p.torrent.touch(p.p.Offset())
	return n, nil
}

// MarkComplete and MarkNotComplete are called by the torrent client setelah
// memverifikasi hash piece. Status disimpan di bitmap .complete dalam folder
// torrent, jadi cache yang sama tetap dianggap lengkap setelah restart: byte di
// disk dipakai ulang, bukan diunduh lagi.
func (p *piece) MarkComplete() error {
	p.torrent.setComplete(p.p.Offset(), true)
	p.torrent.touch(p.p.Offset())
	return nil
}

func (p *piece) MarkNotComplete() error {
	p.torrent.setComplete(p.p.Offset(), false)
	return nil
}

func (p *piece) Completion() storage.Completion {
	return storage.Completion{Complete: p.torrent.isComplete(p.p.Offset()), Ok: true}
}
