//go:build !darwin && !windows && !linux && !freebsd && !openbsd && !netbsd && !dragonfly

package clipboard

func writeText(string) error { return ErrNoWriter }
