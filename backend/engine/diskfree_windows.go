package engine

import (
	"math"

	"golang.org/x/sys/windows"
)

// diskFreeBytes returns the bytes available on the volume containing path
// for the calling user (Windows GetDiskFreeSpaceEx's "FreeBytesAvailable",
// which honors per-user quotas). Returns -1 on failure — callers treat <0
// as "unknown, don't gate on it" rather than failing the operation.
func diskFreeBytes(path string) int64 {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return -1
	}
	var freeAvail, totalBytes, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(p, &freeAvail, &totalBytes, &totalFree); err != nil {
		return -1
	}
	// freeAvail is a uint64; int64(freeAvail) would truncate/wrap a value
	// above MaxInt64 into a negative ("unknown") result. Saturate instead.
	if freeAvail > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(freeAvail)
}
