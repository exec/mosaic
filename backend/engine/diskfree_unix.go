//go:build !windows

package engine

import "golang.org/x/sys/unix"

// diskFreeBytes returns the bytes available on the filesystem containing path,
// from the perspective of the calling user (i.e. excluding root-reserved
// blocks). Returns -1 if the call fails — callers treat <0 as "unknown,
// don't gate on it" rather than failing the operation.
func diskFreeBytes(path string) int64 {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return -1
	}
	// Bavail × Bsize gives the user-available bytes. Bfree includes
	// root-reserved blocks and would over-report.
	return int64(stat.Bavail) * int64(stat.Bsize)
}
