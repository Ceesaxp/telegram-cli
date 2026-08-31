package cell

import "testing"

// withMode sets the process-wide mode for one test and puts it back.
func withMode(t *testing.T, m EmojiMode) {
	t.Helper()
	prev := CurrentEmojiMode()
	SetEmojiMode(m)
	t.Cleanup(func() { SetEmojiMode(prev) })
}

// The three classes fail in different directions, which is the whole reason
// this is a declaration about composition rather than a "narrow or wide"
// switch.
//
// A base plus U+FE0F is a narrow character the tables promote to two cells:
// a terminal that ignores the selector draws one, so the tables OVER-measure
// and a right-aligned row ends short — the three-cell gap after the clock.
// A ZWJ sequence and a regional-indicator pair are the opposite: the tables
// say two, and a terminal that composes nothing draws every part, so they
// UNDER-measure and a row overwrites what is beside it.
func TestTheModesMeasureTheThreeClasses(t *testing.T) {
	tests := []struct {
		name                       string
		s                          string
		tables, separate, reserved int
	}{
		// U+2764 is one cell on its own; U+FE0F is what makes the tables
		// say two.
		{"a selector on a narrow base", "❤️", 2, 1, 2},
		{"another one", "⁉️", 2, 1, 2},
		// Three emoji joined into one family. The reservation is the
		// decomposed six, not the tables' two — a folder named with one
		// used to be reserved at four and drawn at six.
		{"a ZWJ sequence", "👨‍👩‍👧", 2, 6, 6},
		// Two letters that compose into a flag.
		{"a regional-indicator pair", "🇷🇸", 2, 4, 4},
		// A skin tone is a modifier, not a joiner: uncomposed it is a
		// visible swatch beside the hand, not nothing.
		{"a skin-tone modifier", "👍🏻", 2, 4, 4},
		// No composition rule: every terminal agrees, and nothing is
		// reserved on top.
		{"a plain wide emoji", "😀", 2, 2, 2},
		{"plain text", "all", 3, 3, 3},
		// A spacing mark is a grapheme rule, not a composition rule: no
		// terminal draws क and ा side by side, so this must be measured
		// whole in every mode. Summed rune by rune it comes out 2.
		{"a Devanagari spacing mark", "का", 1, 1, 1},
		{"a Tamil vowel sign", "நி", 1, 1, 1},
		{"a mixed label", "2:❤️", 4, 3, 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withMode(t, EmojiAuto)
			if got := Width(tt.s); got != tt.tables {
				t.Errorf("auto Width = %d, want %d", got, tt.tables)
			}
			if got := Reserve(tt.s); got != tt.reserved {
				t.Errorf("auto Reserve = %d, want %d", got, tt.reserved)
			}

			withMode(t, EmojiComposed)
			if got := Width(tt.s); got != tt.tables {
				t.Errorf("composed Width = %d, want the tables' %d", got, tt.tables)
			}
			// A declaration replaces the guess: nothing is reserved on top
			// of a width the user has told us is right.
			if got := Reserve(tt.s); got != tt.tables {
				t.Errorf("composed Reserve = %d, want %d — it is still guessing",
					got, tt.tables)
			}

			withMode(t, EmojiSeparate)
			if got := Width(tt.s); got != tt.separate {
				t.Errorf("separate Width = %d, want %d", got, tt.separate)
			}
			if got := Reserve(tt.s); got != tt.separate {
				t.Errorf("separate Reserve = %d, want %d — it is still guessing",
					got, tt.separate)
			}
		})
	}
}

