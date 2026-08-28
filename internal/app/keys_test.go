package app

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/imtaqin/telegram-cli/internal/config"
	"github.com/imtaqin/telegram-cli/internal/store"
	"github.com/imtaqin/telegram-cli/internal/telegram"
	"github.com/imtaqin/telegram-cli/internal/ui/components/composer"
)

// These tests drive the *real* terminal input decoder rather than hand-built
// tea.KeyPressMsg values. That distinction is the whole point: the alt+1/2/3,
// alt+c and alt+h/alt+l bindings passed every synthetic-message test while
// being dead in a real terminal, because tea.KeyPressMsg.String() returns
// Key.Text whenever the terminal attached any — and the Kitty keyboard
// protocol attaches the macOS Option-composed character ("¡" for Option+1) to
// an alt-modified key press. Only a byte-sequence-level test can catch that.
//
// bubbletea v2's input loop is a thin adapter over uv.EventDecoder
// (charm.land/bubbletea/v2/input.go: uv.KeyPressEvent -> tea.KeyPressMsg), so
// decoding with uv.EventDecoder here reproduces exactly what Update() sees.

// decodeKey runs one raw terminal byte sequence through the ultraviolet event
// decoder and returns it as the bubbletea message the program would receive.
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
				k := newKeyPress(decodeKey(t, seq))
				if !k.matches(tc.binding) {
					t.Errorf("matches(%q) = false for %q (stroke=%q text=%q)",
						tc.binding, seq, k.stroke, k.text)
				}
				if !k.modified {
					t.Errorf("modified = false for alt-modified %q", seq)
				}
				// An alt binding must never be swallowed by quick-type and
				// forwarded to the composer as message text.
				for _, panel := range []FocusPanel{PanelChatView, PanelChatList} {
					if quickTypeTarget(panel, k) {
						t.Errorf("quickTypeTarget(panel %d, %q) = true, want false", panel, seq)
					}
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
			if k := newKeyPress(decodeKey(t, tc.seq)); !k.matches(tc.binding) {
				t.Errorf("matches(%q) = false (stroke=%q text=%q)", tc.binding, k.stroke, k.text)
			}
		})
	}
}

// TestKeyPressModifiedNeverMatchesBareBinding guards the reason String() is
// only consulted for unmodified keys: a Kitty-reported alt+/ carries Text "/",
// which must not fire a plain "/" binding.
func TestKeyPressModifiedNeverMatchesBareBinding(t *testing.T) {
	k := newKeyPress(decodeKey(t, "\x1b[47;3;47u")) // alt+/ with text "/"
	if k.stroke != "alt+/" {
		t.Fatalf("stroke = %q, want alt+/", k.stroke)
	}
	if k.matches("/") {
		t.Error("alt+/ matched the bare \"/\" binding")
	}
	if !k.matches("alt+/") {
		t.Error("alt+/ did not match the \"alt+/\" binding")
	}
}

// TestKeyPressEmptyBindingInert makes sure an unconfigured (empty) binding
// never matches anything.
func TestKeyPressEmptyBindingInert(t *testing.T) {
	for _, seq := range []string{"\x1b1", "a", "\x1b"} {
		if newKeyPress(decodeKey(t, seq)).matches("") {
			t.Errorf("empty binding matched %q", seq)
		}
	}
}

// TestQuickTypeForwardsPlainText keeps the quick-type path working for what it
// is actually for.
func TestQuickTypeForwardsPlainText(t *testing.T) {
	for _, seq := range []string{"a", "z", "1", "?", "¡"} {
		if k := newKeyPress(decodeKey(t, seq)); !quickTypeTarget(PanelChatView, k) {
			t.Errorf("quickTypeTarget(chatview, %q) = false, want true", seq)
		}
	}
}

