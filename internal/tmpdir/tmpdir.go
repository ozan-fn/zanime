// Package tmpdir: folder kerja sementara.
// Di container (mis. Pterodactyl) /tmp tidak bisa ditulis, cepat penuh, dan
// hilang saat restart — sedangkan folder kerja server tetap. Jadi ./tmp
// didahulukan: ZANIME_TMP_DIR → ./tmp → os.TempDir().
package tmpdir

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	once sync.Once
	base string
)

// Base: folder sementara yang bisa ditulis.
func Base() string {
	if d := strings.TrimSpace(os.Getenv("ZANIME_TMP_DIR")); d != "" {
		return d // eksplisit dari user — hormati apa adanya
	}
	once.Do(func() { base = resolve() })
	return base
}

func resolve() string {
	if local, err := filepath.Abs("tmp"); err == nil && writable(local) {
		return local // folder kerja (volume server) → data tetap ada setelah restart
	}
	return os.TempDir() // cwd read-only: pakai temp sistem
}

// writable: folder bisa dibuat dan ditulisi file.
func writable(dir string) bool {
	if dir == "" {
		return false
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false
	}
	f, err := os.CreateTemp(dir, ".probe-*")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return true
}
