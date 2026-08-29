package keys

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

// These tests drive the *real* terminal input decoder rather than
// hand-built tea.KeyPressMsg values. That distinction is the whole point:
// the alt+1/2/3, alt+c and alt+h/alt+l bindings passed every synthetic-
// message test while being dead in a real terminal, because
// tea.KeyPressMsg.String() returns Key.Text whenever the terminal attached
// any — and the Kitty keyboard protocol attaches the macOS Option-composed
// character ("¡" for Option+1) to an alt-modified key press. Only a
// byte-sequence-level test can catch that.
//
// bubbletea v2's input loop is a thin adapter over uv.EventDecoder
// (charm.land/bubbletea/v2/input.go: uv.KeyPressEvent -> tea.KeyPressMsg),
// so decoding with uv.EventDecoder here reproduces exactly what a
// program's Update sees.

// decodeKey runs one raw terminal byte sequence through the ultraviolet
// event decoder and returns it as the bubbletea message the program would
// receive.
func decodeKey(t *testing.T, seq string) tea.KeyPressMsg {
	t.Helper()
	var d uv.EventDecoder
	n, ev := d.Decode([]byte(seq))
	if n != len(seq) {
		t.Fatalf("decode(%q) consumed %d of %d bytes", seq, n, len(seq))
	}
	kp, ok := ev.(uv.KeyPressEvent)
	if !ok {
		t.Fatalf("decode(%q) produced %T, want uv.KeyPressEvent", seq, ev)
	}
	return tea.KeyPressMsg(kp)
}

// TestDecoderKeyStrings records what the real decoder produces for every
// encoding a terminal may use for the app's bindings. The "want String" column
// is the trap: for Kitty-with-associated-text it is the composed character,
// not the binding name, which is why dispatch matches on Keystroke().
func TestDecoderKeyStrings(t *testing.T) {
	cases := []struct {
		name          string
		seq           string
		wantString    string
		wantKeystroke string
	}{
		// Legacy ESC-prefix alt (xterm "metaSendsEscape", iTerm2 "Esc+",
		// Terminal.app "Use Option as Meta key").
		{"legacy alt+1", "\x1b1", "alt+1", "alt+1"},
		{"legacy alt+2", "\x1b2", "alt+2", "alt+2"},
		{"legacy alt+3", "\x1b3", "alt+3", "alt+3"},
		{"legacy alt+c", "\x1bc", "alt+c", "alt+c"},
		{"legacy alt+h", "\x1bh", "alt+h", "alt+h"},
		{"legacy alt+l", "\x1bl", "alt+l", "alt+l"},
		{"legacy alt+j", "\x1bj", "alt+j", "alt+j"},
		{"legacy alt+k", "\x1bk", "alt+k", "alt+k"},

		// Kitty CSI-u, no associated text (US layout / option-as-alt).
		{"kitty alt+1", "\x1b[49;3u", "alt+1", "alt+1"},
		{"kitty alt+2", "\x1b[50;3u", "alt+2", "alt+2"},
		{"kitty alt+3", "\x1b[51;3u", "alt+3", "alt+3"},
		{"kitty alt+c", "\x1b[99;3u", "alt+c", "alt+c"},
		{"kitty alt+h", "\x1b[104;3u", "alt+h", "alt+h"},
		{"kitty alt+l", "\x1b[108;3u", "alt+l", "alt+l"},
		{"kitty alt+j", "\x1b[106;3u", "alt+j", "alt+j"},
		{"kitty alt+k", "\x1b[107;3u", "alt+k", "alt+k"},

		// Kitty CSI-u with associated text (3rd parameter): the macOS
		// Option-composed character rides along with the modifier. String()
		// reports the composed character; only Keystroke() names the binding.
		{"kitty alt+1 + text ¡", "\x1b[49;3;161u", "¡", "alt+1"},
		{"kitty alt+2 + text ™", "\x1b[50;3;8482u", "™", "alt+2"},
		{"kitty alt+3 + text £", "\x1b[51;3;163u", "£", "alt+3"},
		{"kitty alt+c + text ç", "\x1b[99;3;231u", "ç", "alt+c"},
		{"kitty alt+h + text ˙", "\x1b[104;3;729u", "˙", "alt+h"},
		{"kitty alt+l + text ¬", "\x1b[108;3;172u", "¬", "alt+l"},
		{"kitty alt+j + text ∆", "\x1b[106;3;8710u", "∆", "alt+j"},
		{"kitty alt+k + text ˚", "\x1b[107;3;730u", "˚", "alt+k"},

		// XTerm modifyOtherKeys CSI 27 ; mod ; code ~.
		{"modifyOtherKeys alt+1", "\x1b[27;3;49~", "alt+1", "alt+1"},
		{"modifyOtherKeys alt+2", "\x1b[27;3;50~", "alt+2", "alt+2"},
		{"modifyOtherKeys alt+3", "\x1b[27;3;51~", "alt+3", "alt+3"},

		// Non-alt bindings, for contrast.
		{"f1", "\x1bOP", "f1", "f1"},
		{"f2", "\x1bOQ", "f2", "f2"},
		{"f3", "\x1bOR", "f3", "f3"},
		{"ctrl+v", "\x16", "ctrl+v", "ctrl+v"},
		{"tab", "\t", "tab", "tab"},
		{"shift+tab", "\x1b[Z", "shift+tab", "shift+tab"},
		{"esc", "\x1b", "esc", "esc"},
		{"plain 1", "1", "1", "1"},

		// Shifted printable: here String() is the useful spelling and
		// Keystroke() is not — hence dispatch consults both.
		{"kitty shift+/", "\x1b[47:63;2u", "?", "shift+/"},

		// Terminal.app / iTerm2 default: Option composes the character and
		// sends it with NO modifier bit. Genuinely indistinguishable from the
		// user typing "¡"; see the macOS notes on config.KeyConfig.
		{"bare composed ¡ (option-as-input)", "¡", "¡", "¡"},
		{"bare composed ç (option-as-input)", "ç", "ç", "ç"},
		{"bare composed ¬ (option-as-input)", "¬", "¬", "¬"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := decodeKey(t, tc.seq)
			if got := msg.String(); got != tc.wantString {
				t.Errorf("String() = %q, want %q", got, tc.wantString)
			}
			if got := msg.Keystroke(); got != tc.wantKeystroke {
				t.Errorf("Keystroke() = %q, want %q", got, tc.wantKeystroke)
			}
		})
	}
}

