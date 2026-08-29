package app

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/imtaqin/telegram-cli/internal/config"
	"github.com/imtaqin/telegram-cli/internal/store"
	"github.com/imtaqin/telegram-cli/internal/telegram"
	"github.com/imtaqin/telegram-cli/internal/ui/components/composer"
	"github.com/imtaqin/telegram-cli/internal/ui/components/help"
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

// mainModel returns a Model parked on the main screen with the given focus,
// ready to be driven with decoded key presses. setFocus (rather than a bare
// field assignment) is used so the sub-models' own focused flags match — the
// focused-panel dispatch only reaches a component that believes it is focused.
func mainModel(t *testing.T, focus FocusPanel) Model {
	t.Helper()
	m := newTestModel(t)
	m.screen = ScreenMain
	m.setFocus(focus)
	// Pin the line-editing keymap: New() otherwise infers it from the
	// developer's $EDITOR, and vi mode changes what esc means in the
	// composer (first esc leaves insert mode, only the second leaves the
	// panel). The vi behavior is the composer package's to test.
	m.composer.SetEditingMode(composer.ModeEmacs)
	return m
}

// openChatModel is mainModel with a chat open, which several paths require:
// chatview.OpenFind is a no-op with nothing to search, and i/c refuse to
// focus a composer that has no chat to send to.
func openChatModel(t *testing.T, focus FocusPanel) Model {
	t.Helper()
	m := mainModel(t, focus)
	m.chatView.OpenChat(testChatID, "Test Chat")
	m.composer.SetChatId(testChatID)
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
	if m := update(t, openChatModel(t, PanelChatView), "i"); m.focus != PanelComposer {
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
	m := openChatModel(t, PanelChatView)
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

// TestFolderCyclingKeys covers the alt-free folder bindings app.go dispatches:
// vi h/l and lazygit [/], both gated to the chat list, alongside the alt
// spellings that work from anywhere. The chat list's folder index is
// unexported in another package, so the guard conditions are asserted rather
// than the resulting tab.
func TestFolderCyclingKeys(t *testing.T) {
	// normalizeFolders always sorts the implicit "All" folder to index 0 and
	// keeps the rest in server order, so the tab ring is 0 -> 7 -> 9 -> 0.
	const work, family int32 = 7, 9
	seedFolders := func(t *testing.T, m Model) Model {
		t.Helper()
		m = send(t, m, telegram.ChatFoldersMsg{Folders: []*telegram.ChatFolder{
			{ID: telegram.AllChatsFolderID, Title: "All"},
			{ID: work, Title: "Work"},
			{ID: family, Title: "Family"},
		}})
		if got := m.chatList.ActiveFolderID(); got != telegram.AllChatsFolderID {
			t.Fatalf("precondition: active folder = %d, want All (%d)",
				got, telegram.AllChatsFolderID)
		}
		return m
	}

	// The alt spellings keep working, in every encoding a terminal uses.
	base := sizedMainModel(t)
	for _, seq := range []string{"\x1bh", "\x1b[104;3;729u", "\x1bl", "\x1b[108;3;172u"} {
		if k := newKeyPress(decodeKey(t, seq)); !k.matches(base.keys.prevFolder, base.keys.nextFolder) {
			t.Errorf("%q no longer matches a folder binding", seq)
		}
	}

	// Each spelling walks the whole ring and wraps, so a key that is merely
	// swallowed rather than acted on fails here.
	forward := []int32{work, family, telegram.AllChatsFolderID}
	backward := []int32{family, work, telegram.AllChatsFolderID}
	for _, tc := range []struct {
		name string
		seq  string
		want []int32
	}{
		{"vi next", "l", forward},
		{"lazygit next", "]", forward},
		{"alt next", "\x1bl", forward},
		{"vi prev", "h", backward},
		{"lazygit prev", "[", backward},
		{"alt prev", "\x1bh", backward},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := seedFolders(t, sizedMainModel(t))
			for i, want := range tc.want {
				m = update(t, m, tc.seq)
				if got := m.chatList.ActiveFolderID(); got != want {
					t.Fatalf("press %d of %q: active folder = %d, want %d",
						i+1, tc.seq, got, want)
				}
			}
		})
	}

	// Cycling is consumed here: no focus move, no overlay, and nothing put
	// back on the command loop.
	m := seedFolders(t, sizedMainModel(t))
	for _, seq := range []string{"h", "l", "[", "]"} {
		got, cmd := updateCmd(t, m, seq)
		if got.focus != PanelChatList {
			t.Errorf("%q moved focus to %v", seq, got.focus)
		}
		if got.search.IsVisible() || got.contacts.IsVisible() || got.help.IsVisible() {
			t.Errorf("%q opened an overlay", seq)
		}
		if cmd != nil {
			if _, isKey := cmd().(tea.KeyPressMsg); isKey {
				t.Errorf("%q was re-emitted instead of handled", seq)
			}
		}
	}

	// The bare aliases are chat-list only: from the chat view they must not
	// touch the folder tabs, so they stay available to that panel. The alt
	// spellings still work from anywhere.
	view := seedFolders(t, sizedMainModel(t, PanelChatView))
	for _, seq := range []string{"h", "l", "[", "]"} {
		got := update(t, view, seq)
		if got.focus != PanelChatView {
			t.Errorf("%q from the chat view moved focus to %v", seq, got.focus)
		}
		if id := got.chatList.ActiveFolderID(); id != telegram.AllChatsFolderID {
			t.Errorf("%q cycled the folder tab from the chat view (now %d)", seq, id)
		}
	}
	if got := update(t, view, "\x1bl"); got.chatList.ActiveFolderID() != work {
		t.Errorf("alt+l from the chat view: active folder = %d, want %d",
			got.chatList.ActiveFolderID(), work)
	}
}

