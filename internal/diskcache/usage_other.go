//go:build !unix

package diskcache

import "io/fs"

func diskUsage(info fs.FileInfo) int64 { return info.Size() }
