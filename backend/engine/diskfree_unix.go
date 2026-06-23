//go:build !windows

package engine

import (
	"math"

	"golang.org/x/sys/unix"
)

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
	// root-reserved blocks and would over-report. Compute in uint64 and
	// saturate to MaxInt64 so a huge/odd filesystem can't overflow or wrap
	// the int64 result into a negative ("unknown") value.
	avail := uint64(stat.Bavail)
	bsize := uint64(stat.Bsize)
	if bsize == 0 {
		return -1
	}
	if avail > math.MaxInt64/bsize {
		return math.MaxInt64
	}
	return int64(avail * bsize)
}