// TestChatListKeysReachChatList covers the bindings chatlist implements
// itself — left/right arrows and the 1-9 folder jump. They only work if
// app.go does not consume them on the way past, so this asserts app-level
// dispatch claims none of them.
func TestChatListKeysReachChatList(t *testing.T) {
	m := mainModel(t, PanelChatList)
	appBindings := []string{
		m.keys.quit, m.keys.focusChatList, m.keys.focusChatView, m.keys.focusComposer,
		m.keys.search, m.keys.globalSearch, m.keys.contacts, m.keys.contactsAlt,
		m.keys.nextFolder, m.keys.prevFolder, m.keys.nextChat, m.keys.prevChat,
		m.keys.help,
	}
	seqs := []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "0",
		"\x1b[D" /* left */, "\x1b[C" /* right */}
	for _, seq := range seqs {
		k := newKeyPress(decodeKey(t, seq))
		if k.matches(appBindings...) {
			t.Errorf("%q collides with an app-level binding (stroke=%q)", seq, k.stroke)
		}
		got := update(t, m, seq)
		if got.focus != PanelChatList {
			t.Errorf("%q moved focus to %v instead of reaching chatlist", seq, got.focus)
		}
		if got.search.IsVisible() || got.contacts.IsVisible() {
			t.Errorf("%q opened an overlay instead of reaching chatlist", seq)
		}
	}

	// The alt+1/2/3 focus bindings must still outrank the digit jump.
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