// mainModel returns a Model parked on the main screen with the given focus,
// ready to be driven with decoded key presses. setFocus (rather than a bare
// field assignment) is used so the sub-models' own focused flags match — the
// focused-panel dispatch only reaches a component that believes it is focused.
func mainModel(t *testing.T, focus FocusPanel) Model {
	t.Helper()
	m := newTestModel(t)
	m.screen = ScreenMain
	m.setFocus(focus)
	return m
}

// openChatModel is mainModel with a chat open in the chat view, which some
// paths require — chatview.OpenFind is a no-op with no chat to search.
func openChatModel(t *testing.T, focus FocusPanel) Model {
	t.Helper()
	m := mainModel(t, focus)
	m.chatView.OpenChat(testChatID, "Test Chat")
	return m
}

const testChatID int64 = 4242

func update(t *testing.T, m Model, seq string) Model {
	t.Helper()
	next, _ := updateCmd(t, m, seq)
	return next
}

// updateCmd is update, but also hands back the command so a test can inspect
// a key that is re-dispatched rather than acted on directly.
func updateCmd(t *testing.T, m Model, seq string) (Model, tea.Cmd) {
	t.Helper()
	out, cmd := m.Update(decodeKey(t, seq))
	next, ok := out.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want app.Model", out)
	}
	return next, cmd
}

// TestUpdateFocusBindingsFromRawSequences drives Update() with the raw bytes a
// terminal sends and asserts the panel actually moves.
func TestUpdateFocusBindingsFromRawSequences(t *testing.T) {
	cases := []struct {
		name string
		seq  string
		want FocusPanel
	}{
		{"legacy alt+1", "\x1b1", PanelChatList},
		{"kitty alt+1", "\x1b[49;3u", PanelChatList},
		{"kitty alt+1 with composed text", "\x1b[49;3;161u", PanelChatList},
		{"modifyOtherKeys alt+1", "\x1b[27;3;49~", PanelChatList},
		{"f1", "\x1bOP", PanelChatList},

		{"legacy alt+2", "\x1b2", PanelChatView},
		{"kitty alt+2 with composed text", "\x1b[50;3;8482u", PanelChatView},
		{"f2", "\x1bOQ", PanelChatView},

		{"legacy alt+3", "\x1b3", PanelComposer},
		{"kitty alt+3 with composed text", "\x1b[51;3;163u", PanelComposer},
		{"f3", "\x1bOR", PanelComposer},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Start from a panel other than the target so a no-op cannot pass.
			start := PanelGroupInfo
			m := update(t, mainModel(t, start), tc.seq)
			if m.focus != tc.want {
				t.Errorf("focus = %v, want %v", m.focus, tc.want)
			}
		})
	}
}

// TestUpdateContactsToggleFromRawSequences covers alt+c in every encoding.
func TestUpdateContactsToggleFromRawSequences(t *testing.T) {
	for _, seq := range []string{"\x1bc", "\x1b[99;3u", "\x1b[99;3;231u"} {
		t.Run(seq, func(t *testing.T) {
			m := update(t, mainModel(t, PanelChatList), seq)
			if !m.contacts.IsVisible() {
				t.Fatalf("contacts not visible after %q", seq)
			}
			if m.focus != PanelContacts {
				t.Errorf("focus = %v, want PanelContacts", m.focus)
			}
			// And it toggles back off.
			m = update(t, m, seq)
			if m.contacts.IsVisible() {
				t.Errorf("contacts still visible after second %q", seq)
			}
		})
	}
}

// TestFolderAndChatNavBindingsResolve pins the folder/chat-nav keys to the
// resolved bindings Update() gates CycleFolder/SelectDelta on. The chat list's
// folder index lives in another package and is not observable from here, so
// the guard condition itself is asserted.
func TestFolderAndChatNavBindingsResolve(t *testing.T) {
	m := mainModel(t, PanelChatList)
	cases := []struct {
		seq     string
		binding string
	}{
		{"\x1bh", m.keys.prevFolder},
		{"\x1b[104;3u", m.keys.prevFolder},
		{"\x1b[104;3;729u", m.keys.prevFolder},
		{"\x1bl", m.keys.nextFolder},
		{"\x1b[108;3u", m.keys.nextFolder},
		{"\x1b[108;3;172u", m.keys.nextFolder},
		{"\x1bj", m.keys.nextChat},
		{"\x1b[106;3;8710u", m.keys.nextChat},
		{"\x1bk", m.keys.prevChat},
		{"\x1b[107;3;730u", m.keys.prevChat},
	}
	for _, tc := range cases {
		t.Run(tc.binding+"/"+tc.seq, func(t *testing.T) {
			if !newKeyPress(decodeKey(t, tc.seq)).matches(tc.binding) {
				t.Errorf("%q did not match binding %q", tc.seq, tc.binding)
			}
		})
	}
}

