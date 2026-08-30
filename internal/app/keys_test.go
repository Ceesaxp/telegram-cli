package app

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/imtaqin/telegram-cli/internal/config"
	"github.com/imtaqin/telegram-cli/internal/keys"
	"github.com/imtaqin/telegram-cli/internal/store"
	"github.com/imtaqin/telegram-cli/internal/telegram"
	"github.com/imtaqin/telegram-cli/internal/ui/components/chatview"
	"github.com/imtaqin/telegram-cli/internal/ui/components/composer"
	"github.com/imtaqin/telegram-cli/internal/ui/components/dialog"
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
			start := PanelContacts
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
			if !keys.NewPress(decodeKey(t, tc.seq)).Matches(tc.binding) {
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
	k := keys.NewPress(decodeKey(t, "¡"))
	if k.Modified() {
		t.Fatal("bare composed char unexpectedly carries a modifier")
	}
	if k.Matches("alt+1") {
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
		k := keys.NewPress(decodeKey(t, seq))
		if !k.Matches("n", "N") {
			t.Errorf("%q did not match n/N (stroke=%q text=%q)", seq, k.Stroke(), k.Text())
		}
	}
	// Modified variants must not be mistaken for the match-cycling keys, so
	// alt+n stays available to app-level bindings.
	for _, seq := range []string{"\x1bn", "\x1b[110;3u", "\x0e" /* ctrl+n */} {
		k := keys.NewPress(decodeKey(t, seq))
		if k.Matches("n", "N") {
			t.Errorf("%q wrongly matched n/N (stroke=%q text=%q)", seq, k.Stroke(), k.Text())
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

	// "/" means "search the buffer in front of you" in BOTH browsing
	// panels now: the chat list gets its own local filter rather than the
	// global overlay, which is what ctrl+g is for.
	// This is the assertion that pins the change: before it, "/" from the
	// chat list opened the GLOBAL search overlay and moved focus to
	// PanelSearch. Now it stays in the panel and opens chatlist's own
	// filter, so "/" means "search what is in front of me" in both
	// browsing panels rather than one.
	t.Run("chatlist filters itself", func(t *testing.T) {
		m, cmd := updateCmd(t, mainModel(t, PanelChatList), "/")
		if m.search.IsVisible() || m.focus == PanelSearch {
			t.Errorf("global search overlay opened from the chat list "+
				"(visible=%v focus=%v)", m.search.IsVisible(), m.focus)
		}
		if !m.chatList.FilterActive() {
			t.Error("the chat list filter did not open")
		}
		if m.focus != PanelChatList {
			t.Errorf("focus = %v, want PanelChatList", m.focus)
		}
		// Called directly, not re-emitted — same livelock reasoning as the
		// chat view's find above.
		if cmd != nil {
			if _, ok := cmd().(tea.KeyPressMsg); ok {
				t.Error("\"/\" re-emitted a key event instead of calling OpenFilter")
			}
		}
	})

	// While the filter input is open it owns the keyboard, exactly as
	// chatview's find does. This is the negative case for the two bindings
	// this wave adds: h/l must not move panels and q must not quit out
	// from under a query being typed — they are text, and the app yields
	// them to chatlist untouched.
	t.Run("an open filter owns the keys", func(t *testing.T) {
		m := update(t, mainModel(t, PanelChatList), "/")
		if !m.chatList.FilterActive() {
			t.Fatal("precondition: filter not open")
		}
		for _, seq := range []string{"l", "h", "q", "?"} {
			got, cmd := updateCmd(t, m, seq)
			if got.focus != PanelChatList {
				t.Errorf("%q moved focus to %v while filtering", seq, got.focus)
			}
			if got.help.IsVisible() {
				t.Errorf("%q opened the help overlay while filtering", seq)
			}
			if got.search.IsVisible() {
				t.Errorf("%q opened the global search while filtering", seq)
			}
			if quits(cmd) {
				t.Errorf("%q quit while filtering", seq)
			}
			if !got.chatList.FilterActive() {
				t.Errorf("%q closed the filter input", seq)
			}
		}
		// esc is the filter's own close key while the input is open, not
		// the app's focus ladder.
		if got := update(t, m, "\x1b"); got.focus != PanelChatList {
			t.Errorf("esc while filtering moved focus to %v", got.focus)
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
	k := keys.NewPress(decodeKey(t, "\x0b")) // ctrl+k
	if k.Stroke() != "ctrl+k" {
		t.Fatalf("stroke = %q, want ctrl+k", k.Stroke())
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
		if k.Matches(binding) {
			t.Errorf("ctrl+k is bound to %s", name)
		}
	}
	if got := update(t, m, "\x0b"); got.contacts.IsVisible() || got.focus != PanelComposer {
		t.Errorf("ctrl+k disturbed the composer: contacts=%v focus=%v",
			got.contacts.IsVisible(), got.focus)
	}
}

// TestFolderCyclingKeys covers folder cycling after bare h/l were taken away
// from it and given to panel movement. Four spellings remain and all four
// have to keep working, or the change cost something: lazygit's [ and ], the
// left/right arrows, the 1-9 jump (covered in TestChatListKeysReachChatList),
// and the alt+h/alt+l that work from any panel.
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
		if k := keys.NewPress(decodeKey(t, seq)); !k.Matches(base.keys.prevFolder, base.keys.nextFolder) {
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
		{"lazygit next", "]", forward},
		{"arrow next", "\x1b[C", forward},
		{"alt next", "\x1bl", forward},
		{"lazygit prev", "[", backward},
		{"arrow prev", "\x1b[D", backward},
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
	for _, seq := range []string{"[", "]"} {
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
	for _, seq := range []string{"[", "]"} {
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

	// The point of the change: bare h/l no longer touch the folder tabs
	// from either browsing panel. This is the regression that would
	// reappear if the old viFolder gate were restored alongside the new
	// panel movement.
	for _, panel := range []FocusPanel{PanelChatList, PanelChatView} {
		base := seedFolders(t, sizedMainModel(t, panel))
		for _, seq := range []string{"h", "l"} {
			got := update(t, base, seq)
			if id := got.chatList.ActiveFolderID(); id != telegram.AllChatsFolderID {
				t.Errorf("%q from panel %v cycled the folder tab (now %d)",
					seq, panel, id)
			}
		}
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
		k := keys.NewPress(decodeKey(t, seq))
		if k.Matches(appBindings...) {
			t.Errorf("%q collides with an app-level binding (stroke=%q)", seq, k.Stroke())
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
	for _, chord := range []string{"ctrl+w", "ctrl+a", "ctrl+e", "ctrl+u", "ctrl+k"} {
		if !strings.Contains(emacsAll, chord) {
			t.Errorf("the emacs section omits the readline chord %q", chord)
		}
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
			k := keys.NewPress(decodeKey(t, seq))
			if want := "ctrl+" + string(r); k.Stroke() != want {
				t.Fatalf("stroke = %q, want %q", k.Stroke(), want)
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
		k := keys.NewPress(decodeKey(t, tc.seq))
		if k.Stroke() != tc.stroke {
			t.Fatalf("stroke = %q, want %q", k.Stroke(), tc.stroke)
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
	// ctrl+g is what opens it there now: "/" is the chat list's own filter.
	m := update(t, mainModel(t, PanelChatList), "\x07")
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
		"quit": "ctrl+c", "quitBrowsing": "q",
		"focusChatList": "f1", "focusChatView": "f2",
		"focusComposer": "f3", "search": "/", "globalSearch": "ctrl+g",
		"contacts": "alt+c", "contactsAlt": "f4", "help": "?",
		"nextFolder": "alt+l", "prevFolder": "alt+h",
		"nextChat": "alt+j", "prevChat": "alt+k",
		// Handed to chatview rather than dispatched here; the defaults
		// are the keys chatview hardcoded before it took them from config.
		"reply": "r", "editMessage": "e", "deleteMessage": "d",
		"scrollUp": "k", "scrollDown": "j",
		"pageUp": "pgup", "pageDown": "pgdown",
	}
	k := resolveKeys(config.KeyConfig{})
	got := map[string]string{
		"quit": k.quit, "quitBrowsing": k.quitBrowsing,
		"focusChatList": k.focusChatList, "focusChatView": k.focusChatView,
		"focusComposer": k.focusComposer, "search": k.search, "globalSearch": k.globalSearch,
		"contacts": k.contacts, "contactsAlt": k.contactsAlt, "help": k.help,
		"nextFolder": k.nextFolder, "prevFolder": k.prevFolder,
		"nextChat": k.nextChat, "prevChat": k.prevChat,
		"reply": k.reply, "editMessage": k.editMessage, "deleteMessage": k.deleteMessage,
		"scrollUp": k.scrollUp, "scrollDown": k.scrollDown,
		"pageUp": k.pageUp, "pageDown": k.pageDown,
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

// quits reports whether a command is the one that ends the program. Only the
// direct tea.Quit is recognized, which is what every quit path here returns;
// a batch containing it would be a different (and unintended) shape.
func quits(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

// TestViPanelMovement covers the change bare h/l went through: they used to
// cycle the folder tabs, and now move between the two browsing panels, which
// is what left/right mean in a layout that is literally two columns.
//
// The negative half matters as much as the positive: they must be one step at
// a time (no wrap), must never land in the composer, and must be inert while
// an overlay or dialog owns the keyboard.
func TestViPanelMovement(t *testing.T) {
	t.Run("l enters the chat view, h returns", func(t *testing.T) {
		m := update(t, openChatModel(t, PanelChatList), "l")
		if m.focus != PanelChatView {
			t.Fatalf("l from the chat list: focus = %v, want PanelChatView", m.focus)
		}
		if m = update(t, m, "h"); m.focus != PanelChatList {
			t.Fatalf("h from the chat view: focus = %v, want PanelChatList", m.focus)
		}
	})

	// The esc ladder's discipline: a step you cannot take is nothing, not a
	// wrap onto the far side. l from the chat view in particular must not
	// reach the composer — typing is entered deliberately or not at all.
	t.Run("the edges are no-ops, not wraps", func(t *testing.T) {
		for _, tc := range []struct {
			seq   string
			panel FocusPanel
		}{
			{"h", PanelChatList},
			{"l", PanelChatView},
		} {
			got, cmd := updateCmd(t, openChatModel(t, tc.panel), tc.seq)
			if got.focus != tc.panel {
				t.Errorf("%q at the edge of %v moved focus to %v",
					tc.seq, tc.panel, got.focus)
			}
			if got.search.IsVisible() || got.contacts.IsVisible() || got.help.IsVisible() {
				t.Errorf("%q at the edge of %v opened an overlay", tc.seq, tc.panel)
			}
			if quits(cmd) {
				t.Errorf("%q at the edge of %v quit", tc.seq, tc.panel)
			}
		}
	})

	// The composer owns printables: h and l are text there, not motion.
	t.Run("the composer is unaffected", func(t *testing.T) {
		m := openChatModel(t, PanelComposer)
		for _, seq := range []string{"h", "l"} {
			got := update(t, m, seq)
			if got.focus != PanelComposer {
				t.Errorf("%q moved focus out of the composer to %v", seq, got.focus)
			}
		}
		if m = update(t, m, "h"); !m.composer.HasDraft() {
			t.Error("\"h\" typed in the composer did not become text")
		}
	})

	// An overlay or dialog owns the keyboard while it is up. The panels
	// behind it are not visible, so moving focus there would happen blind.
	t.Run("inert while an overlay or dialog is up", func(t *testing.T) {
		cases := map[string]func(t *testing.T) Model{
			"help": func(t *testing.T) Model {
				return update(t, openChatModel(t, PanelChatList), "?")
			},
			"global search": func(t *testing.T) Model {
				return update(t, openChatModel(t, PanelChatList), "\x07") // ctrl+g
			},
			"contacts": func(t *testing.T) Model {
				return update(t, openChatModel(t, PanelChatList), "\x1bc")
			},
			"dialog": func(t *testing.T) Model {
				m := openChatModel(t, PanelChatList)
				d := dialog.NewConfirm(m.theme, "delete", "Delete Message", "Are you sure?")
				m.dialog = &d
				return m
			},
		}
		for name, setup := range cases {
			t.Run(name, func(t *testing.T) {
				m := setup(t)
				before := m.focus
				for _, seq := range []string{"h", "l"} {
					if got := update(t, m, seq); got.focus != before {
						t.Errorf("%q moved focus from %v to %v with the %s up",
							seq, before, got.focus, name)
					}
				}
			})
		}
	})
}

// TestQuitFromBrowsingPanels covers "q" quitting from the chat list and chat
// view — free there because neither is a text surface — and the confirm that
// stands between a single keystroke and a discarded draft.
func TestQuitFromBrowsingPanels(t *testing.T) {
	t.Run("quits from both browsing panels", func(t *testing.T) {
		for _, panel := range []FocusPanel{PanelChatList, PanelChatView} {
			m := openChatModel(t, panel)
			if m.composer.HasDraft() || m.composer.Attachment() != "" {
				t.Fatalf("precondition: composer is not empty in panel %v", panel)
			}
			got, cmd := updateCmd(t, m, "q")
			if !quits(cmd) {
				t.Errorf("q from panel %v did not quit", panel)
			}
			if got.dialog != nil {
				t.Errorf("q from panel %v raised a confirm over an empty composer", panel)
			}
		}
	})

	// The composer owns printables, which is the whole reason a bare letter
	// can mean "quit" anywhere else.
	t.Run("inert from the composer", func(t *testing.T) {
		m, cmd := updateCmd(t, openChatModel(t, PanelComposer), "q")
		if quits(cmd) {
			t.Fatal("q quit from the composer")
		}
		if m.focus != PanelComposer {
			t.Errorf("focus = %v, want PanelComposer", m.focus)
		}
		if !m.composer.HasDraft() {
			t.Error("\"q\" typed in the composer did not become text")
		}
	})

	t.Run("inert while an overlay or dialog is up", func(t *testing.T) {
		// q still closes the help overlay — the one place it already had a
		// meaning, which this change does not disturb.
		h := update(t, openChatModel(t, PanelChatList), "?")
		if !h.help.IsVisible() {
			t.Fatal("precondition: help did not open")
		}
		got, cmd := updateCmd(t, h, "q")
		if quits(cmd) {
			t.Error("q quit instead of closing the help overlay")
		}
		if got.help.IsVisible() {
			t.Error("q did not close the help overlay")
		}

		for name, setup := range map[string]func(t *testing.T) Model{
			"global search": func(t *testing.T) Model {
				return update(t, openChatModel(t, PanelChatList), "\x07")
			},
			"contacts": func(t *testing.T) Model {
				return update(t, openChatModel(t, PanelChatList), "\x1bc")
			},
			"dialog": func(t *testing.T) Model {
				m := openChatModel(t, PanelChatList)
				d := dialog.NewConfirm(m.theme, "delete", "Delete Message", "Are you sure?")
				m.dialog = &d
				return m
			},
		} {
			t.Run(name, func(t *testing.T) {
				if _, cmd := updateCmd(t, setup(t), "q"); quits(cmd) {
					t.Errorf("q quit with the %s up", name)
				}
			})
		}
	})

	// draft builds the state a user would lose: a real message typed into
	// the composer, with focus handed back to a browsing panel.
	draft := func(t *testing.T, text string) Model {
		t.Helper()
		m := openChatModel(t, PanelComposer)
		for _, r := range text {
			m = update(t, m, string(r))
		}
		m.setFocus(PanelChatList)
		return m
	}

	t.Run("a draft asks before discarding it", func(t *testing.T) {
		m := draft(t, "hi")
		if !m.composer.HasDraft() {
			t.Fatal("precondition: no draft in the composer")
		}
		got, cmd := updateCmd(t, m, "q")
		if quits(cmd) {
			t.Fatal("q discarded a draft without asking")
		}
		if got.dialog == nil || !got.dialog.IsVisible() {
			t.Fatal("q with a draft did not raise the confirm dialog")
		}

		// Confirming quits; cancelling returns to where the user was.
		out, cmd := got.Update(dialog.DialogResultMsg{ID: "quit", Confirmed: true})
		if !quits(cmd) {
			t.Error("confirming the quit dialog did not quit")
		}
		if _, ok := out.(Model); !ok {
			t.Fatalf("Update returned %T, want app.Model", out)
		}

		out, cmd = got.Update(dialog.DialogResultMsg{ID: "quit", Confirmed: false})
		if quits(cmd) {
			t.Error("cancelling the quit dialog quit anyway")
		}
		next := out.(Model)
		if next.dialog != nil {
			t.Error("cancelling left the dialog on screen")
		}
		if !next.composer.HasDraft() {
			t.Error("cancelling lost the draft it was protecting")
		}
	})

	// Whitespace is not work. Prompting for it would be the kind of dialog
	// people learn to dismiss unread.
	t.Run("a whitespace-only draft quits at once", func(t *testing.T) {
		m := draft(t, "   ")
		if m.composer.HasDraft() {
			t.Fatal("precondition: whitespace counted as a draft")
		}
		got, cmd := updateCmd(t, m, "q")
		if !quits(cmd) {
			t.Error("q did not quit with a whitespace-only composer")
		}
		if got.dialog != nil {
			t.Error("whitespace raised a confirm dialog")
		}
	})

	// A pending attachment is the other half of the rule: nothing typed,
	// but something to lose.
	t.Run("a pending attachment asks too", func(t *testing.T) {
		m := openChatModel(t, PanelChatList)
		m.composer.SetAttachment("/tmp/pasted.png", true)
		if m.composer.HasDraft() {
			t.Fatal("precondition: an attachment should not count as draft text")
		}
		got, cmd := updateCmd(t, m, "q")
		if quits(cmd) {
			t.Fatal("q discarded a pending attachment without asking")
		}
		if got.dialog == nil || !got.dialog.IsVisible() {
			t.Error("q with a pending attachment did not raise the confirm dialog")
		}
	})
}

// TestHintBarKeysComeFromResolvedKeys is the hint bar's half of the rule
// helpSections already follows: a rebound key must not leave the bar
// advertising the one it replaced.
//
// The guarantee was written against the status bar's key strip. The frame
// replaced that bar, and the hint bar inherited both the job and this test.
func TestHintBarKeysComeFromResolvedKeys(t *testing.T) {
	labels := func(m Model) map[string]string {
		out := map[string]string{}
		for _, h := range m.hintsForMode() {
			out[h.Label] = h.Key
		}
		return out
	}

	m := mainModel(t, PanelChatList)
	for label, want := range map[string]string{"keymap": "?", "quit": "q", "reply": "r", "edit": "e"} {
		if got := labels(m)[label]; got != want {
			t.Errorf("default hint for %q is %q, want %q", label, got, want)
		}
	}

	cfg := &config.Config{}
	cfg.Keys.Help = "f12"
	cfg.Keys.QuitBrowsing = "ctrl+x"
	cfg.Keys.Reply = "ctrl+r"
	rebound := labels(New(cfg, nil, store.NewStore(), telegram.NewTUIAuthorizer(cfg)))
	for label, want := range map[string]string{"keymap": "f12", "quit": "ctrl+x", "reply": "ctrl+r"} {
		if got := rebound[label]; got != want {
			t.Errorf("rebound hint for %q is %q, want %q", label, got, want)
		}
	}

	// The bar is an abbreviation that points at the help card, not a second
	// copy of it: it drops hints from the right when they do not fit, so a
	// long list means the ones that matter go missing on a narrow terminal.
	if got := len(m.hintsForMode()); got > 8 {
		t.Errorf("%d hints is too many to survive a narrow terminal", got)
	}
}

// TestChatViewKeysComeFromConfig covers the plumbing that ended eight config
// fields being advertised and ignored: reply/edit/delete and the scroll and
// page keys are resolved here and handed to the panel that implements them.
//
// The help card reads them back through chatview.ActiveKeys rather than from
// resolvedKeys, because chatview refuses a binding that would shadow a key it
// already owns — and a card that advertised the refused binding would be the
// exact drift this wave exists to remove.
func TestChatViewKeysComeFromConfig(t *testing.T) {
	cfg := &config.Config{}
	cfg.Keys.Reply = "y"
	cfg.Keys.EditMessage = "Option+E"
	cfg.Keys.DeleteMessage = "v"
	cfg.Keys.ScrollUp = "u"
	cfg.Keys.ScrollDown = "n" // collides with chatview's next-match key
	cfg.Keys.PageUp = "b"
	cfg.Keys.PageDown = "f"
	k := resolveKeys(cfg.Keys)

	// Normalized on the way in, like every other configured binding.
	if k.editMessage != "alt+e" {
		t.Errorf("editMessage = %q, want the normalized alt+e", k.editMessage)
	}

	m := New(cfg, nil, store.NewStore(), telegram.NewTUIAuthorizer(cfg))
	row, ok := findBinding(m.helpSections(), "Reply / edit / delete")
	if !ok {
		t.Fatal("no reply/edit/delete row")
	}
	if row.Keys != "y / alt+e / v" {
		t.Errorf("reply row = %q, want the configured \"y / alt+e / x\"", row.Keys)
	}

	// Motions are additive in chatview: a configured scroll key is an extra
	// spelling, not a replacement, so the row has to name both or it
	// advertises the removal of keys that still work. And scroll_down = "n"
	// collided with the next-match key, so chatview dropped it — the card
	// must not claim it.
	scroll, ok := findBinding(m.helpSections(), "Scroll down / up")
	if !ok {
		t.Fatal("no scroll row")
	}
	if scroll.Keys != "j / k / u" {
		t.Errorf("scroll row = %q, want \"j / k / u\" (the collided \"n\" dropped)",
			scroll.Keys)
	}
	page, ok := findBinding(m.helpSections(), "Page down / up, keeping")
	if !ok {
		t.Fatal("no paging row")
	}
	for _, want := range []string{"pgdown", "pgup", "f", "b"} {
		if !slices.Contains(strings.Split(page.Keys, " / "), want) {
			t.Errorf("paging row %q omits %q", page.Keys, want)
		}
	}

	// The defaults leave the rows exactly as they read before chatview
	// became configurable — no duplicated "j / k / j / k".
	plain := New(&config.Config{}, nil, store.NewStore(),
		telegram.NewTUIAuthorizer(&config.Config{}))
	if row, ok := findBinding(plain.helpSections(), "Scroll down / up"); !ok || row.Keys != "j / k" {
		t.Errorf("default scroll row = %q, want \"j / k\"", row.Keys)
	}
	if row, ok := findBinding(plain.helpSections(), "Reply / edit / delete"); !ok || row.Keys != "r / e / d" {
		t.Errorf("default reply row = %q, want \"r / e / d\"", row.Keys)
	}
}

// TestChatViewMnemonicCollisionIsNotAdvertised is the end-to-end version of
// the rule above: reply = "j" would shadow the scroll-down motion, so
// chatview keeps "r". The card has to say "r", and the panel has to mean it.
func TestChatViewMnemonicCollisionIsNotAdvertised(t *testing.T) {
	cfg := &config.Config{}
	cfg.Keys.Reply = "j"
	m := New(cfg, nil, store.NewStore(), telegram.NewTUIAuthorizer(cfg))
	m.screen = ScreenMain
	m.composer.SetEditingMode(composer.ModeEmacs)
	m.setFocus(PanelChatView)
	m.chatView.OpenChat(testChatID, "Test Chat")
	m.composer.SetChatId(testChatID)
	// messageAction needs something to act on: it targets the message at
	// the bottom of the view.
	m.store.Messages.Append(testChatID, &telegram.Message{ID: 11, ChatID: testChatID})

	row, ok := findBinding(m.helpSections(), "Reply / edit / delete")
	if !ok {
		t.Fatal("no reply/edit/delete row")
	}
	if row.Keys != "r / e / d" {
		t.Errorf("row = %q, want the built-in \"r / e / d\" — the configured "+
			"\"j\" collides with scroll-down and was refused", row.Keys)
	}

	// And the card is right: "r" replies, "j" does not.
	_, cmd := updateCmd(t, m, "r")
	if act, ok := findMessageAction(cmd); !ok || act.Action != "reply" {
		t.Errorf("\"r\" produced %+v (found=%v), want a reply action", act, ok)
	}
	_, cmd = updateCmd(t, m, "j")
	if act, ok := findMessageAction(cmd); ok {
		t.Errorf("\"j\" produced the %q action; it should still be scroll-down",
			act.Action)
	}
}

// findMessageAction walks a (possibly batched) command for a chat view
// message action. app.Update batches the focused panel's command with the
// status bar's, so the interesting message is rarely the top-level one.
func findMessageAction(cmd tea.Cmd) (chatview.MessageActionMsg, bool) {
	for _, msg := range flattenCmd(cmd) {
		if act, ok := msg.(chatview.MessageActionMsg); ok {
			return act, true
		}
	}
	return chatview.MessageActionMsg{}, false
}

func flattenCmd(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, flattenCmd(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

// TestHelpLineComesFromResolvedKeys covers the fourth copy of the keymap —
// the bar drawn under the status bar on every frame. It was the last one
// still hardcoded, and it was advertising "h/l:folder" after h/l stopped
// touching the folders.
//
// It is also per-panel now. One line for all five focus states named keys
// that did nothing in four of them: n/N outside the chat view, "find" for
// a "/" that filters, Esc:back from the panel that IS back.
func TestHelpLineComesFromResolvedKeys(t *testing.T) {
	line := func(t *testing.T, panel FocusPanel, name string) string {
		t.Helper()
		m := mainModel(t, panel)
		return m.helpLine(name)
	}

	t.Run("chat list", func(t *testing.T) {
		got := line(t, PanelChatList, "CHATS")
		for _, want := range []string{"?:help", "l:messages", "/:filter",
			"q:quit", "CHATS"} {
			if !strings.Contains(got, want) {
				t.Errorf("chat list line %q omits %q", got, want)
			}
		}
		// The three the single shared line used to lie about here.
		for _, gone := range []string{"n/N", ":find", "Esc:back", "folder"} {
			if strings.Contains(got, gone) {
				t.Errorf("chat list line %q advertises %q, which does "+
					"nothing from this panel", got, gone)
			}
		}
	})

	t.Run("chat view", func(t *testing.T) {
		got := line(t, PanelChatView, "MESSAGES")
		for _, want := range []string{"?:help", "h:chats", "/:find", "n/N:match",
			"Esc:back", "q:quit", "MESSAGES"} {
			if !strings.Contains(got, want) {
				t.Errorf("chat view line %q omits %q", got, want)
			}
		}
		if strings.Contains(got, ":filter") {
			t.Errorf("chat view line %q calls \"/\" a filter; it is find here", got)
		}
	})

	// The composer's line is deliberately different: none of the browsing
	// keys are live there.
	t.Run("composer", func(t *testing.T) {
		got := line(t, PanelComposer, "COMPOSE")
		if !strings.Contains(got, "Enter:send") || !strings.Contains(got, "COMPOSE") {
			t.Errorf("composer line %q is not the composer's", got)
		}
		for _, gone := range []string{"h:", "l:", "q:quit", "n/N"} {
			if strings.Contains(got, gone) {
				t.Errorf("composer line %q advertises the browsing key %q", got, gone)
			}
		}
	})

	// The overlays own the keyboard; the panel keys behind them are not
	// reachable until they close.
	t.Run("overlays", func(t *testing.T) {
		for _, tc := range []struct {
			panel FocusPanel
			name  string
			want  string
		}{
			{PanelSearch, "SEARCH", "Esc:close"},
			{PanelContacts, "CONTACTS", "Esc:close"},
		} {
			got := line(t, tc.panel, tc.name)
			if !strings.Contains(got, tc.want) {
				t.Errorf("%s line %q omits %q", tc.name, got, tc.want)
			}
			for _, gone := range []string{"i or c:compose", "n/N", "Tab:switch"} {
				if strings.Contains(got, gone) {
					t.Errorf("%s line %q advertises %q, inert while the "+
						"overlay is up", tc.name, got, gone)
				}
			}
		}
	})

	t.Run("follows a rebind", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Keys.Help = "f12"
		cfg.Keys.QuitBrowsing = "ctrl+x"
		rebound := New(cfg, nil, store.NewStore(), telegram.NewTUIAuthorizer(cfg))
		rebound.setFocus(PanelChatList)
		got := rebound.helpLine("CHATS")
		for _, want := range []string{"f12:help", "ctrl+x:quit"} {
			if !strings.Contains(got, want) {
				t.Errorf("rebound line %q omits %q", got, want)
			}
		}
		for _, gone := range []string{"?:help", "q:quit"} {
			if strings.Contains(got, gone) {
				t.Errorf("rebound line %q still advertises %q", got, gone)
			}
		}
	})

	// A binding containing a percent sign used to be read as a format verb,
	// because the line was a format string handed to Sprintf.
	t.Run("a percent binding is not a format verb", func(t *testing.T) {
		pct := &config.Config{}
		pct.Keys.Help = "%"
		p := New(pct, nil, store.NewStore(), telegram.NewTUIAuthorizer(pct))
		p.setFocus(PanelChatList)
		if got := p.helpLine("CHATS"); !strings.Contains(got, "%:help") ||
			strings.Contains(got, "%!") {
			t.Errorf("help line mangled a %% binding: %q", got)
		}
	})
}

// TestReservedKeysStopChatViewStealingAppBindings is the end-to-end proof
// for the defect that got this wave rejected: chatview accepted
// reply = "q", the help card advertised it as Reply, and pressing "q" quit
// the application. The panel had no way to know app.go had claimed the key
// first — Model.reservedKeys, handed over in New, is that way.
func TestReservedKeysStopChatViewStealingAppBindings(t *testing.T) {
	for _, tc := range []struct {
		name  string
		key   string
		check func(t *testing.T, m Model)
	}{
		{
			name: "q still quits",
			key:  "q",
			check: func(t *testing.T, m Model) {
				if _, cmd := updateCmd(t, m, "q"); !quits(cmd) {
					t.Error("q did not quit — the chat view took it")
				}
			},
		},
		{
			name: "h still moves panels",
			key:  "h",
			check: func(t *testing.T, m Model) {
				if got := update(t, m, "h"); got.focus != PanelChatList {
					t.Errorf("h left focus at %v — the chat view took it", got.focus)
				}
			},
		},
		{
			name: "i still reaches the composer",
			key:  "i",
			check: func(t *testing.T, m Model) {
				if got := update(t, m, "i"); got.focus != PanelComposer {
					t.Errorf("i left focus at %v — the chat view took it", got.focus)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Keys.Reply = tc.key
			m := New(cfg, nil, store.NewStore(), telegram.NewTUIAuthorizer(cfg))
			m.screen = ScreenMain
			m.composer.SetEditingMode(composer.ModeEmacs)
			m.setFocus(PanelChatView)
			m.chatView.OpenChat(testChatID, "Test Chat")
			m.composer.SetChatId(testChatID)
			m.store.Messages.Append(testChatID, &telegram.Message{
				ID: 11, ChatID: testChatID,
			})

			// The panel refused the binding, so reply keeps its letter...
			if got := m.chatView.ActiveKeys().Reply; got != "r" {
				t.Errorf("reply resolved to %q; a reserved key should have "+
					"been refused in favour of the built-in \"r\"", got)
			}
			// ...and the card must not advertise the refused key either.
			row, ok := findBinding(m.helpSections(), "Reply / edit / delete")
			if !ok {
				t.Fatal("no reply/edit/delete row")
			}
			if strings.Contains(row.Keys, tc.key) {
				t.Errorf("row %q advertises %q, which app.go claims first",
					row.Keys, tc.key)
			}
			// ...and the app-level meaning of the key is intact.
			tc.check(t, m)
			// The reply action is still reachable, on its own letter.
			if _, cmd := updateCmd(t, m, "r"); func() bool {
				act, found := findMessageAction(cmd)
				return !found || act.Action != "reply"
			}() {
				t.Error("\"r\" no longer replies")
			}
		})
	}
}

// TestUnreachableMnemonicIsShownAsUnbound covers the other outcome: a
// configuration that is not reserved but still leaves an action with no
// key, because a sibling mnemonic took it. reply = "e" is accepted — "e"
// is the chat view's own letter, not the app's — and edit is left with
// nothing. The card has to say that rather than render an empty cell,
// which reads as a rendering fault and gets reported as one.
func TestUnreachableMnemonicIsShownAsUnbound(t *testing.T) {
	cfg := &config.Config{}
	cfg.Keys.Reply = "e"
	m := New(cfg, nil, store.NewStore(), telegram.NewTUIAuthorizer(cfg))

	if got := m.chatView.ActiveKeys(); got.Reply != "e" || got.Edit != "" {
		t.Fatalf("precondition: ActiveKeys = %+v, want reply \"e\" and edit "+
			"reported as unreachable", got)
	}

	row, ok := findBinding(m.helpSections(), "Reply / edit / delete")
	if !ok {
		t.Fatal("no reply/edit/delete row")
	}
	if row.Keys != "e / "+unboundKey+" / d" {
		t.Errorf("row = %q, want %q", row.Keys, "e / "+unboundKey+" / d")
	}
	// The specific thing being prevented: a blank between the separators.
	for _, part := range strings.Split(row.Keys, " / ") {
		if strings.TrimSpace(part) == "" {
			t.Errorf("row %q has an empty key cell, which reads as a "+
				"rendering bug rather than as a configuration problem", row.Keys)
		}
	}
	// And no other row is disturbed by one mnemonic collapsing.
	if scroll, ok := findBinding(m.helpSections(), "Scroll down / up"); !ok ||
		scroll.Keys != "j / k" {
		t.Errorf("scroll row = %q, want the untouched \"j / k\"", scroll.Keys)
	}
}

// TestContactsRespectsModals covers the gate the review found missing: the
// contacts toggle had no overlay guard at all, so alt+c opened the contacts
// panel out from under a modal dialog that was still waiting for an answer.
//
// The gate is deliberately NOT the usual noOverlay: contacts is a toggle,
// and gating it on "no overlay is up" would leave the key that opens the
// panel unable to close it.
func TestContactsRespectsModals(t *testing.T) {
	t.Run("a dialog cannot be bypassed", func(t *testing.T) {
		m := openChatModel(t, PanelChatList)
		d := dialog.NewConfirm(m.theme, "delete", "Delete Message", "Are you sure?")
		m.dialog = &d
		for _, seq := range []string{"\x1bc", "\x1bOS"} { // alt+c, f4
			got := update(t, m, seq)
			if got.contacts.IsVisible() {
				t.Errorf("%q opened contacts over an open dialog", seq)
			}
			if got.dialog == nil {
				t.Errorf("%q dismissed the dialog", seq)
			}
		}
	})

	t.Run("the search overlay cannot be bypassed", func(t *testing.T) {
		m := update(t, openChatModel(t, PanelChatList), "\x07") // ctrl+g
		if !m.search.IsVisible() {
			t.Fatal("precondition: global search did not open")
		}
		if got := update(t, m, "\x1bc"); got.contacts.IsVisible() {
			t.Error("alt+c opened contacts over the search overlay")
		}
	})

	t.Run("still toggles itself closed", func(t *testing.T) {
		m := update(t, openChatModel(t, PanelChatList), "\x1bc")
		if !m.contacts.IsVisible() {
			t.Fatal("precondition: contacts did not open")
		}
		if got := update(t, m, "\x1bc"); got.contacts.IsVisible() {
			t.Error("alt+c could no longer close the panel it opened")
		}
	})
}

// TestComposerExceptionListIsTrue pins the claim keymap.go makes and the
// docs copy: the complete list of keys that still fire from a focused
// composer. The review found the list omitting two that do — keys.quit and
// the contacts pair — which matters because a user who rebinds keys.quit to
// a printable loses that character while writing, and nothing warns them.
func TestComposerExceptionListIsTrue(t *testing.T) {
	// keys.quit is matched before every focus gate, so it fires while
	// composing. That is correct by design for a quit key, and the reason
	// the doc comment now spells out the hazard of rebinding it to a letter.
	cfg := &config.Config{}
	cfg.Keys.Quit = "ctrl+x"
	m := New(cfg, nil, store.NewStore(), telegram.NewTUIAuthorizer(cfg))
	m.screen = ScreenMain
	m.composer.SetEditingMode(composer.ModeEmacs)
	m.composer.SetChatId(testChatID)
	m.setFocus(PanelComposer)
	if _, cmd := updateCmd(t, m, "\x18"); !quits(cmd) { // ctrl+x
		t.Error("keys.quit did not fire from the composer, but the keymap " +
			"table lists it as one that does")
	}

	// The contacts pair likewise — alt+c and f4 are not characters.
	for _, seq := range []string{"\x1bc", "\x1bOS"} {
		c := openChatModel(t, PanelComposer)
		if got := update(t, c, seq); !got.contacts.IsVisible() {
			t.Errorf("%q did not open contacts from the composer, but the "+
				"keymap table lists it as one that does", seq)
		}
	}

	// And the counter-claim: quit_browsing is a bare letter, so it must NOT
	// fire here — it is text.
	q := openChatModel(t, PanelComposer)
	if _, cmd := updateCmd(t, q, "q"); quits(cmd) {
		t.Error("keys.quit_browsing quit from the composer; it is supposed " +
			"to be a typed character there")
	}
}

// TestLiveKeysAreAdvertisedWhereTheyWork covers five live bindings
// the help card used to omit. The omission was not harmless: because the
// README drift test compares the README's Key column against this card, a
// key missing here could not be documented there either — so the facts got
// written into the Action column, where the parser never looks. Hiding a
// fact from the test to keep the test green is how a guarantee rots.
//
// Each row is pinned to the handler that implements it, because the other
// failure mode is worse than an omission: advertising a key that does
// nothing is exactly what this wave exists to stop.
func TestLiveKeysAreAdvertisedWhereTheyWork(t *testing.T) {
	m := mainModel(t, PanelChatList)
	sections := m.helpSections()

	// Home/End are handled by the panels (chatview's own g/G/home/end
	// switch, and the list widget's for the chat list), which only works
	// because app-level dispatch claims neither on the way past.
	t.Run("app claims neither", func(t *testing.T) {
		appBindings := []string{
			m.keys.quit, m.keys.quitBrowsing, m.keys.focusChatList,
			m.keys.focusChatView, m.keys.focusComposer, m.keys.search,
			m.keys.globalSearch, m.keys.contacts, m.keys.contactsAlt,
			m.keys.help, m.keys.nextFolder, m.keys.prevFolder,
			m.keys.nextChat, m.keys.prevChat,
		}
		// Every encoding a terminal uses for these two.
		for _, seq := range []string{
			"\x1b[H", "\x1b[1~", "\x1bOH", // home
			"\x1b[F", "\x1b[4~", "\x1bOF", // end
		} {
			k := keys.NewPress(decodeKey(t, seq))
			if k.Matches(appBindings...) || k.Matches(keys.AppFixed...) {
				t.Errorf("%q (%s) collides with an app-level binding, so it "+
					"never reaches the panel the card credits it to",
					seq, k.Stroke())
			}
			for _, panel := range []FocusPanel{PanelChatList, PanelChatView} {
				got := update(t, openChatModel(t, panel), seq)
				if got.focus != panel {
					t.Errorf("%q moved focus from %v to %v instead of "+
						"reaching the panel", seq, panel, got.focus)
				}
				if got.search.IsVisible() || got.contacts.IsVisible() ||
					got.help.IsVisible() {
					t.Errorf("%q opened an overlay from %v", seq, panel)
				}
			}
		}
	})

	// The rows themselves. Split one-key-per-meaning: a "g / G / home / end"
	// row against "Top / bottom" leaves the reader guessing which end
	// "home" is.
	t.Run("rows name both spellings", func(t *testing.T) {
		for _, tc := range []struct{ desc, want string }{
			{"First chat", "g / home"},
			{"Last chat", "G / end"},
			{"Top", "g / home"},
			{"Bottom", "G / end"},
		} {
			row, ok := findBinding(sections, tc.desc)
			if !ok {
				t.Errorf("no %q row", tc.desc)
				continue
			}
			if row.Keys != tc.want {
				t.Errorf("%q row = %q, want %q", tc.desc, row.Keys, tc.want)
			}
		}
	})

	// The composer's own share of these keys. ctrl+d and delete both reach
	// the textarea's DeleteChar, and both die in vi NORMAL state —
	// handleViNormal drops ctrl-carrying keys before its switch and has no
	// "delete" case — so they are emacs-only on the card. home and end go
	// the other way: handleViNormal binds them ("0","home" / "$","end"), so
	// they survive the mode change and belong in both sections.
	t.Run("composer rows follow the editing mode", func(t *testing.T) {
		section := func(t *testing.T, mode composer.EditingMode) help.Section {
			t.Helper()
			c := mainModel(t, PanelComposer)
			c.composer.SetEditingMode(mode)
			for _, sec := range c.helpSections() {
				if strings.HasPrefix(sec.Title, "Composer") {
					return sec
				}
			}
			t.Fatal("no composer section")
			return help.Section{}
		}
		// Token membership, not substring: the vi section legitimately
		// contains "dd" and "D", which a naive Contains would read as a
		// delete binding.
		names := func(sec help.Section) map[string]string {
			out := map[string]string{}
			for _, b := range sec.Bindings {
				for _, key := range strings.Split(b.Keys, " / ") {
					out[key] = b.Desc
				}
			}
			return out
		}

		emacs, vi := names(section(t, composer.ModeEmacs)), names(section(t, composer.ModeVi))

		for _, key := range []string{"ctrl+d", "delete"} {
			desc, ok := emacs[key]
			if !ok {
				t.Errorf("the emacs composer section does not advertise %q", key)
				continue
			}
			if !strings.Contains(strings.ToLower(desc), "delete") {
				t.Errorf("%q row says %q, want it to describe a delete", key, desc)
			}
			if _, wrong := vi[key]; wrong {
				t.Errorf("the vi composer section advertises %q, which stops "+
					"working the moment the user leaves insert mode", key)
			}
		}

		for _, key := range []string{"home", "end"} {
			if _, ok := emacs[key]; !ok {
				t.Errorf("the emacs composer section does not advertise %q", key)
			}
			desc, ok := vi[key]
			if !ok {
				t.Errorf("the vi composer section does not advertise %q, but "+
					"handleViNormal binds it", key)
				continue
			}
			// The row pairs home/end with 0/$, which are normal-mode only.
			// It has to say which half survives the mode change.
			if !strings.Contains(desc, "normal mode") {
				t.Errorf("vi %q row says %q, want it to qualify the half that "+
					"is normal-mode only", key, desc)
			}
		}
	})

	// End to end, through the real decoder: home moves to the start of the
	// line and the delete-class keys remove the character now under the
	// cursor. A row naming a key the textarea ignored would pass every
	// assertion above and still be a lie.
	t.Run("the composer keys really do it", func(t *testing.T) {
		for _, tc := range []struct{ name, seq string }{
			{"ctrl+d", "\x04"},
			{"delete", "\x1b[3~"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				c := openChatModel(t, PanelComposer)
				c = update(t, c, "a")
				c = update(t, c, "b")
				if !strings.Contains(c.composer.View(), "ab") {
					t.Fatalf("precondition: composer view = %q, want the "+
						"typed \"ab\"", c.composer.View())
				}
				c = update(t, c, "\x1b[H") // home
				c = update(t, c, tc.seq)
				view := c.composer.View()
				if strings.Contains(view, "ab") {
					t.Errorf("after home + %s the view still shows \"ab\": %q",
						tc.name, view)
				}
				if !strings.Contains(view, "b") {
					t.Errorf("after home + %s the view lost the wrong "+
						"character: %q", tc.name, view)
				}
			})
		}

		// end returns the cursor to the line end, where a delete has
		// nothing under it to remove.
		c := openChatModel(t, PanelComposer)
		c = update(t, c, "a")
		c = update(t, c, "b")
		c = update(t, c, "\x1b[H") // home
		c = update(t, c, "\x1b[F") // end
		c = update(t, c, "\x04")   // ctrl+d
		if view := c.composer.View(); !strings.Contains(view, "ab") {
			t.Errorf("end did not return the cursor to the line end: after "+
				"home, end, ctrl+d the view is %q, want \"ab\" intact", view)
		}
	})
}

// withDialog is a main-screen model with a confirm dialog up, focused on
// the given panel.
func withDialog(t *testing.T, focus FocusPanel) Model {
	t.Helper()
	m := openChatModel(t, focus)
	d := dialog.NewConfirm(m.theme, "delete", "Delete Message", "Are you sure?")
	m.dialog = &d
	return m
}

// TestDialogIsModalForTheKeyboard covers three defects that shared one
// cause: app-level dispatch gated some bindings on m.dialog and forgot
// others, so a modal dialog was modal for the mouse and for rendering but
// not for the keys.
//
// The symptoms were worse than "a key did the wrong thing". Tab cycled
// panel focus BEHIND the modal — and tab is the first key the dialog's own
// hint line advertises as the way to choose a button, so the one key it
// told you to press was the one key that did nothing to it. Escape moved
// focus on the first press and only cancelled on the second. The panel
// focus keys moved focus with the dialog still waiting for an answer.
func TestDialogIsModalForTheKeyboard(t *testing.T) {
	// The keys that used to act on the panels behind the dialog. Each must
	// now leave focus alone and leave the dialog standing.
	t.Run("panel keys do not fire behind it", func(t *testing.T) {
		for _, tc := range []struct{ name, seq string }{
			{"tab", "\t"},
			{"shift+tab", "\x1b[Z"},
			{"alt+1", "\x1b1"},
			{"alt+2", "\x1b[50;3;8482u"}, // kitty, with composed text
			{"alt+3", "\x1b3"},
			{"f1", "\x1bOP"},
			{"f3", "\x1bOR"},
			{"h", "h"},
			{"l", "l"},
			{"q", "q"},
			{"i", "i"},
			{"?", "?"},
			{"/", "/"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				for _, panel := range []FocusPanel{
					PanelChatList, PanelChatView, PanelComposer,
				} {
					m := withDialog(t, panel)
					got, cmd := updateCmd(t, m, tc.seq)
					if got.focus != panel {
						t.Errorf("from %v: %s moved focus to %v behind the "+
							"dialog", panel, tc.name, got.focus)
					}
					if got.dialog == nil || !got.dialog.IsVisible() {
						t.Errorf("from %v: %s dismissed the dialog", panel, tc.name)
					}
					if got.help.IsVisible() || got.search.IsVisible() ||
						got.contacts.IsVisible() {
						t.Errorf("from %v: %s opened an overlay over the dialog",
							panel, tc.name)
					}
					if quits(cmd) {
						t.Errorf("from %v: %s quit while the dialog was up",
							panel, tc.name)
					}
				}
			})
		}
	})

	// The positive half: the keys the dialog's hint line advertises have to
	// reach it. Tab is the one the bug ate.
	t.Run("tab reaches the dialog", func(t *testing.T) {
		m := withDialog(t, PanelChatView)
		before := m.dialog.View()
		got := update(t, m, "\t")
		if got.dialog == nil {
			t.Fatal("the dialog disappeared")
		}
		if got.dialog.View() == before {
			t.Error("tab did not move the dialog's selection — the key its " +
				"own hint line advertises first still does nothing")
		}
	})

	// One press, from every panel. The old behavior cost two from anywhere
	// but the chat list, and mutated focus underneath the modal on the way.
	t.Run("one esc cancels it", func(t *testing.T) {
		for _, panel := range []FocusPanel{
			PanelChatList, PanelChatView, PanelComposer,
		} {
			m := withDialog(t, panel)
			got, cmd := updateCmd(t, m, "\x1b")

			if got.dialog != nil && got.dialog.IsVisible() {
				t.Errorf("from %v: one esc left the dialog up", panel)
			}
			if got.focus != panel {
				t.Errorf("from %v: esc moved focus to %v while cancelling",
					panel, got.focus)
			}

			// It cancelled rather than confirmed, and the app clears the
			// dialog when that result comes back round.
			var result *dialog.DialogResultMsg
			for _, msg := range flattenCmd(cmd) {
				if r, ok := msg.(dialog.DialogResultMsg); ok {
					result = &r
				}
			}
			if result == nil {
				t.Errorf("from %v: esc produced no dialog result", panel)
				continue
			}
			if result.Confirmed {
				t.Errorf("from %v: esc CONFIRMED the dialog", panel)
			}
			out, _ := got.Update(*result)
			if next := out.(Model); next.dialog != nil {
				t.Errorf("from %v: the dialog outlived its own result", panel)
			}
		}
	})

	// A modal must never be able to trap someone in the program.
	t.Run("ctrl+c and ctrl+q still quit", func(t *testing.T) {
		for _, tc := range []struct{ name, seq string }{
			{"ctrl+c", "\x03"},
			{"ctrl+q", "\x11"},
		} {
			for _, panel := range []FocusPanel{PanelChatList, PanelComposer} {
				if _, cmd := updateCmd(t, withDialog(t, panel), tc.seq); !quits(cmd) {
					t.Errorf("from %v: %s did not quit with a dialog up",
						panel, tc.name)
				}
			}
		}
	})
}
