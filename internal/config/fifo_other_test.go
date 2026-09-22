//go:build !unix

package config

import "testing"

// makeFifo skips: there are no named pipes in the filesystem here.
func makeFifo(t *testing.T, _ string) {
	t.Helper()
	t.Skip("no fifos on this platform")
}