// TestBareComposedOptionCharIsUndetectable documents the one case that cannot
// be fixed in code: a terminal that swallows Option and emits only the
// composed character leaves nothing to match on. See the macOS notes on
// config.KeyConfig.
func TestBareComposedOptionCharIsUndetectable(t *testing.T) {
	k := newKeyPress(decodeKey(t, "¡"))
	if k.modified {
		t.Fatal("bare composed char unexpectedly carries a modifier")
	}
	if k.matches("alt+1") {
		t.Error("bare \"¡\" matched alt+1 — the decoder now reports a modifier; " +
			"revisit the macOS notes on config.KeyConfig")
	}
}

// TestChatViewSearchModeOwnsKeys covers the in-chat search (ctrl+f) yield: once
// chatview's search input is open, app.go must stop consuming keys on its way
// through — esc has to close the search instead of moving focus, and a plain
// printable must not be treated as a panel shortcut.
func TestChatViewSearchModeOwnsKeys(t *testing.T) {
	// Sanity: without search active, "i" from chatview jumps to the composer.
	// This is the behavior the yield below has to suppress, so a passing
	// assertion afterwards means something.
	if m := update(t, mainModel(t, PanelChatView), "i"); m.focus != PanelComposer {
		t.Fatalf("precondition: focus = %v after \"i\", want PanelComposer", m.focus)
	}

	// A chat has to be open: the find input has nothing to search without
	// one, and chatview makes both ctrl+f and OpenFind no-ops in that state.
	m := openChatModel(t, PanelChatView)
	m = update(t, m, "\x06") // ctrl+f
	if !m.chatView.SearchActive() {
		t.Fatal("ctrl+f did not open chatview's search input")
	}

	// A printable that is otherwise an app shortcut stays with the search.
	m = update(t, m, "i")
	if m.focus != PanelChatView {
		t.Errorf("focus = %v after \"i\" in search mode, want PanelChatView", m.focus)
	}
	if !m.chatView.SearchActive() {
		t.Error("search closed after a printable key")
	}

	// Esc belongs to the search input, not to app.go's focus-back handler.
	m = update(t, m, "\x1b")
	if m.focus != PanelChatView {
		t.Errorf("focus = %v after esc in search mode, want PanelChatView", m.focus)
	}
	if m.chatView.SearchActive() {
		t.Error("esc did not close chatview's search input")
	}

	// With the search closed, normal app dispatch resumes.
	if m = update(t, m, "i"); m.focus != PanelComposer {
		t.Errorf("focus = %v after \"i\" post-search, want PanelComposer", m.focus)
	}
}

// TestMatchCycleKeysAreUnmodifiedPrintables pins the key half of the n/N
// yield. The guard is `focus == PanelChatView && HasSearchResults()`, and
// HasSearchResults only becomes true after a live search against Telegram,
// which is out of reach here — so the match test is asserted directly.
func TestMatchCycleKeysAreUnmodifiedPrintables(t *testing.T) {
	for _, seq := range []string{"n", "N", "\x1b[110:78;2u" /* kitty shift+n */} {
		k := newKeyPress(decodeKey(t, seq))
		if !k.matches("n", "N") {
			t.Errorf("%q did not match n/N (stroke=%q text=%q)", seq, k.stroke, k.text)
		}
	}
	// Modified variants must not be mistaken for the match-cycling keys, so
	// alt+n stays available to app-level bindings.
	for _, seq := range []string{"\x1bn", "\x1b[110;3u", "\x0e" /* ctrl+n */} {
		k := newKeyPress(decodeKey(t, seq))
		if k.matches("n", "N") {
			t.Errorf("%q wrongly matched n/N (stroke=%q text=%q)", seq, k.stroke, k.text)
		}
	}
}

