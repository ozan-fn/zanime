//go:build linux

package diskcache

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

const (
	fallocKeepSize  = 0x01
	fallocPunchHole = 0x02
)

// punchHole frees the blocks covering [off, off+n) without changing the file's
// logical length. The range is rounded inward to filesystem blocks: a partial
// block at either edge may contain bytes of the neighbouring piece, so it is
// left alone. A missing file counts as freed because it has no data to keep.
func punchHole(path string, off, n int64) (bool, error) {
	if n <= 0 {
		return false, nil
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()

	block := int64(4096)
	var st syscall.Statfs_t
	if err := syscall.Statfs(filepath.Dir(path), &st); err == nil && st.Bsize > 0 {
		block = int64(st.Bsize)
	}
	start := (off + block - 1) / block * block
	end := (off + n) / block * block
	if end <= start {
		return false, nil
	}
	err = syscall.Fallocate(int(f.Fd()), fallocKeepSize|fallocPunchHole, start, end-start)
	if err != nil {
		return false, err
	}
	return true, nil
}
