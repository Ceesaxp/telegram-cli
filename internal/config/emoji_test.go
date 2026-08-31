package config

import "testing"

// A typo in a cosmetic setting should cost the user the setting, not the
// client — and "narrow"/"wide" are the two words a user is most likely to
// reach for, so they must land somewhere safe rather than somewhere wrong.
func TestResolveEmojiWidth(t *testing.T) {
	tests := map[string]string{
		EmojiWidthComposed: EmojiWidthComposed,
		EmojiWidthSeparate: EmojiWidthSeparate,
		" Separate ":       EmojiWidthSeparate,
		"COMPOSED":         EmojiWidthComposed,
		EmojiWidthAuto:     EmojiWidthAuto,
		"":                 EmojiWidthAuto,
		"narrow":           EmojiWidthAuto,
		"wide":             EmojiWidthAuto,
		"true":             EmojiWidthAuto,
	}
	for in, want := range tests {
		if got := ResolveEmojiWidth(in); got != want {
			t.Errorf("ResolveEmojiWidth(%q) = %q, want %q", in, got, want)
		}
	}
}

// Auto is the default: it is the only value that is safe without having been
// told anything about the terminal.
func TestEmojiWidthDefaultsToAuto(t *testing.T) {
	if got := defaultConfig().UI.EmojiWidth; got != EmojiWidthAuto {
		t.Errorf("the default emoji_width is %q, want %q", got, EmojiWidthAuto)
	}
}

// An existing config predates the field, and the whole point of the setting
// is that a user with the gap has to be able to find it. A key written into
// their file and named in the migration report is how they do.
func TestMigrateAddsEmojiWidth(t *testing.T) {
	cfg := &Config{}
	changes := changeMap(t, Migrate(cfg, nil))

	c, ok := changes["ui.emoji_width"]
	if !ok {
		t.Fatal("ui.emoji_width was not filled in")
	}
	if !c.Absent {
		t.Error("ui.emoji_width was reported as replacing a value it never had")
	}
	if c.New != EmojiWidthAuto {
		t.Errorf("ui.emoji_width -> %q, want %q", c.New, EmojiWidthAuto)
	}
	if cfg.UI.EmojiWidth != EmojiWidthAuto {
		t.Errorf("the config was not updated: %q", cfg.UI.EmojiWidth)
	}
}
