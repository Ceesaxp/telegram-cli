package theme

import (
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Roles is the TUI 2.0 semantic palette (docs/tui-2.0.md, "Palette").
//
// Every colour here is named for what it MEANS, not what it looks like. That
// is the point: amber, green, mauve and blue are semantic — a channel sigil
// is amber because it is a channel, not because amber looks nice there — and
// cyan is the sole focus accent. A renderer that wants "a nice highlight"
// has no role to reach for, which is deliberate.
//
// The existing Theme fields are untouched. The two coexist while the
// components migrate; roles are what new frame code uses.
type Roles struct {
	// Surfaces, back to front.
	Bg       lipgloss.Color // app background and scroller
	Panel    lipgloss.Color // list, rail, headers, composer
	Chrome   lipgloss.Color // top and hint bars
	Sel      lipgloss.Color // selected chat row
	CurLine  lipgloss.Color // selected message row
	Rule     lipgloss.Color // panel separators
	RuleSoft lipgloss.Color // in-panel dividers
	Border   lipgloss.Color // attachment, code and poll frames

	// Text, dimmest to brightest.
	Ghost  lipgloss.Color // separators and inert glyphs
	Faint  lipgloss.Color // timestamps and byte counts
	Dim    lipgloss.Color // secondary copy
	Fg     lipgloss.Color // message body
	Bright lipgloss.Color // active titles and bold spans

	// Semantic accents.
	Cyan  lipgloss.Color // sole focus and key accent
	Amber lipgloss.Color // commands, attachments, channels, inline code
	Green lipgloss.Color // insert mode, online, own messages, sent
	Mauve lipgloss.Color // groups, italics, sender colour
	Blue  lipgloss.Color // DMs, mentions, sender colour
	Red   lipgloss.Color // errors, failures, removed diff lines
}

// darkHex and dark256 are the two renderings of the same role table. They
// are kept as parallel literals rather than generated from one another
// because the 256-colour column is a hand-picked approximation, not a
// mechanical quantisation of the hex — see docs/tui-2.0.md's palette table,
// which is the source both were transcribed from.
var darkHex = Roles{
	Bg: "#0b0d10", Panel: "#0e1116", Chrome: "#12151a",
	Sel: "#171d24", CurLine: "#12171d",
	Rule: "#1f242b", RuleSoft: "#1a1f26", Border: "#262d36",

	Ghost: "#3f4750", Faint: "#465059", Dim: "#5c666e",
	Fg: "#c9ced4", Bright: "#e2e7ec",

	Cyan: "#6fb8c9", Amber: "#d1a86a", Green: "#86b57a",
	Mauve: "#b58ac9", Blue: "#8aa8d0", Red: "#c9736a",
}

var dark256 = Roles{
	Bg: "232", Panel: "233", Chrome: "234",
	Sel: "235", CurLine: "234",
	Rule: "236", RuleSoft: "235", Border: "237",

	Ghost: "239", Faint: "240", Dim: "243",
	Fg: "252", Bright: "255",

	Cyan: "73", Amber: "179", Green: "108",
	Mauve: "139", Blue: "110", Red: "167",
}

// DarkRoles returns the dark palette for the terminal's colour depth.
//
// The depth is resolved once, by the caller, at startup — see
// [SupportsTrueColor]. Re-querying per render would be both wasteful and
// unstable, and this app has a hard rule against asking the terminal
// anything at runtime: an OSC reply arrives as keystrokes and gets typed
// into whatever is focused. Environment variables only.
func DarkRoles(trueColor bool) Roles {
	if trueColor {
		return darkHex
	}
	return dark256
}

// LightRoles inverts the surface and text ramps while keeping the semantic
// accents recognisable. Dark is the high-fidelity reference (decision: the
// design record specifies only that light is "an inversion of the same
// roles"), so this is deliberately a mechanical inversion rather than a
// second hand-tuned palette pretending to be one.
func LightRoles(trueColor bool) Roles {
	r := DarkRoles(trueColor)
	if trueColor {
		r.Bg, r.Panel, r.Chrome = "#fbfcfd", "#f4f6f8", "#eef1f4"
		r.Sel, r.CurLine = "#e4e9ee", "#eaeef2"
		r.Rule, r.RuleSoft, r.Border = "#d3dae1", "#e1e6ea", "#c2cbd4"
		r.Ghost, r.Faint, r.Dim = "#98a1a9", "#7d868e", "#5c666e"
		r.Fg, r.Bright = "#26303a", "#0b0d10"
		return r
	}
	r.Bg, r.Panel, r.Chrome = "231", "255", "254"
	r.Sel, r.CurLine = "253", "254"
	r.Rule, r.RuleSoft, r.Border = "251", "253", "249"
	r.Ghost, r.Faint, r.Dim = "247", "245", "243"
	r.Fg, r.Bright = "236", "232"
	return r
}

// SupportsTrueColor reports whether the terminal advertises 24-bit colour.
//
// Environment only, never a runtime query. This app learned that lesson
// already: termenv's background probe writes an OSC 11 sequence and reads
// the reply off stdin, which under Bubble Tea's raw-mode input loop is
// delivered to the program as keystrokes and typed into the composer.
//
// The original crime scene was glamour's WithAutoStyle, which resolved to
// that probe. glamour is no longer a dependency — the thread grid renders
// entities directly — so the hazard is gone rather than guarded. This
// function is where the rule now lives, and it must stay environment-only.
func SupportsTrueColor() bool {
	switch strings.ToLower(os.Getenv("COLORTERM")) {
	case "truecolor", "24bit":
		return true
	}
	// Some terminals only say so in TERM.
	return strings.Contains(strings.ToLower(os.Getenv("TERM")), "truecolor")
}

// RolesFor picks the palette for a theme name and colour depth. It is the
// single entry point the app uses, so the two decisions are made together
// and once.
func RolesFor(name string, trueColor bool) Roles {
	if name == "light" {
		return LightRoles(trueColor)
	}
	return DarkRoles(trueColor)
}

// SenderColour is the deterministic colour of a person's name.
//
// Deterministic because the colour is an identity cue: the same person has to
// be the same colour on every line of every session, or it is noise rather
// than information. The hash is FNV-1a over the ID's bytes so that
// consecutive user IDs — which is what a small group of colleagues who signed
// up together actually have — do not land in one bucket.
//
// It lives here rather than in the thread grid because the rail names the
// same people. Two implementations that agree today are two implementations
// that can stop agreeing, and a person shown mauve in the thread and blue in
// the rail beside it is two people as far as the reader is concerned.
func SenderColour(id int64, r Roles) lipgloss.Color {
	palette := [...]lipgloss.Color{r.Mauve, r.Cyan, r.Blue, r.Amber}

	const (
		offset64 = uint64(14695981039346656037)
		prime64  = uint64(1099511628211)
	)
	h := offset64
	u := uint64(id)
	for range 8 {
		h ^= u & 0xff
		h *= prime64
		u >>= 8
	}
	return palette[h%uint64(len(palette))]
}
