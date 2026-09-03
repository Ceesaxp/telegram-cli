package app

import (
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/config"
	"github.com/Ceesaxp/telegram-cli/internal/store"
	"github.com/Ceesaxp/telegram-cli/internal/ui/cell"
)

// The declaration has to reach the package that measures.
//
// ui.emoji_width is the setting a user reaches for when the top bar has a
// gap in it, and it does nothing at all unless New hands it to cell — which
// is a package-level mode rather than a field on a panel, precisely so that
// the chat titles and the rail agree with the folder tabs about how wide an
// emoji is.
func TestTheConfiguredEmojiWidthReachesCell(t *testing.T) {
	t.Cleanup(func() { cell.SetEmojiMode(cell.EmojiAuto) })

	tests := map[string]cell.EmojiMode{
		config.EmojiWidthComposed: cell.EmojiComposed,
		config.EmojiWidthSeparate: cell.EmojiSeparate,
		config.EmojiWidthAuto:     cell.EmojiAuto,
		// A typo costs the setting, not the client.
		"narrow": cell.EmojiAuto,
		"":       cell.EmojiAuto,
	}

	for value, want := range tests {
		t.Run(value, func(t *testing.T) {
			// Start from the other mode, so a New that sets nothing at all
			// leaves an observably wrong value rather than the right one by
			// accident.
			cell.SetEmojiMode(cell.EmojiComposed)
			if want == cell.EmojiComposed {
				cell.SetEmojiMode(cell.EmojiSeparate)
			}

			cfg := &config.Config{}
			cfg.UI.EmojiWidth = value
			New(cfg, nil, store.NewStore(), nil)

			if got := cell.CurrentEmojiMode(); got != want {
				t.Errorf("emoji_width = %q left cell in %v, want %v",
					value, got, want)
			}
		})
	}
}
