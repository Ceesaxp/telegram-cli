//go:build !unix

package telegram

import "os"

// sendOpenFlags opens a file to send. Outside unix there is no fifo a
// directory can hold for the open to wait on; see send_open_unix.go.
const sendOpenFlags = os.O_RDONLY

// whyInsideOS is whyInside for the errors a platform gives names to; see
// send_open_unix.go. Here there are none beyond the portable ones.
func whyInsideOS(error) error { return nil }