// TestNoSearchResultsDoesNotYield makes sure the n/N yield is gated on there
// actually being hits: with none, chatview is not in a special mode and app
// shortcuts keep working.
func TestNoSearchResultsDoesNotYield(t *testing.T) {
	m := mainModel(t, PanelChatView)
	if m.chatView.HasSearchResults() {
		t.Fatal("fresh chatview unexpectedly reports search results")
	}
	if got := update(t, m, "i"); got.focus != PanelComposer {
		t.Errorf("focus = %v, want PanelComposer", got.focus)
	}
}

// TestSlashIsContextual covers the vi reading of "/": in the chat view it
// finds within the open chat (forwarded as the ctrl+f chatview already
// binds), everywhere else it opens the global message search.
func TestSlashIsContextual(t *testing.T) {
	t.Run("chatview finds in chat", func(t *testing.T) {
		m, cmd := updateCmd(t, openChatModel(t, PanelChatView), "/")
		if m.search.IsVisible() {
			t.Error("global search overlay opened from the chat view")
		}
		if !m.chatView.SearchActive() {
			t.Error("chatview's in-chat search did not open")
		}
		if m.focus != PanelChatView {
			t.Errorf("focus = %v, want PanelChatView", m.focus)
		}
		// The find must be opened by calling into chatview, not by putting
		// another key back on the command loop — see the self-referential
		// case below for why that distinction matters.
		if cmd != nil {
			if _, ok := cmd().(tea.KeyPressMsg); ok {
				t.Error("\"/\" re-emitted a key event instead of calling OpenFind")
			}
		}
	})

	// Regression for the livelock: with keys.search = "ctrl+f", forwarding a
	// synthetic ctrl+f made the binding match itself and spin the command
	// loop forever. The direct OpenFind call has no such feedback path.
	t.Run("self-referential binding does not livelock", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Keys.Search = "ctrl+f"
		m := openChatModel(t, PanelChatView)
		m.keys = resolveKeys(cfg.Keys)
		if m.keys.search != "ctrl+f" {
			t.Fatalf("precondition: keys.search = %q, want ctrl+f", m.keys.search)
		}

		next, cmd := updateCmd(t, m, "\x1b[102;5u") // ctrl+f
		if !next.chatView.SearchActive() {
			t.Error("in-chat find did not open")
		}
		if cmd != nil {
			if msg, ok := cmd().(tea.KeyPressMsg); ok {
				t.Fatalf("re-emitted %q — this is the livelock", msg.Keystroke())
			}
		}
	})

	t.Run("chatlist searches globally", func(t *testing.T) {
		m := update(t, mainModel(t, PanelChatList), "/")
		if !m.search.IsVisible() || m.focus != PanelSearch {
			t.Errorf("visible=%v focus=%v, want the global search overlay",
				m.search.IsVisible(), m.focus)
		}
	})
}

// TestGlobalSearchBinding keeps the global overlay reachable from the chat
// view, where "/" now means "find in this chat".
func TestGlobalSearchBinding(t *testing.T) {
	for _, panel := range []FocusPanel{PanelChatView, PanelChatList} {
		m := update(t, mainModel(t, panel), "\x07") // ctrl+g
		if !m.search.IsVisible() || m.focus != PanelSearch {
			t.Errorf("panel %v: visible=%v focus=%v, want the global search overlay",
				panel, m.search.IsVisible(), m.focus)
		}
	}
}

