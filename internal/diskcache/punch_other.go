//go:build !linux

package diskcache

// punchHole is only implemented for Linux; other platforms keep whole-file
// eviction instead of risking a zero-filled range that still occupies blocks.
func punchHole(string, int64, int64) (bool, error) { return false, nil }