// TestKeyPressMatchesEveryAltEncoding is the core regression: every encoding a
// terminal may use for an alt binding must match the single configured
// spelling, and must never be mistaken for text input.
func TestKeyPressMatchesEveryAltEncoding(t *testing.T) {
	cases := []struct {
		binding string
		seqs    []string
	}{
		{"alt+1", []string{"\x1b1", "\x1b[49;3u", "\x1b[49;3;161u", "\x1b[27;3;49~"}},
		{"alt+2", []string{"\x1b2", "\x1b[50;3u", "\x1b[50;3;8482u", "\x1b[27;3;50~"}},
		{"alt+3", []string{"\x1b3", "\x1b[51;3u", "\x1b[51;3;163u", "\x1b[27;3;51~"}},
		{"alt+c", []string{"\x1bc", "\x1b[99;3u", "\x1b[99;3;231u"}},
		{"alt+h", []string{"\x1bh", "\x1b[104;3u", "\x1b[104;3;729u"}},
		{"alt+l", []string{"\x1bl", "\x1b[108;3u", "\x1b[108;3;172u"}},
		{"alt+j", []string{"\x1bj", "\x1b[106;3u", "\x1b[106;3;8710u"}},
		{"alt+k", []string{"\x1bk", "\x1b[107;3u", "\x1b[107;3;730u"}},
	}

	for _, tc := range cases {
		for _, seq := range tc.seqs {
			t.Run(tc.binding+"/"+seq, func(t *testing.T) {
				k := NewPress(decodeKey(t, seq))
				if !k.Matches(tc.binding) {
					t.Errorf("matches(%q) = false for %q (stroke=%q text=%q)",
						tc.binding, seq, k.Stroke(), k.Text())
				}
				if !k.Modified() {
					t.Errorf("modified = false for alt-modified %q", seq)
				}
			})
		}
	}
}

// TestKeyPressMatchesUnmodified covers the other half of the strategy: for
// keys with no modifier the String() spelling is what a user configures, so it
// must still match.
func TestKeyPressMatchesUnmodified(t *testing.T) {
	cases := []struct {
		seq     string
		binding string
	}{
		{"/", "/"},
		{"?", "?"},
		{"\x1b[47:63;2u", "?"},       // kitty shift+/, Keystroke is "shift+/"
		{"\x1b[47:63;2u", "shift+/"}, // ...and the keystroke spelling too
		{"\x1bOP", "f1"},
		{"\x16", "ctrl+v"},
		{"\t", "tab"},
		{"\x1b[Z", "shift+tab"},
		{"\x1b", "esc"},
	}
	for _, tc := range cases {
		t.Run(tc.binding+"/"+tc.seq, func(t *testing.T) {
			if k := NewPress(decodeKey(t, tc.seq)); !k.Matches(tc.binding) {
				t.Errorf("matches(%q) = false (stroke=%q text=%q)", tc.binding, k.Stroke(), k.Text())
			}
		})
	}
}

// TestKeyPressModifiedNeverMatchesBareBinding guards the reason String() is
// only consulted for unmodified keys: a Kitty-reported alt+/ carries Text "/",
// which must not fire a plain "/" binding.
func TestKeyPressModifiedNeverMatchesBareBinding(t *testing.T) {
	k := NewPress(decodeKey(t, "\x1b[47;3;47u")) // alt+/ with text "/"
	if k.Stroke() != "alt+/" {
		t.Fatalf("stroke = %q, want alt+/", k.Stroke())
	}
	if k.Matches("/") {
		t.Error("alt+/ matched the bare \"/\" binding")
	}
	if !k.Matches("alt+/") {
		t.Error("alt+/ did not match the \"alt+/\" binding")
	}
}

// TestKeyPressEmptyBindingInert makes sure an unconfigured (empty) binding
// never matches anything.
func TestKeyPressEmptyBindingInert(t *testing.T) {
	for _, seq := range []string{"\x1b1", "a", "\x1b"} {
		if NewPress(decodeKey(t, seq)).Matches("") {
			t.Errorf("empty binding matched %q", seq)
		}
	}
}

// mainModel returns a Model parked on the main screen with the given focus,
// ready to be driven with decoded key presses. setFocus (rather than a bare
// field assignment) is used so the sub-models' own focused flags match — the
// focused-panel dispatch only reaches a component that believes it is focused.
