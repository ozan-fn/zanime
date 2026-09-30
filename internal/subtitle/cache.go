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
	v, ok := c.mem[key]
	c.mu.Unlock()
	if ok {
		return usable(v)
	}
	b, err := os.ReadFile(c.path(key))
	if err != nil {
		return "", false
	}
	c.mu.Lock()
	c.mem[key] = string(b)
	c.mu.Unlock()
	return usable(string(b))
}

// usable reports a cached file as a hit only when its script is the one the
// translation is written in. A file that came back in Arabic, Cyrillic or Han is
// not a usable translation, so it counts as a miss and /status starts the
// conversion again — a bad file left in the cache would otherwise be served
// forever, and a prompt fix would never show.
func usable(vtt string) (string, bool) {
	if mostlyNonLatin(vtt) {
		return "", false
	}
	return vtt, true
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