// Only sequences with a composition RULE are taken apart.
//
// Decomposing everything gets the emoji right and ordinary text wrong. A
// grapheme cluster is not always a composition: "का" is Devanagari ka
// followed by a spacing mark, drawn as one cell by every terminal there is,
// and summed rune by rune it comes to two. A declaration about emoji must
// not quietly re-measure somebody's Hindi.
func TestOnlyComposedSequencesAreTakenApart(t *testing.T) {
	withMode(t, EmojiSeparate)

	tests := map[string]string{
		"Devanagari with a spacing mark": "नमस्ते",
		"Tamil with a vowel sign":        "வணக்கம்",
		"a combining accent":             "café",
		"Hangul from jamo":               "각",
		"ASCII":                          "hello",
	}
	for name, s := range tests {
		t.Run(name, func(t *testing.T) {
			withMode(t, EmojiComposed)
			tables := Width(s)
			withMode(t, EmojiSeparate)

			if got := Width(s); got != tables {
				t.Errorf("%q measures %d here and %d by the tables — text "+
					"with no composition rule was taken apart", s, got, tables)
			}
		})
	}
}

// Width is called on assembled, styled rows. An SGR parameter is digits and
// semicolons, and counting it as text is how a row that measures right comes
// out wrong.
func TestSeparateWidthIgnoresEscapes(t *testing.T) {
	withMode(t, EmojiSeparate)

	plain := "2:❤️"
	tests := map[string]string{
		"around the label": "\x1b[38;5;231;1m" + plain + "\x1b[0m",
		// And through the middle of the sequence. A style run that ends
		// between a base character and its selector splits one grapheme in
		// two, and a measurer that walks the raw bytes sees a bare heart
		// and a stray selector rather than the composed pair — so it
		// applies no correction and reports the tables' width. Styling code
		// does not know where graphemes are; this does.
		"between the base and its selector": "2:\x1b[1m❤\x1b[0m️",
		// A URI is not text. Telegram links carry non-ASCII, and a walk
		// over the raw bytes would find the flag INSIDE the OSC 8 opener,
		// decide it composes, and add two cells for a sequence the
		// terminal never draws.
		"a hyperlink whose URI holds a flag": Link("https://example.com/🇷🇸", plain),
	}

	for name, styled := range tests {
		t.Run(name, func(t *testing.T) {
			if got, want := Width(styled), Width(plain); got != want {
				t.Errorf("measures %d, the same text unstyled measures %d",
					got, want)
			}
		})
	}
}

// The mode is a declaration, and an unrecognised one is a typo in a cosmetic
// setting — it must cost the user the setting, not the client.
func TestParseEmojiMode(t *testing.T) {
	tests := map[string]EmojiMode{
		"composed":  EmojiComposed,
		"separate":  EmojiSeparate,
		" COMPOSED": EmojiComposed,
		"auto":      EmojiAuto,
		"":          EmojiAuto,
		"narrow":    EmojiAuto,
		"wide":      EmojiAuto,
	}
	for in, want := range tests {
		if got := ParseEmojiMode(in); got != want {
			t.Errorf("ParseEmojiMode(%q) = %v, want %v", in, got, want)
		}
	}
}

// Auto is the default because it is the only mode that is safe without being
// told anything: it never under-reserves.
func TestAutoIsTheDefaultAndNeverUnderReserves(t *testing.T) {
	if CurrentEmojiMode() != EmojiAuto {
		t.Fatalf("the default mode is %v, want auto", CurrentEmojiMode())
	}

	withMode(t, EmojiAuto)
	for _, s := range []string{"❤️", "⁉️", "👨‍👩‍👧", "🇷🇸", "😀", "all"} {
		auto := Reserve(s)
		withMode(t, EmojiSeparate)
		separate := Width(s)
		withMode(t, EmojiAuto)

		// Auto has to cover whichever way the terminal turns out to draw
		// it — the tables' answer and the decomposed one both.
		if auto < separate {
			t.Errorf("auto reserves %d for %q, but a terminal that composes "+
				"nothing draws %d", auto, s, separate)
		}
		if auto < Width(s) {
			t.Errorf("auto reserves %d for %q, less than the tables' %d",
				auto, s, Width(s))
		}
	}
}
