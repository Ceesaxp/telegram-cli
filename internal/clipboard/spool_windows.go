//go:build windows

package clipboard

import (
	"os"
	"time"
)

// isStaleSpoolDir reports whether a spool directory is old enough to be
// considered abandoned. Windows has no portable, dependency-free way to
// check whether an arbitrary pid still belongs to a live process (it would
// need OpenProcess via syscall/golang.org/x/sys), so instead we fall back to
// a pure age check: anything untouched for longer than staleSpoolAge is
// swept, regardless of whether its name carries a recognizable pid. A real
// long-running session is protected from this by spool()/spoolPath()
// refreshing the directory's mtime on every paste.
func isStaleSpoolDir(pid int, pidOK bool, info os.FileInfo) bool {
	return time.Since(info.ModTime()) > staleSpoolAge
}

// isSafeExistingSpoolDir reports whether the deterministic spool path at
// path is safe to reuse rather than treat as a possible attack. Windows
// access control is ACL/SID-based rather than unix's simple owner-uid model,
// and comparing the owning SID would need extra syscalls beyond the standard
// library; NTFS also doesn't let an unprivileged user plant an arbitrary
// symlink in a shared temp directory the way a unix symlink attack would, so
// there is no equivalent ownership check here. We still confirm the path is
// a genuine directory and not a reparse point (Go reports a symlink reparse
// point via ModeSymlink) — enough to catch the planted-symlink attack this
// exists to prevent, if not a full ownership check.
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