// TestContactsAltBinding covers the alt-free contacts binding added for
// terminals that cannot report Option/Alt (Ghostty's macos-option-as-alt
// default, Terminal.app, iTerm2).
func TestContactsAltBinding(t *testing.T) {
	for _, seq := range []string{"\x1bOS" /* f4 */, "\x1bc" /* alt+c */} {
		t.Run(seq, func(t *testing.T) {
			m := update(t, mainModel(t, PanelChatList), seq)
			if !m.contacts.IsVisible() || m.focus != PanelContacts {
				t.Fatalf("contacts not opened by %q", seq)
			}
			if m = update(t, m, seq); m.contacts.IsVisible() {
				t.Errorf("contacts still visible after a second %q", seq)
			}
		})
	}
}

// TestCtrlKIsNotAnAppBinding guards the reason ctrl+k was rejected as the
// alt-free contacts key: the composer's textarea binds it to readline
// kill-to-end-of-line, and app-level bindings are checked first, so claiming
// it here would silently break line editing.
func TestCtrlKIsNotAnAppBinding(t *testing.T) {
	m := mainModel(t, PanelComposer)
	k := newKeyPress(decodeKey(t, "\x0b")) // ctrl+k
	if k.stroke != "ctrl+k" {
		t.Fatalf("stroke = %q, want ctrl+k", k.stroke)
	}
	for name, binding := range map[string]string{
		"contacts":     m.keys.contacts,
		"contacts_alt": m.keys.contactsAlt,
		"search":       m.keys.search,
		"globalSearch": m.keys.globalSearch,
		"quit":         m.keys.quit,
		"nextFolder":   m.keys.nextFolder,
		"prevFolder":   m.keys.prevFolder,
		"nextChat":     m.keys.nextChat,
		"prevChat":     m.keys.prevChat,
	} {
		if k.matches(binding) {
			t.Errorf("ctrl+k is bound to %s", name)
		}
	}
	if got := update(t, m, "\x0b"); got.contacts.IsVisible() || got.focus != PanelComposer {
		t.Errorf("ctrl+k disturbed the composer: contacts=%v focus=%v",
			got.contacts.IsVisible(), got.focus)
	}
}

// TestViFolderKeysOnlyInChatList pins the h/l trade-off: they cycle the folder
// tabs (and are therefore not quick-typed) while the chat list is focused, and
// stay ordinary message text everywhere else. The chat list's folder index is
// unexported in another package, so the guard conditions are asserted rather
// than the resulting tab.
func TestViFolderKeysOnlyInChatList(t *testing.T) {
	for _, seq := range []string{"h", "l"} {
		k := newKeyPress(decodeKey(t, seq))
		if quickTypeTarget(PanelChatList, k) {
			t.Errorf("%q is still quick-typed from the chat list", seq)
		}
		if !quickTypeTarget(PanelChatView, k) {
			t.Errorf("%q stopped being quick-typed from the chat view", seq)
		}
	}
	// The alt spellings keep working from every panel.
	m := mainModel(t, PanelChatView)
	for _, seq := range []string{"\x1bh", "\x1b[104;3;729u", "\x1bl", "\x1b[108;3;172u"} {
		k := newKeyPress(decodeKey(t, seq))
		if !k.matches(m.keys.prevFolder, m.keys.nextFolder) {
			t.Errorf("%q no longer matches a folder binding", seq)
		}
	}
	// Quick-type still works for the other printables in the chat list.
	for _, seq := range []string{"a", "z", "?"} {
		if !quickTypeTarget(PanelChatList, newKeyPress(decodeKey(t, seq))) {
			t.Errorf("%q stopped being quick-typed from the chat list", seq)
		}
	}
}

