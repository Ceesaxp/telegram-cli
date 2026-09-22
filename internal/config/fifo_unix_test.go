//go:build unix

package config

import (
	"syscall"
	"testing"
)

// makeFifo creates a named pipe at path.
func makeFifo(t *testing.T, path string) {
	t.Helper()
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}
}