// TestComposeRequiresAnExplicitMove covers the removal of quick-type. A
// printable key used to jump to the composer and become message text, which
// made every single-character binding a trade-off against typing that
// character — and made a stray keystroke silently become a message. Typing is
// now always entered deliberately.
func TestComposeRequiresAnExplicitMove(t *testing.T) {
	for _, panel := range []FocusPanel{PanelChatList, PanelChatView} {
		m := openChatModel(t, panel)
		for _, seq := range []string{"a", "z", "q", "?", "0", "¡", " "} {
			if got := update(t, m, seq); got.focus == PanelComposer {
				t.Errorf("panel %v: %q jumped to the composer", panel, seq)
			}
		}
		// i and c are the deliberate ways in, from both browsing panels.
		for _, seq := range []string{"i", "c"} {
			if got := update(t, m, seq); got.focus != PanelComposer {
				t.Errorf("panel %v: %q did not focus the composer (got %v)", panel, seq, got.focus)
			}
		}
	}
	// alt+c is still the contacts overlay, not compose — the modifier is
	// what separates them.
	if got := update(t, mainModel(t, PanelChatList), "\x1bc"); !got.contacts.IsVisible() {
		t.Error("alt+c no longer opens the contacts overlay")
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

// TestComposerFocusIsSticky covers the send-and-keep-typing behavior: a chat
// is a run of messages, so focus stays put after a send and esc is the way
// out. Nothing in the submit path may move it.
func TestComposerFocusIsSticky(t *testing.T) {
	m := openChatModel(t, PanelComposer)
	m.composer.SetChatId(testChatID)

	m = send(t, m, composer.MessageSubmittedMsg{ChatId: testChatID, Text: "hello"})
	if m.focus != PanelComposer {
		t.Errorf("focus = %v after a send, want PanelComposer", m.focus)
	}

	// A second send from the same state behaves the same.
	m = send(t, m, composer.MessageSubmittedMsg{ChatId: testChatID, Text: "and again"})
	if m.focus != PanelComposer {
		t.Errorf("focus = %v after a second send, want PanelComposer", m.focus)
	}

	// Esc steps out, once the composer has nothing of its own to cancel.
	if m = update(t, m, "\x1b"); m.focus != PanelChatView {
		t.Errorf("focus = %v after esc, want PanelChatView", m.focus)
	}
}

// TestHelpOverlay covers the "?" overlay: it opens from the browsing panels,
// owns the keyboard while up, closes on esc/?/q, and is never opened from the
// composer, where "?" is a character someone wants to type.
func TestHelpOverlay(t *testing.T) {
	for _, panel := range []FocusPanel{PanelChatList, PanelChatView} {
		m := update(t, mainModel(t, panel), "?")
		if !m.help.IsVisible() {
			t.Errorf("panel %v: \"?\" did not open the help overlay", panel)
		}
		// Focus is untouched — the overlay is modal, not a panel.
		if m.focus != panel {
			t.Errorf("panel %v: opening help moved focus to %v", panel, m.focus)
		}
	}

	// From the composer "?" is text, not a binding.
	if m := update(t, mainModel(t, PanelComposer), "?"); m.help.IsVisible() {
		t.Error("\"?\" opened the help overlay from the composer")
	}

	// While it is up, keys that would otherwise act are swallowed.
	m := update(t, mainModel(t, PanelChatList), "?")
	for _, seq := range []string{"\x1b2" /* alt+2 */, "\x1bc" /* alt+c */, "i", "/"} {
		got := update(t, m, seq)
		if got.focus != PanelChatList {
			t.Errorf("%q moved focus to %v behind the help overlay", seq, got.focus)
		}
		if got.contacts.IsVisible() || got.search.IsVisible() {
			t.Errorf("%q opened another overlay behind the help overlay", seq)
		}
		if !got.help.IsVisible() {
			t.Errorf("%q closed the help overlay", seq)
		}
	}

	// Scroll keys reach the component instead of being swallowed.
	if got := update(t, m, "j"); !got.help.IsVisible() {
		t.Error("j closed the help overlay instead of scrolling it")
	}

	// Each of the closing keys works.
	for _, seq := range []string{"\x1b" /* esc */, "?", "q"} {
		if got := update(t, m, seq); got.help.IsVisible() {
			t.Errorf("%q did not close the help overlay", seq)
		}
	}

	// Quit still outranks it — the overlay must not trap the user.
	if _, cmd := m.Update(decodeKey(t, "\x03")); cmd == nil {
		t.Error("ctrl+c produced no command while the help overlay was open")
	}
}

// TestHelpSectionsComeFromResolvedKeys is the anti-drift check: the overlay
// must describe the bindings the dispatcher actually matches, so rebinding a
// key has to change what the overlay says.
func TestHelpSectionsComeFromResolvedKeys(t *testing.T) {
	cfg := &config.Config{}
	cfg.Keys.Contacts = "alt+p"
	cfg.Keys.GlobalSearch = "ctrl+y"
	cfg.Keys.Help = "f12"
	cfg.Keys.Quit = "ctrl+x"
	m := New(cfg, nil, store.NewStore(), telegram.NewTUIAuthorizer(cfg))

	var all string
	for _, sec := range m.helpSections() {
		if sec.Title == "" {
			t.Error("a help section has no title")
		}
		if len(sec.Bindings) == 0 {
			t.Errorf("help section %q has no bindings", sec.Title)
		}
		for _, b := range sec.Bindings {
			if b.Keys == "" || b.Desc == "" {
				t.Errorf("section %q has an incomplete binding %+v", sec.Title, b)
			}
			all += b.Keys + "\x00"
		}
	}

	for _, want := range []string{"alt+p", "ctrl+y", "f12", "ctrl+x"} {
		if !strings.Contains(all, want) {
			t.Errorf("the overlay does not mention the configured %q", want)
		}
	}
	// The defaults these replaced must be gone, or the overlay is lying.
	for _, gone := range []string{"alt+c", "ctrl+g"} {
		if strings.Contains(all, gone) {
			t.Errorf("the overlay still advertises the replaced default %q", gone)
		}
	}

	// The hardcoded quit keys are always live, so they belong on the row
	// even when a third is configured.
	if row, ok := findBinding(m.helpSections(), "Quit"); !ok {
		t.Error("no Quit row")
	} else {
		for _, want := range []string{"ctrl+c", "ctrl+q", "ctrl+x"} {
			if !strings.Contains(row.Keys, want) {
				t.Errorf("Quit row %q omits %q", row.Keys, want)
			}
		}
	}

	// The footer names the configured help key, not a stale "?".
	if got := m.helpFooter(); !strings.Contains(got, "f12") || strings.Contains(got, "?") {
		t.Errorf("footer = %q, want it to name the configured help key", got)
	}
	for _, want := range []string{"esc", "q"} {
		if !strings.Contains(m.helpFooter(), want) {
			t.Errorf("footer %q omits the %q close key", m.helpFooter(), want)
		}
	}
}

// findBinding returns the first binding whose Desc starts with prefix.
func findBinding(sections []help.Section, descPrefix string) (help.Binding, bool) {
	for _, sec := range sections {
		for _, b := range sec.Bindings {
			if strings.HasPrefix(b.Desc, descPrefix) {
				return b, true
			}
		}
	}
	return help.Binding{}, false
}

// TestHelpDescriptionsMatchBehavior pins the rows the review found lying:
// each assertion drives the key and checks the overlay's wording against what
// actually happened.
func TestHelpDescriptionsMatchBehavior(t *testing.T) {
	m := openChatModel(t, PanelComposer)
	sections := m.helpSections()

	t.Run("tab really is global", func(t *testing.T) {
		row, ok := findBinding(sections, "Cycle panel focus")
		if !ok {
			t.Fatal("no panel-cycling row")
		}
		if !strings.Contains(row.Keys, "tab") {
			t.Fatalf("row keys = %q, want tab", row.Keys)
		}
		// Advertised without a caveat, so it must work from every panel
		// including the one the card is least likely to be read from.
		for _, panel := range []FocusPanel{PanelComposer, PanelChatList, PanelChatView} {
			if got := update(t, openChatModel(t, panel), "\t"); got.focus == panel {
				t.Errorf("tab did nothing from panel %v", panel)
			}
		}
	})

	t.Run("ctrl+g says it is not available while composing", func(t *testing.T) {
		row, ok := findBinding(sections, "Search all chats")
		if !ok {
			t.Fatal("no global-search row")
		}
		if !strings.Contains(row.Desc, "composing") {
			t.Errorf("Desc = %q, want it to note the composer exclusion", row.Desc)
		}
		// Which is the truth: it opens elsewhere, not from the composer.
		if got := update(t, openChatModel(t, PanelComposer), "\x07"); got.search.IsVisible() {
			t.Error("ctrl+g opened the search overlay from the composer")
		}
		if got := update(t, openChatModel(t, PanelChatView), "\x07"); !got.search.IsVisible() {
			t.Error("ctrl+g did not open the search overlay from the chat view")
		}
	})

	t.Run("ctrl+d and ctrl+u are described as pages", func(t *testing.T) {
		row, ok := findBinding(sections, "Page down / up")
		if !ok {
			t.Fatal("no paging row")
		}
		if !strings.Contains(row.Keys, "ctrl+d") {
			t.Fatalf("first paging row is %q, want the ctrl+d/ctrl+u row", row.Keys)
		}
		// chatview moves by a full panel height for these, not a half page.
		var all string
		for _, sec := range sections {
			for _, b := range sec.Bindings {
				all += b.Desc + "\x00"
			}
		}
		if strings.Contains(all, "Half page") {
			t.Error("the overlay still calls ctrl+d/ctrl+u a half page")
		}
	})
}

// TestComposerHelpFollowsEditingMode: the two keymaps disagree about what esc
// does, which is exactly what a reader opens the card to settle.
func TestComposerHelpFollowsEditingMode(t *testing.T) {
	find := func(t *testing.T, mode composer.EditingMode) help.Section {
		t.Helper()
		m := mainModel(t, PanelComposer)
		m.composer.SetEditingMode(mode)
		for _, sec := range m.helpSections() {
			if strings.HasPrefix(sec.Title, "Composer") {
				return sec
			}
		}
		t.Fatal("no composer section")
		return help.Section{}
	}

	emacs := find(t, composer.ModeEmacs)
	if !strings.Contains(emacs.Title, "emacs") {
		t.Errorf("title = %q, want it to name the emacs keymap", emacs.Title)
	}
	var emacsAll string
	for _, b := range emacs.Bindings {
		emacsAll += b.Keys + "\x00" + b.Desc + "\x00"
	}
	if !strings.Contains(emacsAll, "ctrl+w") || !strings.Contains(emacsAll, "ctrl+a / ctrl+e") {
		t.Error("the emacs section omits its readline chords")
	}
	if strings.Contains(emacsAll, "insert mode") {
		t.Error("the emacs section describes vi's esc")
	}

	vi := find(t, composer.ModeVi)
	if !strings.Contains(vi.Title, "vi") {
		t.Errorf("title = %q, want it to name the vi keymap", vi.Title)
	}
	var viAll string
	for _, b := range vi.Bindings {
		viAll += b.Keys + "\x00" + b.Desc + "\x00"
	}
	if !strings.Contains(viAll, "insert mode") {
		t.Error("the vi section does not explain that esc leaves insert mode first")
	}
	if strings.Contains(viAll, "ctrl+w") {
		t.Error("the vi section advertises emacs chords")
	}
	// Both keymaps keep the chords that are not line editing.
	for _, sec := range []help.Section{emacs, vi} {
		var keys string
		for _, b := range sec.Bindings {
			keys += b.Keys + "\x00"
		}
		for _, want := range []string{"enter", "ctrl+j", "ctrl+t", "ctrl+v", "ctrl+o"} {
			if !strings.Contains(keys, want) {
				t.Errorf("%s omits %q", sec.Title, want)
			}
		}
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

// TestResolveKeysDefaults pins the defaults app.go falls back to when a
// config predates a field (or is the zero value used by these tests).
func TestResolveKeysDefaults(t *testing.T) {
	want := map[string]string{
		"quit": "ctrl+c", "focusChatList": "f1", "focusChatView": "f2",
		"focusComposer": "f3", "search": "/", "globalSearch": "ctrl+g",
		"contacts": "alt+c", "contactsAlt": "f4", "help": "?",
		"nextFolder": "alt+l", "prevFolder": "alt+h",
		"nextChat": "alt+j", "prevChat": "alt+k",
	}
	k := resolveKeys(config.KeyConfig{})
	got := map[string]string{
		"quit": k.quit, "focusChatList": k.focusChatList, "focusChatView": k.focusChatView,
		"focusComposer": k.focusComposer, "search": k.search, "globalSearch": k.globalSearch,
		"contacts": k.contacts, "contactsAlt": k.contactsAlt, "help": k.help,
		"nextFolder": k.nextFolder, "prevFolder": k.prevFolder,
		"nextChat": k.nextChat, "prevChat": k.prevChat,
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

// TestHelpDoesNotSurviveAScreenChange covers the trap the review found: the
// help card's close and scroll keys are handled only in Update's ScreenMain
// branch, so a card left open across an auth transition would be unclosable
// — and every key aimed at dismissing it would land in the phone or 2FA
// field drawn behind it, typed blind.
func TestHelpDoesNotSurviveAScreenChange(t *testing.T) {
	transitions := []struct {
		name string
		msg  tea.Msg
	}{
		{"auth error", AuthErrorMsg{Err: errors.New("session revoked")}},
		{"auth wants a phone number", AuthStateChangedMsg{State: int(telegram.AuthStateWaitPhone)}},
		{"auth wants a code", AuthStateChangedMsg{State: int(telegram.AuthStateWaitCode)}},
		{"auth wants the 2FA password", AuthStateChangedMsg{State: int(telegram.AuthStateWaitPassword)}},
	}
	for _, tc := range transitions {
		t.Run(tc.name, func(t *testing.T) {
			m := update(t, sizedMainModel(t), "?")
			if !m.help.IsVisible() {
				t.Fatal("precondition: help did not open")
			}

			m = send(t, m, tc.msg)
			if m.screen != ScreenAuth {
				t.Fatalf("screen = %v, want ScreenAuth", m.screen)
			}
			if m.help.IsVisible() {
				t.Error("the help card survived the transition to the auth screen")
			}
			// The auth form is what the user sees, not a help card.
			if view := m.View().Content; strings.Contains(view, "to close") {
				t.Error("the help card is still drawn over the auth screen")
			}

			// Keys reach auth rather than being swallowed by the card's
			// handler, which does not run outside ScreenMain.
			before := m.auth.View()
			if after := update(t, m, "5"); after.auth.View() == before {
				t.Error("a keystroke did not reach the auth form")
			}
		})
	}
}

// TestHelpIsNeverDrawnOffTheMainScreen is the second lock: even if some
// future path sets the flag without going through setScreen, View refuses to
// draw a card whose dismiss keys are unreachable.
func TestHelpIsNeverDrawnOffTheMainScreen(t *testing.T) {
	m := sizedMainModel(t)
	m.help.SetVisible(true)
	for _, screen := range []ScreenState{ScreenAuth, ScreenLoading} {
		m.screen = screen // deliberately bypassing setScreen
		if strings.Contains(m.View().Content, "to close") {
			t.Errorf("the help card was drawn on screen %v", screen)
		}
	}
	m.screen = ScreenMain
	if !strings.Contains(m.View().Content, "to close") {
		t.Error("the help card is not drawn on the main screen")
	}
}

// TestHelpSwallowsMouse: a click or wheel event over the card would
// otherwise act on the panels hidden underneath it.
func TestHelpSwallowsMouse(t *testing.T) {
	// Focused on the chat view, so a click into the left panel is a change
	// the assertion can actually see.
	m := update(t, sizedMainModel(t, PanelChatView), "?")
	if !m.help.IsVisible() {
		t.Fatal("precondition: help did not open")
	}
	click := tea.MouseClickMsg{Button: tea.MouseLeft, X: 5, Y: 5}
	if without := send(t, sizedMainModel(t, PanelChatView), click); without.focus != PanelChatList {
		t.Fatalf("precondition: the same click without the card gave focus %v, want PanelChatList",
			without.focus)
	}

	clicked := send(t, m, click)
	if clicked.focus != PanelChatView {
		t.Errorf("a click behind the help card moved focus to %v", clicked.focus)
	}
	if !clicked.help.IsVisible() {
		t.Error("a click closed the help card")
	}

	wheeled := send(t, m, tea.MouseWheelMsg{Button: tea.MouseWheelUp, X: 5, Y: 5})
	if !wheeled.help.IsVisible() {
		t.Error("a wheel event closed the help card")
	}
}

// TestComposeNeedsAChat covers the guard restored with quick-type's removal:
// i/c must not focus a composer that has nothing to send to, and must yield
// to an overlay or dialog that owns its own input.
func TestComposeNeedsAChat(t *testing.T) {
	for _, panel := range []FocusPanel{PanelChatList, PanelChatView} {
		for _, seq := range []string{"i", "c"} {
			// No chat open.
			if got := update(t, mainModel(t, panel), seq); got.focus == PanelComposer {
				t.Errorf("panel %v: %q focused the composer with no chat open", panel, seq)
			}
			// Overlay open.
			overlay := update(t, openChatModel(t, panel), "\x1bc") // contacts
			if !overlay.contacts.IsVisible() {
				t.Fatal("precondition: contacts did not open")
			}
			if got := update(t, overlay, seq); got.focus == PanelComposer {
				t.Errorf("panel %v: %q stole a key from the contacts overlay", panel, seq)
			}
		}
	}
}

// TestTabCyclesFromTheComposer: the help card advertises tab as a global
// binding, so it has to work from the composer too.
func TestTabCyclesFromTheComposer(t *testing.T) {
	m := update(t, openChatModel(t, PanelComposer), "\t")
	if m.focus == PanelComposer {
		t.Error("tab did not cycle out of the composer")
	}
	// Shift+tab still steps the other way.
	if got := update(t, openChatModel(t, PanelComposer), "\x1b[Z"); got.focus != PanelChatView {
		t.Errorf("shift+tab from the composer went to %v, want PanelChatView", got.focus)
	}
}
