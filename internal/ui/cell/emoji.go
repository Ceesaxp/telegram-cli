package cell

import (
	"strings"
	"sync/atomic"

	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
)

// Emoji width is not a property of the string, and this package cannot ask.
//
// A terminal decides how wide an emoji sequence is drawn, and terminals
// disagree — not slightly, and not in one direction. Three classes of
// sequence have a *composition* rule, and a terminal that applies it and one
// that does not produce different widths:
//
//   - A base character followed by U+FE0F. "❤️" is U+2764 plus the emoji
//     presentation selector. A terminal that honours the selector draws a
//     double-width picture; one that ignores it draws the narrow text heart
//     the base character already was. Tables say 2, that terminal draws 1 —
//     the row comes out SHORT.
//   - A ZWJ sequence. "👨‍👩‍👧" is three emoji joined by U+200D. A terminal that
//     composes draws one family in two cells; one that does not draws three
//     emoji in six. Tables say 2 — the row comes out LONG.
//   - A regional-indicator pair. "🇷🇸" is two letters that compose into a flag
//     in two cells, or stay two letter-boxes in four. Tables say 2 — LONG
//     again.
//
// So "does this terminal draw emoji narrow or wide" is the wrong question:
// the same terminal is narrow on the first class and wide on the other two.
// The right question is whether it COMPOSES, which is what the modes below
// name. There is no environment variable that answers it, and the runtime
// query that would was removed in wave 5 — its response bytes leaked into
// the composer. So it is declared, or it is guessed at pessimistically.
type EmojiMode int32

const (
	// EmojiAuto measures with the Unicode tables and, where a caller asks
	// for a RESERVATION rather than a measurement, keeps room for whichever
	// of the two renderings is wider.
	//
	// The pessimism goes in one direction only. Over-reserving costs a gap
	// or a tab dropped early — visible, harmless and self-evident.
	// Under-reserving lets a row run past its budget and overwrite what is
	// beside it, which is what put "nnected" on somebody's top bar. Given
	// the choice between a gap and a corrupted row, this takes the gap.
	//
	// The default, and the only mode that is a guess.
	EmojiAuto EmojiMode = iota

	// EmojiComposed declares that this terminal applies every composition
	// rule: a selector is honoured, a ZWJ sequence is one glyph, a flag is
	// a flag. The tables are then exactly right and nothing is reserved on
	// top of them.
	EmojiComposed

	// EmojiSeparate declares that this terminal applies none of them: a
	// selector is ignored and the base drawn as text, and a joined or
	// paired sequence is drawn as its separate parts. Widths are then the
	// sum of the pieces — narrower than the tables for the first class,
	// wider for the other two.
	EmojiSeparate
)

// The mode is process-wide and set once at startup from config, like
// lipgloss's colour profile: it describes the terminal the process is
// attached to, which does not change while it runs. Atomic because the
// renderer and the Bubble Tea input loop are different goroutines.
var emojiMode atomic.Int32

// SetEmojiMode declares how this terminal draws composed emoji. Call it
// once, before the first render.
func SetEmojiMode(m EmojiMode) { emojiMode.Store(int32(m)) }

// CurrentEmojiMode reports the declared mode.
func CurrentEmojiMode() EmojiMode { return EmojiMode(emojiMode.Load()) }

// Reserve is how much room to keep for s.
//
// In a declared mode it is [Width]: the user has said which way their
// terminal draws, so there is nothing to hedge. In [EmojiAuto] it is the
// WIDER of the two renderings — what the tables say, and what a terminal
// that composes nothing would draw. That is the tight upper bound over the
// possibilities, which is what a reservation wants: it can only ever be too
// generous, and being too generous costs a gap.
//
// Counting a cell per composition rune, which is what this did first, is not
// that bound. A three-person family is two ZWJs, so it reserved four — and a
// terminal that composes nothing draws six, straight over whatever was
// beside it. The bug is the one this code exists to prevent, in the code
// meant to prevent it.
//
// Callers laying out a row against a budget want this; callers measuring
// what was actually drawn want [Width].
func Reserve(s string) int {
	if CurrentEmojiMode() != EmojiAuto {
		return Width(s)
	}
	tables, separate := ansi.StringWidth(s), separateWidth(s)
	if separate > tables {
		return separate
	}
	return tables
}

func isComposing(r rune) bool {
	switch {
	case r == 0xFE0F: // VARIATION SELECTOR-16, emoji presentation
		return true
	case r == 0x200D: // ZERO WIDTH JOINER
		return true
	case r >= 0x1F1E6 && r <= 0x1F1FF: // REGIONAL INDICATOR
		return true
	case r >= 0x1F3FB && r <= 0x1F3FF: // EMOJI MODIFIER, skin tone
		// A terminal that composes tints the hand. One that does not draws
		// the swatch beside it, in two more cells — the same failure as a
		// flag that stays two letters.
		return true
	}
	return false
}

// separateWidth measures s as a terminal that composes nothing draws it.
//
// It is the tables' answer PLUS a correction for each cluster that carries a
// composition rule, rather than a measurement built up cluster by cluster.
// That is not a detail: this must agree with the tables exactly on text that
// has no such rule, and summing per-cluster widths does not. "नमस्ते" is
// three cells measured whole and four measured a grapheme at a time — so a
// declaration about emoji would have silently re-measured somebody's Hindi.
//
// Escapes are stripped first for the same reason [Width] delegates to a
// sequence-aware measurer: this is called on assembled, styled rows, and an
// SGR parameter is not text.
func separateWidth(s string) int {
	s = ansi.Strip(s)
	total := ansi.StringWidth(s)

	state := -1
	for len(s) > 0 {
		var cluster string
		cluster, s, _, state = uniseg.FirstGraphemeClusterInString(s, state)
		if composes(cluster) {
			total += decomposedWidth(cluster) - ansi.StringWidth(cluster)
		}
	}
	return total
}

func composes(cluster string) bool {
	for _, r := range cluster {
		if isComposing(r) {
			return true
		}
	}
	return false
}

// decomposedWidth is a composed cluster measured rune by rune, which is what
// "the rule was not applied" means.
//
// The joiners and selectors need no special case: they are zero-width on
// their own, because they are instructions rather than glyphs. A skin-tone
// modifier is not — it is a visible swatch when nothing tints the hand it
// followed — and measuring per rune is what gets that right.
func decomposedWidth(cluster string) int {
	total := 0
	for _, r := range cluster {
		total += ansi.StringWidth(string(r))
	}
	return total
}

// ParseEmojiMode reads a configured value. Unrecognised falls back to
// [EmojiAuto] rather than failing: a typo in a cosmetic setting should cost
// the user the setting, not the client.
func ParseEmojiMode(v string) EmojiMode {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "composed":
		return EmojiComposed
	case "separate":
		return EmojiSeparate
	default:
		return EmojiAuto
	}
}
