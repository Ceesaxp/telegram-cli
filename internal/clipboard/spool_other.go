//go:build !unix && !windows

package clipboard

import (
	"os"
	"time"
)

// isStaleSpoolDir lets the package build on platforms that are neither unix
// (covered by spool_unix.go) nor windows (spool_windows.go) — e.g. js,
// plan9, wasip1. spool() still runs here even though readImage always fails
// on these platforms (see clipboard_other.go), so a real implementation is
// needed; it falls back to the same age-based check used on Windows, since
// there's no portable liveness check available.
func isStaleSpoolDir(pid int, pidOK bool, info os.FileInfo) bool {
	return time.Since(info.ModTime()) > staleSpoolAge
}

// isSafeExistingSpoolDir has no ownership model to check on these platforms;
// confirming the path is still a genuine directory, and not a symlink, is
// the best available defense against a pre-planted attack path.
func isSafeExistingSpoolDir(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	return info.IsDir()
}
