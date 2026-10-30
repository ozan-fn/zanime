//go:build unix

package diskcache

import (
	"io/fs"
	"syscall"
)

// diskUsage returns allocated blocks, not the sparse file's logical length.
func diskUsage(info fs.FileInfo) int64 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return int64(st.Blocks) * 512
	}
	return info.Size()
}
