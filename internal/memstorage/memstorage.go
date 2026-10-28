// Package memstorage is a torrent storage backend that keeps pieces in RAM.
// It never touches the disk.
//
// The cache is a single LRU budget shared by every torrent opened by a Client.
// When the budget is exceeded the least recently used pieces are dropped and
// their index is announced on Evicted, so the torrent client can mark them
// incomplete and fetch them again when a reader seeks back.
package memstorage

import (
	"container/list"
	"context"
	"io"
	"sync"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
)

var (
	_ storage.ClientImpl = (*Client)(nil)
	_ storage.PieceImpl  = piece{}
)

// Eviction identifies one piece dropped from the RAM cache.
type Eviction struct {
	Hash  metainfo.Hash
	Index int
}

// Client is a zero-disk storage.ClientImpl.
type Client struct {
	limit int64
	cache *cache
}

// New returns a client whose piece data never exceeds limit bytes in RAM.
// A piece larger than limit is still stored (one piece must fit to make
// progress); the budget then temporarily exceeds limit by that piece.
func New(limit int64) *Client {
	return &Client{
		limit: limit,
		cache: &cache{
			limit:   limit,
			entries: make(map[key]*list.Element),
			evicted: make(chan Eviction, 1024),
		},
	}
}

func (c *Client) Limit() int64             { return c.limit }
func (c *Client) Used() int64              { return c.cache.usage() }
func (c *Client) Evictions() int64         { return c.cache.evictionCount() }
func (c *Client) Evicted() <-chan Eviction { return c.cache.evicted }

// Reserve takes n bytes out of the same budget the piece cache uses, dropping
// least recently used pieces to make room. It reports false when the budget
// cannot fit n bytes even after eviction, so callers can cancel instead of
// going over the memory limit.
func (c *Client) Reserve(n int64) bool {
	if n <= 0 {
		return true
	}
	c.cache.mu.Lock()
	evicted := c.cache.makeRoom(n)
	ok := c.cache.used+n <= c.cache.limit
	if ok {
		c.cache.used += n
	}
	c.cache.mu.Unlock()
	c.cache.report(evicted)
	return ok
}

// Release gives n bytes reserved with Reserve back to the budget.
func (c *Client) Release(n int64) {
	if n <= 0 {
		return
	}
	c.cache.mu.Lock()
	c.cache.used -= n
	if c.cache.used < 0 {
		c.cache.used = 0
	}
	c.cache.mu.Unlock()
}

func (c *Client) OpenTorrent(_ context.Context, _ *metainfo.Info, infoHash metainfo.Hash) (storage.TorrentImpl, error) {
	return storage.TorrentImpl{
		Piece: func(p metainfo.Piece) storage.PieceImpl {
			return piece{
				cache:  c.cache,
				key:    key{hash: infoHash, index: p.Index()},
				length: p.Length(),
			}
		},
		Close: func() error { return nil },
	}, nil
}

type key struct {
	hash  metainfo.Hash
	index int
}

type entry struct {
	key      key
	data     []byte
	complete bool
}

type cache struct {
	mu        sync.Mutex
	limit     int64
	used      int64
	evictions int64
	lru       list.List
	entries   map[key]*list.Element
	evicted   chan Eviction
}

func (c *cache) usage() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.used
}

func (c *cache) evictionCount() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.evictions
}

func (c *cache) read(k key, b []byte, off int64) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	el := c.entries[k]
	if el == nil {
		return 0, io.EOF
	}
	c.lru.MoveToFront(el)
	e := el.Value.(*entry)
	if off >= int64(len(e.data)) {
		return 0, io.EOF
	}
	n := copy(b, e.data[off:])
	if n < len(b) {
		return n, io.EOF
	}
	return n, nil
}

func (c *cache) write(k key, length int64, b []byte, off int64) (int, error) {
	if off < 0 || off+int64(len(b)) > length {
		return 0, io.ErrShortWrite
	}

	var evicted []key
	c.mu.Lock()
	el := c.entries[k]
	if el == nil {
		evicted = c.makeRoom(length)
		e := &entry{key: k, data: make([]byte, length)}
		el = c.lru.PushFront(e)
		c.entries[k] = el
		c.used += length
	}
	c.lru.MoveToFront(el)
	e := el.Value.(*entry)
	n := copy(e.data[off:], b)
	e.complete = false
	c.mu.Unlock()

	c.report(evicted)
	return n, nil
}

// makeRoom drops LRU entries until need bytes fit in the budget. Caller holds mu.
func (c *cache) makeRoom(need int64) (evicted []key) {
	for c.used+need > c.limit && c.lru.Len() > 0 {
		el := c.lru.Back()
		e := el.Value.(*entry)
		c.lru.Remove(el)
		delete(c.entries, e.key)
		c.used -= int64(len(e.data))
		c.evictions++
		evicted = append(evicted, e.key)
	}
	return evicted
}

func (c *cache) report(evicted []key) {
	for _, k := range evicted {
		c.evicted <- Eviction{Hash: k.hash, Index: k.index}
	}
}

func (c *cache) setComplete(k key, complete bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el := c.entries[k]; el != nil {
		el.Value.(*entry).complete = complete
	}
}

func (c *cache) completion(k key) storage.Completion {
	c.mu.Lock()
	defer c.mu.Unlock()
	el := c.entries[k]
	return storage.Completion{Ok: true, Complete: el != nil && el.Value.(*entry).complete}
}

type piece struct {
	cache  *cache
	key    key
	length int64
}

func (p piece) ReadAt(b []byte, off int64) (int, error) {
	return p.cache.read(p.key, b, off)
}

func (p piece) WriteAt(b []byte, off int64) (int, error) {
	return p.cache.write(p.key, p.length, b, off)
}

func (p piece) MarkComplete() error {
	p.cache.setComplete(p.key, true)
	return nil
}

func (p piece) MarkNotComplete() error {
	p.cache.setComplete(p.key, false)
	return nil
}

func (p piece) Completion() storage.Completion {
	return p.cache.completion(p.key)
}
