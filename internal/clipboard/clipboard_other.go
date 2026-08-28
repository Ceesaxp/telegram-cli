//go:build !darwin && !windows && !linux && !freebsd && !openbsd && !netbsd && !dragonfly

package clipboard

func readImage(dir string) (string, error) { return "", ErrUnsupported }

func readFilePath() (string, error) { return "", ErrUnsupported }
