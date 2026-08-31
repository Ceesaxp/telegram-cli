package media

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"strings"
	"sync/atomic"
)

// kittyImageID hands out a distinct id per placement.
//
// Without one the terminal assigns its own, and this client then has no way
// to delete a specific image later — only a=d with no id, which deletes
// every placement on the screen including the inline art in the thread. An
// overlay that cleaned up after itself by erasing everything else is not
// cleaning up.
//
// It starts high to stay clear of ids any other program sharing the terminal
// is likely to have taken; kitty ids are a 32-bit space shared per terminal,
// not per process.
var kittyImageID atomic.Uint32

func init() { kittyImageID.Store(0x7e10_0000) }

func nextKittyID() uint32 { return kittyImageID.Add(1) }

// renderKitty renders an image using the Kitty graphics protocol.
// See: https://sw.kovidgoyal.net/kitty/graphics-protocol/
//
// q=2 on every chunk suppresses the terminal's OK and error replies. Those
// come back on stdin, and under Bubble Tea's raw-mode input loop anything on
// stdin is a keystroke — so an unsuppressed transmission types "_Gi=31;OK\"
// into whatever has focus. It is the same hazard as the OSC 11 background
// probe this codebase already removed (see theme.SupportsTrueColor), arriving
// by a different door.
func renderKitty(img image.Image) (string, error) {
	out, _, err := renderKittyWithID(img)
	return out, err
}

// renderKittyWithID is renderKitty plus the id it placed the image under, so
// a caller that owns the image's lifetime can delete it later with
// [KittyDelete].
func renderKittyWithID(img image.Image) (string, uint32, error) {
	bounds := img.Bounds()

	// Encode image as PNG.
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", 0, fmt.Errorf("encoding PNG for kitty: %w", err)
	}

	encoded := base64.StdEncoding.EncodeToString(buf.Bytes())
	id := nextKittyID()

	var b strings.Builder

	// Send image data in chunks of 4096 bytes.
	chunkSize := 4096
	for i := 0; i < len(encoded); i += chunkSize {
		end := i + chunkSize
		if end > len(encoded) {
			end = len(encoded)
		}

		chunk := encoded[i:end]
		more := 1
		if end >= len(encoded) {
			more = 0
		}

		if i == 0 {
			// First chunk: include image metadata.
			b.WriteString(fmt.Sprintf("\033_Ga=T,f=100,i=%d,q=2,s=%d,v=%d,m=%d;%s\033\\",
				id, bounds.Dx(), bounds.Dy(), more, chunk))
		} else {
			b.WriteString(fmt.Sprintf("\033_Gm=%d,q=2;%s\033\\", more, chunk))
		}
	}

	return b.String(), id, nil
}

// KittyDelete is the sequence that removes one image and every placement of
// it, freeing the terminal's copy of the data too (d=I rather than d=i).
//
// An id of 0 returns "" rather than a delete-everything: kitty reads a=d
// with no id as "delete all visible placements", which would take the
// thread's inline art with it.
func KittyDelete(id uint32) string {
	if id == 0 {
		return ""
	}
	return fmt.Sprintf("\033_Ga=d,d=I,i=%d,q=2\033\\", id)
}
