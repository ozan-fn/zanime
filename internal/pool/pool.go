// Package pool menyediakan satu worker pool global (alitto/pond v2) untuk
// semua pekerjaan paralel: resolve server embedmaster dan terjemahan cue.
// Batas worker dari WORKERS, default 20.
package pool

import (
	"sync"

	"github.com/alitto/pond/v2"

	"zanime/internal/config"
)

var (
	once sync.Once
	p    pond.Pool
	size int
)

// Get mengembalikan pool global. Dibuat saat pertama dipakai supaya WORKERS
// dari .env (dibaca setelah init paket) ikut terbaca.
func Get() pond.Pool {
	once.Do(func() {
		size = int(config.Int("WORKERS", 20))
		p = pond.NewPool(size)
	})
	return p
}

// Workers = batas worker pool (dipakai log/test).
func Workers() int {
	Get()
	return size
}
