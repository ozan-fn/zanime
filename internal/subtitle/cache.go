// Disk cache for finished subtitle translations.
package subtitle

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sync"
)

// SubtitleCache keeps finished translations on disk so a rewatch never pays for
// the same episode twice. A cache miss on every play would re-translate the whole
// file on each seek.
type SubtitleCache struct {
	dir string
	mu  sync.Mutex
	mem map[string]string
}

func NewSubtitleCache(dir string) *SubtitleCache {
	os.MkdirAll(dir, 0o755)
	return &SubtitleCache{dir: dir, mem: map[string]string{}}
}

func (c *SubtitleCache) path(key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(c.dir, hex.EncodeToString(sum[:])+".vtt")
}

func (c *SubtitleCache) Get(key string) (string, bool) {
	c.mu.Lock()
	if v, ok := c.mem[key]; ok {
		c.mu.Unlock()
		return v, true
	}
	c.mu.Unlock()
	b, err := os.ReadFile(c.path(key))
	if err != nil {
		return "", false
	}
	c.mu.Lock()
	c.mem[key] = string(b)
	c.mu.Unlock()
	return string(b), true
}

func (c *SubtitleCache) Put(key, vtt string) {
	c.mu.Lock()
	c.mem[key] = vtt
	c.mu.Unlock()
	tmp := c.path(key) + ".tmp"
	if os.WriteFile(tmp, []byte(vtt), 0o644) == nil {
		os.Rename(tmp, c.path(key))
	}
}
