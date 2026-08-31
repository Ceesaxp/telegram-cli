package render

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/imtaqin/telegram-cli/internal/config"
	"github.com/imtaqin/telegram-cli/internal/store"
	"github.com/imtaqin/telegram-cli/internal/telegram"
)

// photoWithArt is a photo whose art is already in the renderer's cache, so
// the drawing decision is exercised without touching a decoder.
func photoWithArt(t *testing.T, r *MessageRenderer, rows int) *telegram.Message {
	t.Helper()
	const fileID = "cached-photo"
	art := make([]string, rows)
	for i := range art {
		art[i] = strings.Repeat("#", 20)
	}
	r.imgCache.Set("img:"+fileID, strings.Join(art, "\n"))

	return &telegram.Message{
		ID: 1, ChatID: 1, Date: 1700000000,
		Content: &telegram.MessagePhoto{Photo: &telegram.Photo{
			Sizes: []*telegram.PhotoSize{{
				Width: 20, Height: rows,
				File: &telegram.File{ID: fileID, Downloaded: true, Path: "/fake.jpg"},
			}},
		}},
	}
}

// "on_open" means the picture appears when you OPEN it — in the full-pane
// overlay enter raises — and not in the history.
//
// It used to draw the art inline for any photo whose thumbnail had been
// downloaded, which is a different feature with the same name. The height was
// the damage: a message that grows from one line to twenty when a thumbnail
// lands invalidates the chat view's line index under the reader, and the
// scroll arithmetic and the }/{ motions are computed from it. A photo landing
// mid-scroll made the next motion jump somewhere unrelated.
func TestOnOpenDrawsACardNotArt(t *testing.T) {
	r := newTestRenderer()
	st := store.NewStore()
	msg := photoWithArt(t, r, 12)

	for _, policy := range []string{config.InlineImagesOnOpen, config.InlineImagesNever} {
		t.Run(policy, func(t *testing.T) {
			r.SetInlineImages(policy)
			lines := r.RenderBody(msg, st, BodyOptions{Width: 40})

			for i, line := range lines {
				if strings.Contains(ansi.Strip(line), "####") {
					t.Fatalf("line %d is inline art under %q:\n%s", i, policy,
						ansi.Strip(line))
				}
			}
			// A card, so the message still says what it is.
			joined := ansi.Strip(strings.Join(lines, "\n"))
			if !strings.Contains(joined, "photo") {
				t.Errorf("no metadata card under %q:\n%s", policy, joined)
			}
		})
	}
}

// "always" draws the art.
func TestAlwaysDrawsTheArt(t *testing.T) {
	r := newTestRenderer()
	r.SetInlineImages(config.InlineImagesAlways)

	lines := r.RenderBody(photoWithArt(t, r, 4), store.NewStore(),
		BodyOptions{Width: 40})
	if len(lines) == 0 || !strings.Contains(ansi.Strip(lines[0]), "####") {
		t.Errorf("no art under \"always\":\n%s", strings.Join(lines, "\n"))
	}
}

// The design record bounds the inline preview: "Always may use an eight-row
// card preview." The bound is the point — it makes a photo's height a
// property of the setting rather than of the image, which is what the chat
// view's line index needs.
//
// Asserted on the renderer's budget rather than on rendered rows, because
// that is the one mechanism: every image renderer this type builds goes
// through inlineRenderer, and a picture is SCALED to its budget rather than
// cut down afterwards. Testing the output instead would need a real decoder
// and would pass just as well against a version that cropped.
func TestEveryInlineRendererIsBounded(t *testing.T) {
	tests := []struct {
		name string
		rows int
	}{
		{"the configured height", 100},
		{"an unset height, which falls back to the default", 0},
		{"a height already inside the bound", 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newTestRenderer()
			r.SetImageProtocol(0, 200, tt.rows)
			if got := r.imgRend.MaxRows(); got > inlineArtRows {
				t.Errorf("budget is %d rows, over the %d-row bound",
					got, inlineArtRows)
			}
		})
	}

	// Including the one a renderer is born with: the bound has to hold on
	// the path where nobody calls SetImageProtocol at all.
	if got := newTestRenderer().imgRend.MaxRows(); got > inlineArtRows {
		t.Errorf("the default renderer's budget is %d rows, over the bound", got)
	}
}

// A photo with no art yet is a card under every policy — there is nothing to
// draw, and the card is what says so.
func TestAPhotoWithNoArtIsAlwaysACard(t *testing.T) {
	st := store.NewStore()
	msg := &telegram.Message{
		ID: 1, ChatID: 1, Date: 1700000000,
		Content: &telegram.MessagePhoto{Photo: &telegram.Photo{
			Sizes: []*telegram.PhotoSize{{Width: 800, Height: 600}},
		}},
	}

	for _, policy := range []string{
		config.InlineImagesNever, config.InlineImagesOnOpen, config.InlineImagesAlways,
	} {
		r := newTestRenderer()
		r.SetInlineImages(policy)
		joined := ansi.Strip(strings.Join(r.RenderBody(msg, st, BodyOptions{Width: 40}), "\n"))
		if !strings.Contains(joined, "photo") {
			t.Errorf("%q: no card for an undownloaded photo:\n%s", policy, joined)
		}
	}
}
