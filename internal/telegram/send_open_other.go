//go:build !unix

package telegram

import "os"

// sendOpenFlags opens a file to send. Outside unix there is no fifo a
// directory can hold for the open to wait on; see send_open_unix.go.
const sendOpenFlags = os.O_RDONLY
