//go:build unix

package clipboard

import (
	"os"
	"syscall"
	"time"
)

// isStaleSpoolDir decides whether a sibling spool directory is safe to
// reclaim. The rule is layered so that an unreliable or absent liveness
// signal never causes a premature delete:
//
//   - A directory touched within staleSpoolAge is never removed, regardless
//     of what its name looks like or what the pid check below says. This is
//     the hard safety floor: it protects an in-flight upload even if the
//     liveness check is ever wrong, and gives a just-abandoned process's
//     directory a grace period.
//   - Past that grace period, a directory whose name doesn't carry a
//     parseable pid (e.g. a random-suffix os.MkdirTemp fallback, ours or an
//     older build's) is reclaimed — an old, untouched directory like that is
//     never legitimately in use.
//   - Past the grace period, a directory whose pid is confirmed dead
//     (syscall.Kill returns ESRCH) is reclaimed.
//   - Past the grace period, a directory whose pid still belongs to a
//     running process is *also* reclaimed: on a long-lived machine, pids get
//     reused, so "the pid exists" stops being good evidence that it's still
//     the same process once the directory has gone untouched for two days
//     straight. A real long-running session is protected from this by
//     spool()/spoolPath() refreshing the directory's mtime on every paste.
func isStaleSpoolDir(pid int, pidOK bool, info os.FileInfo) bool {
	// (a) Hard floor: never reclaim a directory touched recently, no matter
	// what its name looks like or what the liveness check below says. This
	// protects an in-flight upload even if isProcessAlive is ever wrong on
	// some platform quirk, and gives a just-abandoned directory a grace
	// period.
	if time.Since(info.ModTime()) <= staleSpoolAge {
		return false
	}
	if !pidOK {
		// (b) No name-based signal at all (e.g. a random-suffix
		// os.MkdirTemp fallback, ours or an older build's); age is the only
		// evidence available, and it already says stale.
		return true
	}
	if !isProcessAlive(pid) {
		// (c) Confirmed dead, and already past the grace period.
		return true
	}
	// (d) The pid still belongs to a running process, but the directory has
	// gone untouched for two days straight. A real long-running session
	// keeps refreshing its mtime on every paste, so a directory that
	// reaches this point either belongs to a genuinely idle session or —
	// much more likely on a long-lived machine — the pid has since been
	// recycled by an unrelated process. Either way it's safe to reclaim.
	return true
}

// isProcessAlive reports whether pid currently belongs to a running
// process. Signal 0 does no actual signaling; it only checks whether the
// process could be signaled. ESRCH means it's gone. Any other outcome (nil,
// or EPERM for a process we don't own) means it's still alive.
func isProcessAlive(pid int) bool {
	return syscall.Kill(pid, 0) != syscall.ESRCH
}

// isSafeExistingSpoolDir reports whether the deterministic spool path at
// path is safe to reuse rather than treat as a possible attack: it must be a
// real directory (not a symlink — which could point anywhere, e.g. into
// ~/.ssh), owned by us, and not group- or world-accessible.
func isSafeExistingSpoolDir(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	if !info.IsDir() {
		return false
	}
	if info.Mode().Perm() != 0o700 {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}
	return int(stat.Uid) == os.Getuid()
}