// TestResolveKeysDefaults pins the defaults app.go falls back to when a
// config predates a field (or is the zero value used by these tests).
func TestResolveKeysDefaults(t *testing.T) {
	want := map[string]string{
		"quit": "ctrl+c", "focusChatList": "f1", "focusChatView": "f2",
		"focusComposer": "f3", "search": "/", "globalSearch": "ctrl+g",
		"contacts": "alt+c", "contactsAlt": "f4", "nextFolder": "alt+l",
		"prevFolder": "alt+h", "nextChat": "alt+j", "prevChat": "alt+k",
	}
	k := resolveKeys(config.KeyConfig{})
	got := map[string]string{
		"quit": k.quit, "focusChatList": k.focusChatList, "focusChatView": k.focusChatView,
		"focusComposer": k.focusComposer, "search": k.search, "globalSearch": k.globalSearch,
		"contacts": k.contacts, "contactsAlt": k.contactsAlt, "nextFolder": k.nextFolder,
		"prevFolder": k.prevFolder, "nextChat": k.nextChat, "prevChat": k.prevChat,
	}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("%s = %q, want %q", name, got[name], w)
		}
	}
	// A configured value wins and is normalized on the way in.
	if k := resolveKeys(config.KeyConfig{Contacts: "Option+C"}); k.contacts != "alt+c" {
		t.Errorf("configured contacts = %q, want alt+c", k.contacts)
	}
}

// TestComposerKeepsItsChords is the audit backing the "app-level dispatch
// claims almost nothing while the composer is focused" contract in keymap.go.
// The composer's line editing (emacs readline chords, vi mode, ctrl+j for a
// newline, ctrl+o for $EDITOR) only works if app.go lets those keys through,
// and app-level bindings are checked first — so every ctrl+<letter> except
// the three documented exceptions must leave the app state untouched.
//
// Keys are built as Kitty CSI-u sequences (CSI <code>;5u, modifier 5 = ctrl)
// rather than raw control bytes, because the legacy bytes for ctrl+i, ctrl+m
// and ctrl+[ are indistinguishable from tab, enter and esc.
func TestComposerKeepsItsChords(t *testing.T) {
	// ctrl+c and ctrl+q quit; ctrl+v pastes. Everything else falls through.
	exceptions := map[rune]bool{'c': true, 'q': true, 'v': true}

	for r := 'a'; r <= 'z'; r++ {
		if exceptions[r] {
			continue
		}
		seq := fmt.Sprintf("\x1b[%d;5u", r)
		t.Run(string(r), func(t *testing.T) {
			k := newKeyPress(decodeKey(t, seq))
			if want := "ctrl+" + string(r); k.stroke != want {
				t.Fatalf("stroke = %q, want %q", k.stroke, want)
			}
			m := update(t, mainModel(t, PanelComposer), seq)
			if m.focus != PanelComposer {
				t.Errorf("ctrl+%c moved focus to %v", r, m.focus)
			}
			if m.search.IsVisible() {
				t.Errorf("ctrl+%c opened the global search overlay", r)
			}
			if m.contacts.IsVisible() {
				t.Errorf("ctrl+%c opened the contacts overlay", r)
			}
		})
	}

	// The two chords the concurrent composer work depends on, called out by
	// name so a future binding that claims them fails loudly here.
	for _, tc := range []struct{ seq, stroke string }{
		{"\x0a", "ctrl+j"}, // newline
		{"\x0f", "ctrl+o"}, // external editor
	} {
		k := newKeyPress(decodeKey(t, tc.seq))
		if k.stroke != tc.stroke {
			t.Fatalf("stroke = %q, want %q", k.stroke, tc.stroke)
		}
		if m := update(t, mainModel(t, PanelComposer), tc.seq); m.focus != PanelComposer {
			t.Errorf("%s moved focus to %v", tc.stroke, m.focus)
		}
	}
}

