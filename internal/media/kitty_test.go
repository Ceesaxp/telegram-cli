package media

import (
	"image"
	"image/color"
	"strings"
	"testing"
)

// testImage is deliberately high-entropy: a smooth gradient compresses to
// well under one 4096-byte chunk, and a single-chunk transmission would let
// the continuation chunks go unchecked.
func testImage(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	n := uint32(2166136261)
	for y := range h {
		for x := range w {
			n = (n ^ uint32(x*31+y*17)) * 16777619
			img.Set(x, y, color.RGBA{
				R: uint8(n), G: uint8(n >> 8), B: uint8(n >> 16), A: 255,
			})
		}
	}
	return img
}

// Every chunk carries q=2, which suppresses the terminal's OK and error
// replies.
//
// Those replies come back on STDIN, and under Bubble Tea's raw-mode input
// loop anything on stdin is a keystroke: an unsuppressed transmission types
// "_Gi=31;OK\" into whatever has focus. It is the same hazard as the OSC 11
// background probe this codebase already removed, arriving by a different
// door, and it was live for every inline photo drawn on kitty before this
// test existed.
func TestEveryKittyChunkSuppressesTheReply(t *testing.T) {
	// Big enough that the base64 payload needs several 4096-byte chunks, so
	// the continuation chunks are covered and not only the first.
	art, _, err := renderKittyWithID(testImage(200, 200))
	if err != nil {
		t.Fatalf("rendering: %v", err)
	}

	chunks := strings.Split(art, "\x1b_G")[1:]
	if len(chunks) < 2 {
		t.Fatalf("want a multi-chunk transmission, got %d chunk(s)", len(chunks))
	}
	for i, chunk := range chunks {
		control, _, ok := strings.Cut(chunk, ";")
		if !ok {
			t.Fatalf("chunk %d has no payload separator", i)
		}
		if !strings.Contains(control, "q=2") {
			t.Errorf("chunk %d does not suppress the terminal's reply: %q", i, control)
		}
	}
}

// The image is placed under an id this client chose, so it can be deleted
// later without deleting everything else on the screen.
func TestKittyPlacesUnderAnIDWeChose(t *testing.T) {
	art, id, err := renderKittyWithID(testImage(8, 8))
	if err != nil {
		t.Fatalf("rendering: %v", err)
	}
	if id == 0 {
		t.Fatal("no placement id")
	}
	if !strings.Contains(art, "i=") {
		t.Errorf("the transmission carries no id: %q", art[:min(80, len(art))])
	}

	// Distinct per placement: two images sharing an id would make deleting
	// one delete the other.
	_, second, _ := renderKittyWithID(testImage(8, 8))
	if second == id {
		t.Errorf("two placements share the id %d", id)
	}
}

// A bare a=d is "every placement on screen", which would take the thread's
// inline art with the overlay's photo.
func TestKittyDeleteNamesTheImage(t *testing.T) {
	got := KittyDelete(31)
	for _, want := range []string{"a=d", "d=I", "i=31", "q=2"} {
		if !strings.Contains(got, want) {
			t.Errorf("KittyDelete(31) = %q, missing %q", got, want)
		}
	}
	if KittyDelete(0) != "" {
		t.Error("KittyDelete(0) produced a delete-everything sequence")
	}
}

// Cell-based protocols are erased by the next frame, so a teardown for them
// would be a sequence emitted for nothing.
func TestOnlyKittyNeedsATeardown(t *testing.T) {
	tests := []struct {
		protocol Protocol
		teardown bool
	}{
		{ProtocolKitty, true},
		{ProtocolSixel, false},
		{ProtocolBlocks, false},
	}
	for _, tt := range tests {
		t.Run(ProtocolName(tt.protocol), func(t *testing.T) {
			p, err := NewImageRenderer(tt.protocol, 40, 20).PlaceImage(testImage(16, 16))
			if err != nil {
				t.Fatalf("placing: %v", err)
			}
			if got := p.Teardown != ""; got != tt.teardown {
				t.Errorf("teardown present = %v, want %v", got, tt.teardown)
			}
		})
	}
}