// TestSearchKeysYieldToOpenOverlays covers a defect the contextual-"/" work
// exposed: while an overlay owns a text input, "/" has to be typable into it
// rather than re-triggering the binding that opened it.
func TestSearchKeysYieldToOpenOverlays(t *testing.T) {
	// Open the global search from the chat list, then type "/" into it.
	m := update(t, mainModel(t, PanelChatList), "/")
	if !m.search.IsVisible() {
		t.Fatal("precondition: global search did not open")
	}
	if m = update(t, m, "/"); m.focus != PanelSearch {
		t.Errorf("focus = %v after \"/\" inside the search overlay, want PanelSearch", m.focus)
	}

	// Same for the contacts overlay's filter.
	c := update(t, mainModel(t, PanelChatList), "\x1bc")
	if !c.contacts.IsVisible() {
		t.Fatal("precondition: contacts did not open")
	}
	if c = update(t, c, "/"); c.search.IsVisible() {
		t.Error("\"/\" inside the contacts overlay opened the global search")
	}
}

// TestChatListFolderKeysNotQuickTyped covers the alt-free chat-list bindings
// chatlist implements internally (left/right arrows and the 1-9 folder jump).
// They only ever reach chatlist if app.go's quick-type does not claim them
// first, which for the digits it previously did.
func TestChatListFolderKeysNotQuickTyped(t *testing.T) {
	for _, seq := range []string{"1", "2", "3", "4", "5", "6", "7", "8", "9"} {
		k := newKeyPress(decodeKey(t, seq))
		if quickTypeTarget(PanelChatList, k) {
			t.Errorf("digit %q is still quick-typed from the chat list", seq)
		}
		// Digits stay ordinary text everywhere else.
		if !quickTypeTarget(PanelChatView, k) {
			t.Errorf("digit %q stopped being quick-typed from the chat view", seq)
		}
	}
	// "0" has no folder to jump to, so it stays typable.
	if !quickTypeTarget(PanelChatList, newKeyPress(decodeKey(t, "0"))) {
		t.Error(`"0" should still be quick-typed from the chat list`)
	}
	// Arrows are not printable, so quick-type never claimed them.
	for _, seq := range []string{"\x1b[D", "\x1b[C"} { // left, right
		if quickTypeTarget(PanelChatList, newKeyPress(decodeKey(t, seq))) {
			t.Errorf("arrow %q is quick-typed from the chat list", seq)
		}
	}
	// And the alt+1/2/3 focus bindings must still win over the digit jump,
	// since they are matched before dispatch reaches chatlist.
	m := mainModel(t, PanelChatList)
	for _, tc := range []struct {
		seq  string
		want FocusPanel
	}{
		{"\x1b1", PanelChatList}, {"\x1b[50;3;8482u", PanelChatView}, {"\x1b3", PanelComposer},
	} {
		if got := update(t, m, tc.seq); got.focus != tc.want {
			t.Errorf("%q: focus = %v, want %v", tc.seq, got.focus, tc.want)
		}
	}
}

// TestComposerEditingModeFromConfig covers the wiring between
// ui.compose_editing and the composer's editing mode. The resolution rules
// themselves live in config and are tested there; this pins the translation
// and that New() actually applies it.
func TestComposerEditingModeFromConfig(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")

	cases := []struct {
		setting string
		editor  string
		want    composer.EditingMode
	}{
		{"vi", "", composer.ModeVi},
		{"emacs", "vim", composer.ModeEmacs},
		{"auto", "vim", composer.ModeVi},
		{"auto", "nano", composer.ModeEmacs},
		{"", "", composer.ModeEmacs},
	}
	for _, tc := range cases {
		t.Run(tc.setting+"/"+tc.editor, func(t *testing.T) {
			t.Setenv("EDITOR", tc.editor)
			if got := composerEditingMode(tc.setting); got != tc.want {
				t.Errorf("composerEditingMode(%q) with EDITOR=%q = %v, want %v",
					tc.setting, tc.editor, got, tc.want)
			}
		})
	}

	// New() applies it rather than leaving the composer on its default.
	t.Setenv("EDITOR", "")
	cfg := &config.Config{}
	cfg.UI.ComposeEditing = config.ComposeEditingVi
	m := New(cfg, nil, store.NewStore(), telegram.NewTUIAuthorizer(cfg))
	if got := m.composer.EditingMode(); got != composer.ModeVi {
		t.Errorf("New() left the composer in mode %v, want ModeVi", got)
	}
}
