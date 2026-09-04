package app

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Ceesaxp/telegram-cli/internal/config"
	"github.com/Ceesaxp/telegram-cli/internal/keys"
	"github.com/Ceesaxp/telegram-cli/internal/store"
	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/chatlist"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/chatview"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/composer"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/dialog"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/help"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
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

// TestRetiredChordsAreInert is decision I-1's negative test, and the reason
// the retired bindings are worth a test at all: they are gone, and the way
// they used to fail — silently, on a terminal that would not report Option —
// is indistinguishable from the way an inert key fails. So the client has to
// be able to say which it is.
//
// Every encoding of every retired chord is driven through the real decoder,
// as the positive tests did, because that is what caught them being dead in
// the first place.
func TestRetiredChordsAreInert(t *testing.T) {
	cases := []struct {
		name string
		seqs []string
	}{
		// Panel focus: h, l, i, Esc and Tab cover it.
		{"alt+1", []string{"\x1b1", "\x1b[49;3u", "\x1b[49;3;161u", "\x1b[27;3;49~"}},
		{"alt+2", []string{"\x1b2", "\x1b[50;3u", "\x1b[50;3;8482u"}},
		{"alt+3", []string{"\x1b3", "\x1b[51;3u", "\x1b[51;3;163u"}},
		{"f1", []string{"\x1bOP"}},
		{"f2", []string{"\x1bOQ"}},
		{"f3", []string{"\x1bOR"}},
		// Contacts: c.
		{"alt+c", []string{"\x1bc", "\x1b[99;3u", "\x1b[99;3;231u"}},
		{"f4", []string{"\x1bOS"}},
		// Chat navigation: J and K.
		{"alt+j", []string{"\x1bj", "\x1b[106;3u", "\x1b[106;3;8710u"}},
		{"alt+k", []string{"\x1bk", "\x1b[107;3u", "\x1b[107;3;730u"}},
		// Folder cycling: [ and ].
		{"alt+h", []string{"\x1bh", "\x1b[104;3u", "\x1b[104;3;729u"}},
		{"alt+l", []string{"\x1bl", "\x1b[108;3u", "\x1b[108;3;172u"}},
	}
	for _, tc := range cases {
		for _, seq := range tc.seqs {
			t.Run(tc.name+"/"+seq, func(t *testing.T) {
				m := mainModel(t, PanelChatView)
				before := m.chatList.ActiveFolderID()

				next, cmd := updateCmd(t, m, seq)

				if next.focus != PanelChatView {
					t.Errorf("focus moved to %v", next.focus)
				}
				if next.contacts.IsVisible() {
					t.Error("the contacts overlay opened")
				}
				if next.chatList.ActiveFolderID() != before {
					t.Error("the folder changed")
				}
				if quits(cmd) {
					t.Error("it quit")
				}
			})
		}
	}
}

// TestRetiredLettersAreTypable is the other half of the ground rule: a
// retired binding whose spelling is a printable has to be typable in the
// composer. J, K, u and c are the plain spellings that replaced the alt
// chords, and every one of them is a letter somebody writes with.
func TestRetiredLettersAreTypable(t *testing.T) {
	m := openChatModel(t, PanelComposer)
	m = typeIntoComposer(t, m, "cJKu[]m")
	if got := m.composer.Draft(); got != "cJKu[]m" {
		t.Errorf("Draft = %q — a browsing-panel binding reached the composer", got)
	}
}

// TestContactsToggle covers the plain letter that replaced alt+c and its f4
// fallback (decision I-1). It is a toggle, so the key that opens the overlay
// has to close it.
func TestContactsToggle(t *testing.T) {
	m := update(t, mainModel(t, PanelChatList), "c")
	if !m.contacts.IsVisible() {
		t.Fatal("contacts not visible after c")
	}
	if m.focus != PanelContacts {
		t.Errorf("focus = %v, want PanelContacts", m.focus)
	}

	m = update(t, m, "c")
	if m.contacts.IsVisible() {
		t.Error("contacts still visible after a second c")
	}
	if m.focus != PanelChatList {
		t.Errorf("focus = %v after closing, want PanelChatList", m.focus)
	}

	// And from the chat view too — both browsing panels.
	if got := update(t, mainModel(t, PanelChatView), "c"); !got.contacts.IsVisible() {
		t.Error("c did not open contacts from the chat view")
	}
}

// TestFolderAndChatNavBindingsResolve pins the folder and chat-nav keys to
// the bindings Update gates CycleFolder and SelectDelta on. They are plain
// keys now, so the only thing worth asserting is that the resolved value is
// the one the dispatcher will see.
func TestFolderAndChatNavBindingsResolve(t *testing.T) {
	m := mainModel(t, PanelChatList)
	for seq, binding := range map[string]string{
		"[": m.keys.prevFolder,
		"]": m.keys.nextFolder,
		"J": m.keys.nextChat,
		"K": m.keys.prevChat,
		"u": m.keys.nextUnread,
		"c": m.keys.contacts,
		"i": m.keys.compose,
	} {
		t.Run(seq, func(t *testing.T) {
			if !keys.NewPress(decodeKey(t, seq)).Matches(binding) {
				t.Errorf("%q did not match binding %q", seq, binding)
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
		"compose":      m.keys.compose,
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

// TestFolderCyclingKeys covers folder cycling after the keymap cut. [ and ]
// are the whole binding now, at app level, working from BOTH browsing panels
// (decision I-1) — they used to be chat-list only, with alt+h/alt+l as the
// pair that worked anywhere and mostly did not work at all. The chat list
// keeps the arrows and the 1-9 jump, which are its own.
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

	// Each spelling walks the whole ring and wraps, so a key that is merely
	// swallowed rather than acted on fails here.
	forward := []int32{work, family, telegram.AllChatsFolderID}
	backward := []int32{family, work, telegram.AllChatsFolderID}
	for _, tc := range []struct {
		name  string
		seq   string
		panel FocusPanel
		want  []int32
	}{
		{"bracket next, chat list", "]", PanelChatList, forward},
		{"bracket prev, chat list", "[", PanelChatList, backward},
		{"bracket next, chat view", "]", PanelChatView, forward},
		{"bracket prev, chat view", "[", PanelChatView, backward},
		{"arrow next", "\x1b[C", PanelChatList, forward},
		{"arrow prev", "\x1b[D", PanelChatList, backward},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := seedFolders(t, sizedMainModel(t, tc.panel))
			for i, want := range tc.want {
				m = update(t, m, tc.seq)
				if got := m.chatList.ActiveFolderID(); got != want {
					t.Fatalf("press %d of %q: active folder = %d, want %d",
						i+1, tc.seq, got, want)
				}
			}
		})
	}

	// Cycling is consumed: no focus move, no overlay, and nothing put back
	// on the command loop.
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

	// Bare h/l do not touch the folder tabs from either browsing panel:
	// that role belongs entirely to [ / ], the arrows and the digits.
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
		m.keys.quit, m.keys.quitBrowsing, m.keys.compose,
		m.keys.search, m.keys.globalSearch, m.keys.contacts,
		m.keys.nextFolder, m.keys.prevFolder,
		m.keys.nextChat, m.keys.prevChat, m.keys.nextUnread,
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
		// i is the deliberate way in, from both browsing panels.
		for _, seq := range []string{"i"} {
			if got := update(t, m, seq); got.focus != PanelComposer {
				t.Errorf("panel %v: %q did not focus the composer (got %v)", panel, seq, got.focus)
			}
		}
	}
	// c is the contacts overlay, not compose. Both are bare letters now,
	// so what separates them is the binding, not a modifier.
	if got := update(t, mainModel(t, PanelChatList), "c"); !got.contacts.IsVisible() {
		t.Error("c no longer opens the contacts overlay")
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
	for _, seq := range []string{"\x1b" /* esc */, "?"} {
		if got := update(t, m, seq); got.help.IsVisible() {
			t.Errorf("%q did not close the help overlay", seq)
		}
	}

	// And q is not one of them (decision I-8). It used to close the card
	// and, one keystroke later, quit — so "?qq" was an exit nobody meant
	// to type. It is swallowed here like any other key behind the card.
	got, cmd := updateCmd(t, m, "q")
	if !got.help.IsVisible() {
		t.Error("q closed the help overlay; q closes no overlay")
	}
	if quits(cmd) {
		t.Error("q quit from behind the help overlay")
	}

	// Quit still outranks it — the overlay must not trap the user.
	if _, cmd := m.Update(decodeKey(t, "\x11")); cmd == nil {
		t.Error("ctrl+q produced no command while the help overlay was open")
	}
}

// TestHelpSectionsComeFromResolvedKeys is the anti-drift check: the overlay
// must describe the bindings the dispatcher actually matches, so rebinding a
// key has to change what the overlay says.
func TestHelpSectionsComeFromResolvedKeys(t *testing.T) {
	cfg := &config.Config{}
	cfg.Keys.Contacts = "ctrl+p"
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

	for _, want := range []string{"ctrl+p", "ctrl+y", "f12", "ctrl+x"} {
		if !strings.Contains(all, want) {
			t.Errorf("the overlay does not mention the configured %q", want)
		}
	}
	// The defaults these replaced must be gone, or the overlay is lying.
	for _, gone := range []string{"ctrl+g"} {
		if strings.Contains(all, gone) {
			t.Errorf("the overlay still advertises the replaced default %q", gone)
		}
	}

	// The hardcoded quit keys are always live, so they belong on the row
	// even when a third is configured.
	if row, ok := findBinding(m.helpSections(), "Quit"); !ok {
		t.Error("no Quit row")
	} else {
		for _, want := range []string{"ctrl+q", "ctrl+x"} {
			if !strings.Contains(row.Keys, want) {
				t.Errorf("Quit row %q omits %q", row.Keys, want)
			}
		}
	}

	// The footer names the configured help key, not a stale "?".
	if got := m.helpFooter(); !strings.Contains(got, "f12") || strings.Contains(got, "?") {
		t.Errorf("footer = %q, want it to name the configured help key", got)
	}
	if got := m.helpFooter(); !strings.Contains(got, "esc") {
		t.Errorf("footer %q omits the esc close key", got)
	}
	// And it does not offer q, which closes no overlay (decision I-8).
	if got := m.helpFooter(); strings.Contains(got, "q to close") ||
		strings.Contains(got, "/ q ") {
		t.Errorf("footer %q still advertises q as a way out of the card", got)
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
	// ctrl+q quits; ctrl+v pastes. Everything else falls through.
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
	c := update(t, mainModel(t, PanelChatList), "c")
	if !c.contacts.IsVisible() {
		t.Fatal("precondition: contacts did not open")
	}
	if c = update(t, c, "/"); c.search.IsVisible() {
		t.Error("\"/\" inside the contacts overlay opened the global search")
	}
}

// TestResolveKeysDefaults pins the defaults app.go falls back to when a
// config predates a field (or is the zero value used by these tests). They
// are the table in docs/interaction-model.md's "Configuration" section.
func TestResolveKeysDefaults(t *testing.T) {
	want := map[string]string{
		"quit": "ctrl+q", "quitBrowsing": "q",
		"search": "/", "globalSearch": "ctrl+g",
		"contacts": "c", "compose": "i", "help": "?",
		"nextChat": "J", "prevChat": "K", "nextUnread": "u",
		"nextFolder": "]", "prevFolder": "[",
		// Handed to chatview rather than dispatched here; the defaults
		// are the keys chatview hardcoded before it took them from config.
		"reply": "r", "editMessage": "e", "deleteMessage": "d",
		"markRead": "m",
	}
	k := resolveKeys(config.KeyConfig{})
	got := map[string]string{
		"quit": k.quit, "quitBrowsing": k.quitBrowsing,
		"search": k.search, "globalSearch": k.globalSearch,
		"contacts": k.contacts, "compose": k.compose, "help": k.help,
		"nextChat": k.nextChat, "prevChat": k.prevChat, "nextUnread": k.nextUnread,
		"nextFolder": k.nextFolder, "prevFolder": k.prevFolder,
		"reply": k.reply, "editMessage": k.editMessage, "deleteMessage": k.deleteMessage,
		"markRead": k.markRead,
	}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("%s = %q, want %q", name, got[name], w)
		}
	}
	// A configured value wins and is normalized on the way in.
	if k := resolveKeys(config.KeyConfig{Contacts: "CTRL+P"}); k.contacts != "ctrl+p" {
		t.Errorf("configured contacts = %q, want ctrl+p", k.contacts)
	}
}

// TestResolveKeysRefusesCollisions is decision I-13's single rule at the
// app level: a value replaces the default, and a value that collides with
// something already bound is refused rather than allowed to shadow it.
func TestResolveKeysRefusesCollisions(t *testing.T) {
	t.Run("with a key the app hardcodes", func(t *testing.T) {
		// "h" moves between panels. contacts cannot have it.
		k := resolveKeys(config.KeyConfig{Contacts: "h"})
		if k.contacts != "c" {
			t.Errorf("contacts = %q, want the default kept", k.contacts)
		}
	})

	t.Run("with another field", func(t *testing.T) {
		// An explicit setting outranks a default whatever the field order,
		// so the explicit one wins and the other falls back.
		k := resolveKeys(config.KeyConfig{Contacts: "u"})
		if k.contacts != "u" {
			t.Errorf("contacts = %q, want the explicit u", k.contacts)
		}
		if k.nextUnread != "" {
			t.Errorf("nextUnread = %q, want it left unbound rather than sharing u",
				k.nextUnread)
		}
	})

	t.Run("two explicit settings", func(t *testing.T) {
		// Field order is the tie-break, and only between two settings the
		// user has already made ambiguous. contacts comes first.
		k := resolveKeys(config.KeyConfig{Contacts: "f7", NextUnread: "f7"})
		if k.contacts != "f7" {
			t.Errorf("contacts = %q, want f7", k.contacts)
		}
		if k.nextUnread != "u" {
			t.Errorf("nextUnread = %q, want its default back", k.nextUnread)
		}
	})

	t.Run("a refused binding is inert, not misdirected", func(t *testing.T) {
		// An unbound action must never fire on somebody else's key:
		// Press.Matches treats "" as never matching, which is what makes
		// the empty string a safe way to say "unreachable".
		k := resolveKeys(config.KeyConfig{Contacts: "u"})
		if keys.NewPress(decodeKey(t, "u")).Matches(k.nextUnread) {
			t.Error("the unbound next_unread still matched a key")
		}
	})
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
			overlay := update(t, openChatModel(t, panel), "c") // contacts
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
				return update(t, openChatModel(t, PanelChatList), "c")
			},
			"dialog": func(t *testing.T) Model {
				m := openChatModel(t, PanelChatList)
				d := dialog.NewConfirm(m.roles, "delete", "Delete Message", "Are you sure?")
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
		// Including the help card, which q used to close (decision I-8).
		// It neither closes it nor quits behind it.
		h := update(t, openChatModel(t, PanelChatList), "?")
		if !h.help.IsVisible() {
			t.Fatal("precondition: help did not open")
		}
		got, cmd := updateCmd(t, h, "q")
		if quits(cmd) {
			t.Error("q quit from behind the help overlay")
		}
		if !got.help.IsVisible() {
			t.Error("q closed the help overlay; q closes no overlay")
		}

		for name, setup := range map[string]func(t *testing.T) Model{
			"global search": func(t *testing.T) Model {
				return update(t, openChatModel(t, PanelChatList), "\x07")
			},
			"contacts": func(t *testing.T) Model {
				return update(t, openChatModel(t, PanelChatList), "c")
			},
			"dialog": func(t *testing.T) Model {
				m := openChatModel(t, PanelChatList)
				d := dialog.NewConfirm(m.roles, "delete", "Delete Message", "Are you sure?")
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
	labels := func(m Model, s Surface) map[string]string {
		out := map[string]string{}
		for _, h := range m.hintsFor(s) {
			out[h.Label] = h.Key
		}
		return out
	}

	m := mainModel(t, PanelChatList)
	for label, want := range map[string]string{"keymap": "?", "quit": "q", "compose": "i", "unread": "u"} {
		if got := labels(m, SurfaceChatList)[label]; got != want {
			t.Errorf("default chat list hint for %q is %q, want %q", label, got, want)
		}
	}
	for label, want := range map[string]string{"reply": "r", "edit": "e", "find": "/"} {
		if got := labels(m, SurfaceChatView)[label]; got != want {
			t.Errorf("default chat view hint for %q is %q, want %q", label, got, want)
		}
	}

	cfg := &config.Config{}
	cfg.Keys.Help = "f12"
	cfg.Keys.QuitBrowsing = "ctrl+x"
	cfg.Keys.Reply = "ctrl+r"
	cfg.Keys.NextUnread = "f7"
	rebound := New(cfg, nil, store.NewStore(), telegram.NewTUIAuthorizer(cfg))
	for label, want := range map[string]string{"keymap": "f12", "quit": "ctrl+x", "unread": "f7"} {
		if got := labels(rebound, SurfaceChatList)[label]; got != want {
			t.Errorf("rebound chat list hint for %q is %q, want %q", label, got, want)
		}
	}
	if got := labels(rebound, SurfaceChatView)["reply"]; got != "ctrl+r" {
		t.Errorf("rebound reply hint is %q, want ctrl+r", got)
	}

	// The bar is an abbreviation that points at the help card, not a second
	// copy of it: it drops hints from the right when they do not fit, so a
	// long list means the ones that matter go missing on a narrow terminal.
	for _, s := range allSurfaces() {
		if got := len(m.hintsFor(s)); got > 9 {
			t.Errorf("%v has %d hints, too many to survive a narrow terminal", s, got)
		}
	}
}

// allSurfaces is every surface the resolver can return, for the tests that
// have to walk all of them. A new surface added to the enum and not here
// fails TestEverySurfaceIsReachable rather than being quietly untested.
func allSurfaces() []Surface {
	return []Surface{
		SurfaceChatList, SurfaceChatView,
		SurfaceComposerInsert, SurfaceComposerVi,
		SurfaceReactions, SurfaceAttach, SurfacePalette, SurfaceMedia,
		SurfaceHelp, SurfaceDialog, SurfaceSearch, SurfaceContacts,
		SurfaceAuth, SurfaceLoading,
	}
}

// TestChatViewKeysComeFromConfig covers the plumbing that ended config
// fields being advertised and ignored: reply/edit/delete and mark_read are
// resolved here and handed to the panel that implements them.
//
// The help card reads them back through chatview.ActiveKeys rather than from
// resolvedKeys, because chatview refuses a binding that would shadow a key it
// already owns — and a card that advertised the refused binding would be the
// exact drift this wave exists to remove.
func TestChatViewKeysComeFromConfig(t *testing.T) {
	cfg := &config.Config{}
	// Keys the chat view does not already claim. "a" has moved twice — p
	// became pin, t became threads — and a configured mnemonic colliding
	// with a claimed one is refused rather than double-bound, which is the
	// collision resolver working rather than a failure to configure.
	cfg.Keys.Reply = "a"
	cfg.Keys.EditMessage = "CTRL+E"
	cfg.Keys.DeleteMessage = "v"
	cfg.Keys.MarkRead = "w"
	k := resolveKeys(cfg.Keys)

	// Normalized on the way in, like every other configured binding.
	if k.editMessage != "ctrl+e" {
		t.Errorf("editMessage = %q, want the normalized ctrl+e", k.editMessage)
	}

	m := New(cfg, nil, store.NewStore(), telegram.NewTUIAuthorizer(cfg))
	row, ok := findBinding(m.helpSections(), "Reply / edit / delete")
	if !ok {
		t.Fatal("no reply/edit/delete row")
	}
	// ctrl+e is one of the chat view's own buffer motions, so the panel
	// refuses it and keeps "e" — and the card must quote what the panel
	// matches, not what the file asked for.
	if row.Keys != "a / e / v" {
		t.Errorf("reply row = %q, want \"a / e / v\" (the refused ctrl+e falling back)",
			row.Keys)
	}
	mark, ok := findBinding(m.helpSections(), "Mark this chat read")
	if !ok {
		t.Fatal("no mark-read row")
	}
	if mark.Keys != "w" {
		t.Errorf("mark-read row = %q, want the configured w", mark.Keys)
	}

	// The defaults leave the rows exactly as they read before chatview
	// became configurable.
	plain := New(&config.Config{}, nil, store.NewStore(),
		telegram.NewTUIAuthorizer(&config.Config{}))
	if row, ok := findBinding(plain.helpSections(), "Reply / edit / delete"); !ok || row.Keys != "r / e / d" {
		t.Errorf("default reply row = %q, want \"r / e / d\"", row.Keys)
	}
	if row, ok := findBinding(plain.helpSections(), "Mark this chat read"); !ok || row.Keys != "m" {
		t.Errorf("default mark-read row = %q, want \"m\"", row.Keys)
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

// TestEverySurfaceIsReachable pins the resolver against the enum: a surface
// nothing can produce is a hint set nobody will ever see, and a surface the
// resolver returns that allSurfaces does not list is one the drift tests
// below silently skip.
func TestEverySurfaceIsReachable(t *testing.T) {
	reach := map[Surface]surfaceInputs{
		SurfaceChatList:       {screen: ScreenMain, focus: PanelChatList},
		SurfaceChatView:       {screen: ScreenMain, focus: PanelChatView},
		SurfaceComposerInsert: {screen: ScreenMain, focus: PanelComposer},
		SurfaceComposerVi: {screen: ScreenMain, focus: PanelComposer,
			composerViNormal: true},
		SurfaceReactions: {screen: ScreenMain, reactionsOpen: true},
		SurfaceAttach:    {screen: ScreenMain, attachOpen: true},
		SurfacePalette:   {screen: ScreenMain, paletteOpen: true},
		SurfaceMedia:     {screen: ScreenMain, mediaOpen: true},
		SurfaceHelp:      {screen: ScreenMain, helpOpen: true},
		SurfaceDialog:    {screen: ScreenMain, dialogOpen: true},
		SurfaceSearch:    {screen: ScreenMain, searchOpen: true},
		SurfaceContacts:  {screen: ScreenMain, contactsOpen: true},
		SurfaceAuth:      {screen: ScreenAuth},
		SurfaceLoading:   {screen: ScreenLoading},
	}

	for _, s := range allSurfaces() {
		in, ok := reach[s]
		if !ok {
			t.Errorf("%v has no state that produces it", s)
			continue
		}
		if got := resolveSurface(in); got != s {
			t.Errorf("%+v resolved to %v, want %v", in, got, s)
		}
	}
	if len(reach) != len(allSurfaces()) {
		t.Errorf("%d surfaces listed, %d reachable", len(allSurfaces()), len(reach))
	}
}

// TestOverlaysOutrankFocus: an overlay owns the keyboard regardless of which
// panel is focused behind it. Getting this backwards would draw the
// composer's hints while a confirm dialog sat over it — which is one of the
// four places the mode-keyed bar named inert keys (decision I-6).
func TestOverlaysOutrankFocus(t *testing.T) {
	base := surfaceInputs{screen: ScreenMain, focus: PanelComposer}
	cases := map[Surface]func(*surfaceInputs){
		SurfaceReactions: func(in *surfaceInputs) { in.reactionsOpen = true },
		SurfaceAttach:    func(in *surfaceInputs) { in.attachOpen = true },
		SurfacePalette:   func(in *surfaceInputs) { in.paletteOpen = true },
		SurfaceMedia:     func(in *surfaceInputs) { in.mediaOpen = true },
		SurfaceHelp:      func(in *surfaceInputs) { in.helpOpen = true },
		SurfaceDialog:    func(in *surfaceInputs) { in.dialogOpen = true },
		SurfaceSearch:    func(in *surfaceInputs) { in.searchOpen = true },
		SurfaceContacts:  func(in *surfaceInputs) { in.contactsOpen = true },
	}
	for want, open := range cases {
		in := base
		open(&in)
		if got := resolveSurface(in); got != want {
			t.Errorf("%v over a focused composer resolved to %v", want, got)
		}
	}
}

// TestTheHintBarFollowsTheSurface is decision I-6 end to end. The bar was
// keyed by MODE, so the chat-view set showed in the chat list, under
// contacts, under a confirm dialog and in a vi composer — four surfaces that
// share ModeNormal and agree on almost no keys.
func TestTheHintBarFollowsTheSurface(t *testing.T) {
	hints := func(m Model) string {
		m.refreshChrome()
		return ansi.Strip(m.hintBar.View())
	}

	t.Run("the chat list gets its own set, not the chat view's", func(t *testing.T) {
		got := hints(sizedMainModel(t, PanelChatList))
		if !strings.Contains(got, "filter") {
			t.Errorf("the chat list bar omits its filter hint: %q", got)
		}
		if strings.Contains(got, "reply") || strings.Contains(got, "yank") {
			t.Errorf("the chat list bar advertises chat view keys: %q", got)
		}
	})

	t.Run("contacts gets the contacts set", func(t *testing.T) {
		m := update(t, sizedMainModel(t, PanelChatList), "c")
		if !m.contacts.IsVisible() {
			t.Fatal("precondition: contacts did not open")
		}
		got := hints(m)
		if !strings.Contains(got, "open") || !strings.Contains(got, "close") {
			t.Errorf("the contacts bar is not the contacts set: %q", got)
		}
		if strings.Contains(got, "quit") || strings.Contains(got, "reply") {
			t.Errorf("the contacts bar advertises keys contacts does not have: %q", got)
		}
	})

	t.Run("a dialog gets the dialog set", func(t *testing.T) {
		m := sizedMainModel(t, PanelChatView)
		d := dialog.NewConfirm(m.roles, "quit", "Quit", "Discard the draft?")
		m.dialog = &d
		got := hints(m)
		if !strings.Contains(got, "answer") || !strings.Contains(got, "accept") {
			t.Errorf("the dialog bar is not the dialog set: %q", got)
		}
		if strings.Contains(got, "reply") {
			t.Errorf("the dialog bar advertises chat view keys: %q", got)
		}
	})

	t.Run("a vi composer in command state gets the VI set", func(t *testing.T) {
		m := openChatModel(t, PanelComposer)
		m.width, m.height = 100, 40
		m.updateLayout()
		m.composer.SetEditingMode(composer.ModeVi)
		m = update(t, m, "\x1b")
		if !m.composer.IsViNormalMode() {
			t.Fatal("precondition: esc did not reach vi normal mode")
		}
		got := hints(m)
		if !strings.Contains(got, "insert") || !strings.Contains(got, "command") {
			t.Errorf("the vi bar is not the VI set: %q", got)
		}
		if strings.Contains(got, "newline") {
			t.Errorf("the vi bar advertises ctrl+j, which inserts nothing there: %q", got)
		}
	})
}

// TestThereIsOneHintRow. The chat list drew a second one inside its own
// column, and with the bar keyed by the live surface that row could only be
// redundant or wrong: it repeated the bar's first three hints whenever the
// list had focus, and described the CHAT LIST's keys — "l open", "/ filter"
// — while the chat view had the keyboard and those keys meant something
// else. It is gone; the bar is the hint row.
func TestThereIsOneHintRow(t *testing.T) {
	for _, panel := range []FocusPanel{PanelChatList, PanelChatView} {
		t.Run(fmt.Sprint(panel), func(t *testing.T) {
			m := sizedMainModel(t, panel)
			m.chatList.MarkLoadedForTest()
			m.refreshChrome()

			rows := strings.Split(ansi.Strip(m.View().Content), "\n")
			var hintRows int
			for _, row := range rows {
				// A hint row is the one that pairs keys with labels. The
				// bar is the only row that should.
				if strings.Contains(row, "j/k ") {
					hintRows++
				}
			}
			if hintRows != 1 {
				t.Errorf("%d rows carry hints, want exactly one:\n%s",
					hintRows, strings.Join(rows[len(rows)-3:], "\n"))
			}
		})
	}
}

// TestTheHintBarSaysHowToClearAFilter: the way out of a filter was the one
// thing the chat list's footer said that the bar could not, because it is
// this panel's own state rather than a binding. It is in the registry now —
// a reader who cannot see how to widen a narrowed list is left wondering
// where their chats went.
func TestTheHintBarSaysHowToClearAFilter(t *testing.T) {
	m := sizedMainModel(t, PanelChatList)
	m.chatList.MarkLoadedForTest()

	// Unfiltered, the set leads with the motions.
	m.refreshChrome()
	if got := ansi.Strip(m.hintBar.View()); strings.Contains(got, "clear") {
		t.Errorf("an unfiltered list advertises a way out of a filter: %q", got)
	}

	// Typing one: esc clears, enter keeps.
	m = update(t, m, "/")
	if !m.chatList.FilterActive() {
		t.Fatal("precondition: the filter input did not open")
	}
	m.refreshChrome()
	bar := ansi.Strip(m.hintBar.View())
	for _, want := range []string{"esc", "clear", "enter", "keep"} {
		if !strings.Contains(bar, want) {
			t.Errorf("the bar omits %q while the filter input is open: %q", want, bar)
		}
	}

	// Applied but closed — the state with no cursor blinking to explain
	// why the list is short.
	m = typeIntoComposer(t, m, "al")
	m = update(t, m, "\r")
	if m.chatList.FilterActive() || m.chatList.FilterQuery() == "" {
		t.Fatalf("precondition: filter active=%v query=%q",
			m.chatList.FilterActive(), m.chatList.FilterQuery())
	}
	m.refreshChrome()
	if got := ansi.Strip(m.hintBar.View()); !strings.Contains(got, "clear filter") {
		t.Errorf("the bar omits the way out of an applied filter: %q", got)
	}
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
	if motion, ok := findBinding(m.helpSections(), "Cursor to the next"); !ok ||
		motion.Keys != "j / k" {
		t.Errorf("motion row = %q, want the untouched \"j / k\"", motion.Keys)
	}
}

// TestContactsRespectsModals covers the gate the review found missing: the
// contacts toggle had no overlay guard at all, so it opened the contacts
// panel out from under a modal dialog that was still waiting for an answer.
//
// The gate is deliberately NOT the usual noOverlay: contacts is a toggle,
// and gating it on "no overlay is up" would leave the key that opens the
// panel unable to close it.
func TestContactsRespectsModals(t *testing.T) {
	t.Run("a dialog cannot be bypassed", func(t *testing.T) {
		m := openChatModel(t, PanelChatList)
		d := dialog.NewConfirm(m.roles, "delete", "Delete Message", "Are you sure?")
		m.dialog = &d
		got := update(t, m, "c")
		if got.contacts.IsVisible() {
			t.Error("c opened contacts over an open dialog")
		}
		if got.dialog == nil {
			t.Error("c dismissed the dialog")
		}
	})

	t.Run("the search overlay cannot be bypassed", func(t *testing.T) {
		m := update(t, openChatModel(t, PanelChatList), "\x07") // ctrl+g
		if !m.search.IsVisible() {
			t.Fatal("precondition: global search did not open")
		}
		if got := update(t, m, "c"); got.contacts.IsVisible() {
			t.Error("c opened contacts over the search overlay")
		}
	})

	t.Run("still toggles itself closed", func(t *testing.T) {
		m := update(t, openChatModel(t, PanelChatList), "c")
		if !m.contacts.IsVisible() {
			t.Fatal("precondition: contacts did not open")
		}
		if got := update(t, m, "c"); got.contacts.IsVisible() {
			t.Error("c could no longer close the panel it opened")
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

	// And the counter-claim, which is now the whole rest of the list: every
	// bare letter the browsing panels bind is TEXT here. contacts is on
	// this side of the line since decision I-1 made it "c" — alt+c was not
	// a character and could fire from the composer; "c" is one and cannot.
	for _, seq := range []string{"q", "c", "J", "K", "u", "[", "]", "i", "?"} {
		p := openChatModel(t, PanelComposer)
		got, cmd := updateCmd(t, p, seq)
		if quits(cmd) {
			t.Errorf("%q quit from the composer", seq)
		}
		if got.contacts.IsVisible() || got.help.IsVisible() || got.search.IsVisible() {
			t.Errorf("%q opened an overlay from the composer", seq)
		}
		if got.focus != PanelComposer {
			t.Errorf("%q moved focus out of the composer", seq)
		}
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
			m.keys.quit, m.keys.quitBrowsing, m.keys.compose,
			m.keys.search, m.keys.globalSearch, m.keys.contacts,
			m.keys.help, m.keys.nextFolder, m.keys.prevFolder,
			m.keys.nextChat, m.keys.prevChat, m.keys.nextUnread,
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
	d := dialog.NewConfirm(m.roles, "delete", "Delete Message", "Are you sure?")
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
	t.Run("ctrl+q still quits", func(t *testing.T) {
		for _, tc := range []struct{ name, seq string }{
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

// --- decision I-3: Escape never discards typed text -----------------------

// typeIntoComposer drives the whole app with the bytes a terminal sends for
// each character, so the text arrives at the composer the way it does in the
// running client — through Update's dispatch — rather than being planted.
func typeIntoComposer(t *testing.T, m Model, text string) Model {
	t.Helper()
	for _, r := range text {
		m = update(t, m, string(r))
	}
	return m
}

// TestEscKeepsTheDraftAcrossTheLadder is the end-to-end form of I-3: the
// cancel rung took the text with it, so a reply typed and then thought
// better of cost the words as well as the target. q asks before dropping
// the same text; esc did not ask at all.
func TestEscKeepsTheDraftAcrossTheLadder(t *testing.T) {
	for _, tc := range []struct {
		name    string
		editing composer.EditingMode
		escapes int // presses needed to reach the cancel rung
	}{
		{"emacs", composer.ModeEmacs, 1},
		{"vi", composer.ModeVi, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := openChatModel(t, PanelComposer)
			m.composer.SetEditingMode(tc.editing)
			m = typeIntoComposer(t, m, "half a thought")
			m.composer.EnterReplyMode(7, "someone: hello")

			for range tc.escapes {
				m = update(t, m, "\x1b")
			}

			if got := m.composer.Draft(); got != "half a thought" {
				t.Errorf("Draft = %q, want the text to survive the cancel", got)
			}
			if m.composer.IsComposing() {
				t.Error("reply target survived the cancel")
			}
			if m.focus != PanelComposer {
				t.Errorf("focus = %v, want the composer to keep it", m.focus)
			}
		})
	}
}

// TestEscAfterAnEditRestoresTheDraft: e loaded the message over whatever was
// half-written and the draft was gone. It is parked now, and the cancel
// hands it back.
func TestEscAfterAnEditRestoresTheDraft(t *testing.T) {
	m := openChatModel(t, PanelComposer)
	m = typeIntoComposer(t, m, "unsent")
	m.composer.EnterEditMode(99, "the old message")

	m = update(t, m, "\x1b")

	if got := m.composer.Draft(); got != "unsent" {
		t.Errorf("Draft = %q, want the parked draft back", got)
	}
	if m.composer.IsEditing() {
		t.Error("still editing after Esc")
	}
}

// --- decision I-7: the delete confirm says for whom -----------------------

// deleteDialog is the model with the real delete confirm up, opened the way
// pressing d opens it, so the test sees the button set the app actually
// builds rather than one written out beside it.
func deleteDialog(t *testing.T) Model {
	t.Helper()
	m := openChatModel(t, PanelChatView)
	out, _ := m.handleMessageAction(chatview.MessageActionMsg{
		Action: "delete", ChatId: testChatID, MessageId: 7,
	})
	got := out.(Model)
	if got.dialog == nil || !got.dialog.IsVisible() {
		t.Fatal("d opened no delete dialog")
	}
	return got
}

// TestDeleteConfirmOffersBothReaches: "Are you sure?" named nothing and
// deleted for everyone. The reach of a delete is a decision, and a dialog
// that makes it silently is making it for the user.
func TestDeleteConfirmOffersBothReaches(t *testing.T) {
	m := deleteDialog(t)
	plain := ansi.Strip(m.dialog.View())

	for _, want := range []string{"Delete this message?", "Ca(n)cel", "For (m)e", "For (e)veryone"} {
		if !strings.Contains(plain, want) {
			t.Errorf("the delete dialog is missing %q:\n%s", want, plain)
		}
	}
	if strings.Contains(plain, "Are you sure?") {
		t.Errorf("the delete dialog still asks a question that names nothing:\n%s", plain)
	}
}

// TestDeleteAnswersChooseTheReach walks the three answers to the dialog
// itself. What each answer DOES is deleteRevokes's job, tested below,
// because the command it builds needs a live Telegram client to run.
func TestDeleteAnswersChooseTheReach(t *testing.T) {
	cases := []struct {
		key       rune
		wantValue string
	}{
		{'n', deleteCancel},
		{'m', deleteForMe},
		{'e', deleteForEveryone},
	}
	for _, tc := range cases {
		t.Run(string(tc.key), func(t *testing.T) {
			m := deleteDialog(t)
			d, cmd := m.dialog.Update(tea.KeyPressMsg(tea.Key{
				Code: tc.key, Text: string(tc.key),
			}))
			if d.IsVisible() {
				t.Errorf("%q left the dialog open", string(tc.key))
			}
			if cmd == nil {
				t.Fatalf("%q produced no result", string(tc.key))
			}
			res, ok := cmd().(dialog.DialogResultMsg)
			if !ok {
				t.Fatalf("%q produced %T, want DialogResultMsg", string(tc.key), cmd())
			}
			if res.Value != tc.wantValue {
				t.Errorf("%q chose %q, want %q", string(tc.key), res.Value, tc.wantValue)
			}
		})
	}
}

// TestEnterOnTheDeleteConfirmDeletesNothing: Enter accepts the highlighted
// button, and the highlighted button is Cancel. This is the reflex the
// dialog exists to survive.
func TestEnterOnTheDeleteConfirmDeletesNothing(t *testing.T) {
	m := deleteDialog(t)
	_, cmd := m.dialog.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if cmd == nil {
		t.Fatal("enter produced no result")
	}
	res := cmd().(dialog.DialogResultMsg)
	if res.Confirmed {
		t.Errorf("enter on a fresh delete confirm deleted: %+v", res)
	}
	if _, ok := deleteRevokes(res.Value); ok {
		t.Errorf("enter's answer %q deletes something", res.Value)
	}
}

// TestDeleteRevokes is the rule the dialog's answers feed: "for everyone"
// revokes, "for me" does not, and anything else — a cancel, an Escape's
// empty value, a value from some future dialog — deletes nothing at all.
// The safe direction for a destructive action is to do nothing.
func TestDeleteRevokes(t *testing.T) {
	cases := []struct {
		answer     string
		wantRevoke bool
		wantOK     bool
	}{
		{deleteForEveryone, true, true},
		{deleteForMe, false, true},
		{deleteCancel, false, false},
		{"", false, false},
		{"something else", false, false},
	}
	for _, tc := range cases {
		revoke, ok := deleteRevokes(tc.answer)
		if revoke != tc.wantRevoke || ok != tc.wantOK {
			t.Errorf("deleteRevokes(%q) = (%v, %v), want (%v, %v)",
				tc.answer, revoke, ok, tc.wantRevoke, tc.wantOK)
		}
	}
}

// TestQuitConfirmAnswersToYAndN: the quit confirm gets the accelerators for
// free, because it is the same component. y quits, n returns.
func TestQuitConfirmAnswersToYAndN(t *testing.T) {
	for _, tc := range []struct {
		key      rune
		wantQuit bool
	}{{'y', true}, {'n', false}} {
		t.Run(string(tc.key), func(t *testing.T) {
			m := openChatModel(t, PanelComposer)
			m = typeIntoComposer(t, m, "half a thought")
			m.setFocus(PanelChatList)
			m = update(t, m, "q")
			if m.dialog == nil {
				t.Fatal("q with a draft opened no confirm")
			}

			d, cmd := m.dialog.Update(tea.KeyPressMsg(tea.Key{
				Code: tc.key, Text: string(tc.key),
			}))
			if d.IsVisible() {
				t.Errorf("%q left the confirm open", string(tc.key))
			}
			res := cmd().(dialog.DialogResultMsg)
			if res.Confirmed != tc.wantQuit {
				t.Errorf("%q gave Confirmed = %v, want %v", string(tc.key), res.Confirmed, tc.wantQuit)
			}

			out, quitCmd := m.Update(res)
			if got := quits(quitCmd); got != tc.wantQuit {
				t.Errorf("%q: quit = %v, want %v", string(tc.key), got, tc.wantQuit)
			}
			if out.(Model).dialog != nil {
				t.Errorf("%q left the dialog on the model", string(tc.key))
			}
		})
	}
}

// TestARefusedQuitKeyLeavesTheLetterTypable is decision I-13's sharpest
// edge, from the dispatcher's side: quit is matched before every focus
// gate, so quit = "x" meant that pressing x while writing a message quit
// the application. config refuses the binding; this is the proof that the
// refusal reaches the keymap the app actually dispatches on.
func TestARefusedQuitKeyLeavesTheLetterTypable(t *testing.T) {
	cfg := &config.Config{}
	cfg.Keys.Quit = "x"
	s := store.NewStore()
	var tg *telegram.Client
	m := New(cfg, tg, s, telegram.NewTUIAuthorizer(cfg))
	m.screen = ScreenMain
	m.composer.SetEditingMode(composer.ModeEmacs)
	m.chatView.OpenChat(testChatID, "Test Chat")
	m.composer.SetChatId(testChatID)
	m.setFocus(PanelComposer)

	if m.keys.quit != config.DefaultQuitKey {
		t.Fatalf("keys.quit resolved to %q, want the refused value replaced by %q",
			m.keys.quit, config.DefaultQuitKey)
	}

	next, cmd := updateCmd(t, m, "x")
	if quits(cmd) {
		t.Fatal("x quit the application from the composer")
	}
	if got := next.composer.Draft(); got != "x" {
		t.Errorf("Draft = %q, want the x to have been typed", got)
	}

	if _, cmd := updateCmd(t, next, "\x11"); !quits(cmd) {
		t.Error("ctrl+q no longer quits after the refusal")
	}
}

// --- wave 2: the keymap cut ----------------------------------------------

// seededChatList is a main-screen model whose chat list actually holds rows,
// which the keys that walk it need. Chats reach the list through the store
// and a refresh, the same way they do at runtime.
func seededChatList(t *testing.T, focus FocusPanel, unread ...int64) Model {
	t.Helper()
	m := mainModel(t, focus)
	m.width, m.height = 100, 40
	m.updateLayout()

	isUnread := map[int64]bool{}
	for _, id := range unread {
		isUnread[id] = true
	}
	for _, id := range []int64{101, 102, 103, 104} {
		chat := &telegram.Chat{
			ID:    id,
			Title: fmt.Sprintf("Chat %d", id),
			Type:  telegram.ChatTypePrivate,
		}
		if isUnread[id] {
			chat.UnreadCount = 3
		}
		m.store.Chats.Set(chat)
	}
	m.chatList.MarkLoadedForTest()
	_ = m.chatList.View() // the refresh the dirty flag is waiting for

	if got := m.chatList.Count(); got != 4 {
		t.Fatalf("seeded %d chats, want 4", got)
	}
	return m
}

// TestJKOpenTheNextChat covers decision I-1's replacement for alt+j/alt+k.
// They open a chat outright, which is what separates them from the chat
// list's own j/k — and they work from the chat view, where the chat list's
// motions do not reach.
func TestJKOpenTheNextChat(t *testing.T) {
	for _, panel := range []FocusPanel{PanelChatList, PanelChatView} {
		t.Run(fmt.Sprint(panel), func(t *testing.T) {
			m := seededChatList(t, panel)
			first := m.chatList.CursorChatId()

			next, cmd := updateCmd(t, m, "J")
			if next.chatList.CursorChatId() == first {
				t.Fatal("J did not move the cursor")
			}
			opened, ok := selectedChat(cmd)
			if !ok {
				t.Fatal("J opened no chat")
			}
			if opened != next.chatList.CursorChatId() {
				t.Errorf("J opened %d but the cursor is on %d",
					opened, next.chatList.CursorChatId())
			}
			// And it does not leave the panel it was pressed in.
			if next.focus != panel {
				t.Errorf("J moved focus from %v to %v", panel, next.focus)
			}

			back, cmd := updateCmd(t, next, "K")
			if got, _ := selectedChat(cmd); got != first {
				t.Errorf("K opened %d, want back to %d", got, first)
			}
			if back.chatList.CursorChatId() != first {
				t.Error("K did not move the cursor back")
			}
		})
	}
}

// TestUOpensTheNextUnreadChat: the chat list footer advertised "u unread"
// for a release with nothing bound to it, which is how decision I-6 came to
// require every hint to be derived rather than written.
func TestUOpensTheNextUnreadChat(t *testing.T) {
	m := seededChatList(t, PanelChatList, 103)

	next, cmd := updateCmd(t, m, "u")
	got, ok := selectedChat(cmd)
	if !ok {
		t.Fatal("u opened no chat")
	}
	if got != 103 {
		t.Errorf("u opened %d, want the unread chat 103", got)
	}
	if next.chatList.CursorChatId() != 103 {
		t.Errorf("the cursor is on %d, want 103", next.chatList.CursorChatId())
	}
}

// TestUWithNothingUnreadReportsRatherThanMoving: a key that silently does
// nothing is a key people learn is broken.
func TestUWithNothingUnreadReportsRatherThanMoving(t *testing.T) {
	m := seededChatList(t, PanelChatList)
	before := m.chatList.CursorChatId()

	next, cmd := updateCmd(t, m, "u")
	if _, ok := selectedChat(cmd); ok {
		t.Error("u opened a chat with nothing unread")
	}
	if next.chatList.CursorChatId() != before {
		t.Error("u moved the cursor with nothing unread")
	}
	if !strings.Contains(next.composer.View(), "no unread") &&
		!strings.Contains(next.hintBar.View(), "no unread") {
		t.Error("u said nothing when there was nothing to go to")
	}
}

// selectedChat reads the chat a command asks to open, if it does.
func selectedChat(cmd tea.Cmd) (int64, bool) {
	for _, msg := range flattenCmd(cmd) {
		if sel, ok := msg.(chatlist.ChatSelectedMsg); ok {
			return sel.ChatId, true
		}
	}
	return 0, false
}

// TestChatListOpensWhatTheCursorIsOn is decision I-2. The cursor was
// decoupled from the open chat so that j would not load a history per press,
// which stands; what did not stand was l and i acting on the OPEN chat while
// the cursor sat elsewhere, so that jjjl landed in the wrong conversation.
func TestChatListOpensWhatTheCursorIsOn(t *testing.T) {
	// Move the cursor two rows down without opening anything, the way j
	// does, then leave rightward.
	move := func(t *testing.T, m Model) (Model, int64) {
		t.Helper()
		m = update(t, m, "j")
		m = update(t, m, "j")
		cursor := m.chatList.CursorChatId()
		if cursor == 0 {
			t.Fatal("the cursor went nowhere")
		}
		if cursor == m.chatView.ChatId() {
			t.Fatal("precondition: the cursor is still on the open chat")
		}
		return m, cursor
	}

	t.Run("l opens it and focuses the chat view", func(t *testing.T) {
		m, cursor := move(t, seededChatList(t, PanelChatList))
		m = update(t, m, "l")
		if m.chatView.ChatId() != cursor {
			t.Errorf("the chat view shows %d, want the cursored %d",
				m.chatView.ChatId(), cursor)
		}
		if m.focus != PanelChatView {
			t.Errorf("focus = %v, want the chat view", m.focus)
		}
	})

	t.Run("enter means the same thing", func(t *testing.T) {
		m, cursor := move(t, seededChatList(t, PanelChatList))
		// Enter goes through the list widget, which asks for the chat by
		// message rather than opening it inline — so the command has to be
		// run for the open to happen, as the runtime would.
		m, cmd := updateCmd(t, m, "\r")
		opened, ok := selectedChat(cmd)
		if !ok {
			t.Fatal("enter opened no chat")
		}
		if opened != cursor {
			t.Errorf("enter opened %d, want the cursored %d", opened, cursor)
		}
		m = send(t, m, chatlist.ChatSelectedMsg{ChatId: opened})
		if m.chatView.ChatId() != cursor {
			t.Errorf("the chat view shows %d, want the cursored %d",
				m.chatView.ChatId(), cursor)
		}
	})

	t.Run("i opens it and points the composer at it", func(t *testing.T) {
		m, cursor := move(t, seededChatList(t, PanelChatList))
		m = update(t, m, "i")
		if m.composer.ChatId() != cursor {
			t.Errorf("the composer is pointed at %d, want the cursored %d",
				m.composer.ChatId(), cursor)
		}
		if m.focus != PanelComposer {
			t.Errorf("focus = %v, want the composer", m.focus)
		}
	})

	t.Run("l on the open chat reloads nothing", func(t *testing.T) {
		m := seededChatList(t, PanelChatList)
		m = update(t, m, "l") // opens the cursored chat
		m.setFocus(PanelChatList)
		open := m.chatView.ChatId()

		next, cmd := updateCmd(t, m, "l")
		if _, ok := selectedChat(cmd); ok {
			t.Error("l on the already-open chat asked for it again")
		}
		if next.chatView.ChatId() != open {
			t.Error("l on the already-open chat changed it")
		}
		if next.focus != PanelChatView {
			t.Errorf("focus = %v, want the chat view", next.focus)
		}
	})
}

// TestAClickInTheThreadMovesTheCursor is decision I-11: mouse users had no
// way to choose a target for r, y or +, because a click focused the panel
// and left the cursor wherever the keyboard had put it.
//
// Asserted through r, which is the thing that was broken: what the reply
// targets is what "the cursor" means to a user.
func TestAClickInTheThreadMovesTheCursor(t *testing.T) {
	m := sizedMainModel(t, PanelChatList)
	m.chatView.OpenChat(testChatID, "Test Chat")
	m.composer.SetChatId(testChatID)
	for i := int64(1); i <= 12; i++ {
		m.store.Messages.Append(testChatID, &telegram.Message{
			ID: i, ChatID: testChatID, Date: 1_700_000_000,
			Content: &telegram.MessageText{Text: &telegram.FormattedText{
				Text: fmt.Sprintf("message %d", i),
			}},
		})
	}
	m.chatView.MarkLoadedForTest()

	msgs := m.store.Messages.Get(testChatID)
	newest := msgs[len(msgs)-1].ID

	// The row an older message is drawn on, read off the frame itself so
	// the test does not depend on how tall a message happens to be.
	y := -1
	for i, line := range strings.Split(m.View().Content, "\n") {
		if strings.Contains(ansi.Strip(line), "message 3") {
			y = i
			break
		}
	}
	if y < 0 {
		t.Fatal("message 3 is not on screen")
	}

	x := m.layout.ChatListWidth + 4
	out, _ := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: x, Y: y})
	got := out.(Model)

	if got.focus != PanelChatView {
		t.Fatalf("the click did not focus the chat view (focus = %v)", got.focus)
	}

	_, cmd := updateCmd(t, got, "r")
	action, ok := findMessageAction(cmd)
	if !ok {
		t.Fatal("r after the click dispatched no message action")
	}
	if action.MessageId == 0 {
		t.Fatal("r after the click targeted no message")
	}
	if action.MessageId == newest {
		t.Errorf("r still targets the newest message (%d) — the click did "+
			"not move the cursor", newest)
	}
	if action.MessageId != 3 {
		t.Errorf("r targets message %d, want the one that was clicked (3)",
			action.MessageId)
	}
}

// TestAClickOnTheHeaderMovesNothing: the header and the day dividers are not
// messages, and moving the cursor to the nearest one would be a guess the
// reader did not make.
func TestAClickOnTheHeaderMovesNothing(t *testing.T) {
	m := sizedMainModel(t, PanelChatList)
	m.chatView.OpenChat(testChatID, "Test Chat")
	for i := int64(1); i <= 12; i++ {
		m.store.Messages.Append(testChatID, &telegram.Message{
			ID: i, ChatID: testChatID, Date: 1_700_000_000,
			Content: &telegram.MessageText{Text: &telegram.FormattedText{
				Text: fmt.Sprintf("message %d", i),
			}},
		})
	}
	m.chatView.MarkLoadedForTest()

	// Move the cursor off the tail first, so a click that reset it would
	// show up.
	m = update(t, m, "l")
	m = update(t, m, "k")
	m = update(t, m, "k")
	_, cmd := updateCmd(t, m, "r")
	before, ok := findMessageAction(cmd)
	if !ok {
		t.Fatal("precondition: r targeted nothing")
	}

	// Row 0 of the chat view column is its header.
	x := m.layout.ChatListWidth + 4
	y := 0
	if m.layout.TopBar {
		y = 1
	}
	out, _ := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: x, Y: y})

	_, cmd = updateCmd(t, out.(Model), "r")
	after, ok := findMessageAction(cmd)
	if !ok {
		t.Fatal("r targeted nothing after the header click")
	}
	if after.MessageId != before.MessageId {
		t.Errorf("a click on the header moved the cursor from %d to %d",
			before.MessageId, after.MessageId)
	}
}

// TestTheShippedConfigResolvesToTheShippedKeymap drives resolveKeys with the
// config the app actually runs on — every field filled in, which is what
// config.Load hands New — rather than the zero struct the tests above use.
//
// That difference hid a regression: a zero KeyConfig takes every default
// through the fallback path, verbatim and unnormalized, so nothing exercised
// what happens to a value that IS set. With a config file present — and
// every real config has one — next_chat = "J" went through NormalizeKey and
// came back "j", and since app-level dispatch runs before the focused panel,
// plain j switched chats instead of moving the chat list's cursor.
func TestTheShippedConfigResolvesToTheShippedKeymap(t *testing.T) {
	shipped := config.KeyConfig{
		Quit: "ctrl+q", QuitBrowsing: "q",
		Search: "/", GlobalSearch: "ctrl+g",
		Contacts: "c", Compose: "i", Help: "?",
		NextChat: "J", PrevChat: "K", NextUnread: "u",
		NextFolder: "]", PrevFolder: "[",
		Reply: "r", EditMessage: "e", DeleteMessage: "d", MarkRead: "m",
	}
	k := resolveKeys(shipped)

	for name, pair := range map[string][2]string{
		"quit":          {k.quit, "ctrl+q"},
		"quitBrowsing":  {k.quitBrowsing, "q"},
		"search":        {k.search, "/"},
		"globalSearch":  {k.globalSearch, "ctrl+g"},
		"contacts":      {k.contacts, "c"},
		"compose":       {k.compose, "i"},
		"help":          {k.help, "?"},
		"nextChat":      {k.nextChat, "J"},
		"prevChat":      {k.prevChat, "K"},
		"nextUnread":    {k.nextUnread, "u"},
		"nextFolder":    {k.nextFolder, "]"},
		"prevFolder":    {k.prevFolder, "["},
		"reply":         {k.reply, "r"},
		"editMessage":   {k.editMessage, "e"},
		"deleteMessage": {k.deleteMessage, "d"},
		"markRead":      {k.markRead, "m"},
	} {
		if got, want := pair[0], pair[1]; got != want {
			t.Errorf("%s resolved to %q, want the shipped %q", name, got, want)
		}
	}

	// And the two that matter most, end to end: J opens a chat, j does not.
	m := seededChatList(t, PanelChatList)
	m.keys = k
	first := m.chatList.CursorChatId()

	moved, cmd := updateCmd(t, m, "J")
	if _, opened := selectedChat(cmd); !opened {
		t.Error("J did not open a chat")
	}
	if moved.chatList.CursorChatId() == first {
		t.Error("J did not move the cursor")
	}

	cursorOnly, cmd := updateCmd(t, m, "j")
	if _, opened := selectedChat(cmd); opened {
		t.Error("plain j opened a chat — it must only move the cursor")
	}
	if cursorOnly.chatList.CursorChatId() == first {
		t.Error("j did not move the chat list's cursor")
	}
}

// --- decision I-12: the fourth badge -------------------------------------

// TestViComposerShowsItsOwnBadge is I-12 end to end. The composer's command
// state shared NORMAL with the browsing panels while sharing none of their
// keys: q, r, y, e and ? are all inert there, and i and h/l mean something
// else. A badge whose job is "what does the next key do" cannot honestly say
// NORMAL for two keymaps that agree on nothing.
func TestViComposerShowsItsOwnBadge(t *testing.T) {
	m := openChatModel(t, PanelComposer)
	m.width, m.height = 100, 40
	m.updateLayout()
	m.composer.SetEditingMode(composer.ModeVi)
	m.refreshChrome()

	if got := ansi.Strip(m.composer.View()); !strings.Contains(got, "INSERT") {
		t.Fatalf("a vi composer does not start in INSERT:\n%s", got)
	}

	m = update(t, m, "\x1b")
	m.refreshChrome()
	if got := m.Mode(); got != ModeVi {
		t.Fatalf("Mode() = %v after esc, want VI", got)
	}
	view := ansi.Strip(m.composer.View())
	if !strings.Contains(view, "VI") {
		t.Errorf("the badge does not read VI:\n%s", view)
	}
	if strings.Contains(view, "NORMAL") {
		t.Errorf("the badge still reads NORMAL in vi's command state:\n%s", view)
	}

	// `:` opens the palette from VI — vim's own muscle memory (I-12).
	if got := update(t, m, ":"); !got.palette.IsVisible() {
		t.Error("`:` did not open the palette from a vi composer")
	}
	// `?` does not open help there: the composer owns printables, and the
	// badge describes key routing rather than changing it.
	if got := update(t, m, "?"); got.help.IsVisible() {
		t.Error("`?` opened the help card from a vi composer")
	}
}

// TestAnEmacsComposerNeverShowsVI: the badge is the composer's own state,
// not a label the host can put on any composer.
func TestAnEmacsComposerNeverShowsVI(t *testing.T) {
	m := openChatModel(t, PanelComposer)
	m.width, m.height = 100, 40
	m.updateLayout()
	m.composer.SetEditingMode(composer.ModeEmacs)

	for _, seq := range []string{"\x1b", "\x1b"} {
		m = update(t, m, seq)
		m.refreshChrome()
		if got := ansi.Strip(m.composer.View()); strings.Contains(got, "VI") {
			t.Fatalf("an emacs composer reported VI:\n%s", got)
		}
	}
}

// TestTheBadgeColumnDoesNotMove: VI is two cells and INSERT is six, and the
// two alternate under the reader's eyes every time a vi user presses Esc.
// An unpadded badge would drag the prompt after it four cells left.
func TestTheBadgeColumnDoesNotMove(t *testing.T) {
	m := openChatModel(t, PanelComposer)
	m.width, m.height = 100, 40
	m.updateLayout()
	m.composer.SetEditingMode(composer.ModeVi)
	m.refreshChrome()

	promptColumn := func(m Model) int {
		t.Helper()
		row := ansi.Strip(strings.Split(m.composer.View(), "\n")[0])
		return strings.Index(row, "›")
	}

	insert := promptColumn(m)
	if insert < 0 {
		t.Fatal("no prompt glyph on the composer row")
	}

	m = update(t, m, "\x1b")
	m.refreshChrome()
	if got := promptColumn(m); got != insert {
		t.Errorf("the prompt moved from column %d to %d when the badge "+
			"changed to VI", insert, got)
	}
}

// TestTheDialogHintNamesTheButtonsItHas: the bar said "y/n answer" for every
// dialog, which is right for a confirm and wrong for the delete choice —
// advertising an inert y on the one surface where a wrong press is
// destructive. Both the bar and the dialog's own line are renderings of the
// one button set, so they cannot name different letters.
func TestTheDialogHintNamesTheButtonsItHas(t *testing.T) {
	cases := []struct {
		name  string
		open  func(t *testing.T, m Model) Model
		want  string
		inert string
	}{
		{
			name: "the delete choice",
			open: func(t *testing.T, m Model) Model {
				t.Helper()
				return deleteDialog(t)
			},
			want:  "n/m/e",
			inert: "y",
		},
		{
			name: "a two-button confirm",
			open: func(t *testing.T, m Model) Model {
				t.Helper()
				d := dialog.NewConfirm(m.roles, "quit", "Quit", "Discard the draft?")
				m.dialog = &d
				return m
			},
			want: "n/y",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.open(t, sizedMainModel(t, PanelChatView))
			m.width, m.height = 100, 40
			m.updateLayout()
			m.refreshChrome()

			var answer string
			for _, h := range m.hintsFor(SurfaceDialog) {
				if h.Label == "answer" {
					answer = h.Key
				}
			}
			if answer != tc.want {
				t.Errorf("the bar offers %q, want %q", answer, tc.want)
			}

			// And the dialog's own line agrees, because it is the same
			// button set rendered twice rather than two copies of it.
			if got := m.dialog.Accelerators(); got != tc.want {
				t.Errorf("the dialog's own line offers %q, want %q", got, tc.want)
			}

			bar := ansi.Strip(m.hintBar.View())
			if !strings.Contains(bar, tc.want) {
				t.Errorf("the rendered bar omits %q:\n%s", tc.want, bar)
			}
			if tc.inert != "" && strings.Contains(bar, tc.inert+"/") {
				t.Errorf("the rendered bar advertises the inert %q:\n%s", tc.inert, bar)
			}
		})
	}
}

// TestTheDialogHintIsAbsentWithNoDialog: with nothing to answer there are no
// answer letters, and hint() drops a row whose key resolved to nothing
// rather than leaving a blank in the bar.
func TestTheDialogHintIsAbsentWithNoDialog(t *testing.T) {
	m := sizedMainModel(t, PanelChatView)
	for _, h := range m.hintsFor(SurfaceDialog) {
		if h.Label == "answer" {
			t.Errorf("an answer hint (%q) with no dialog up", h.Key)
		}
	}
}

// TestEscFromABrowsingPanelLeavesTheDraftAlone completes decision I-3's
// coverage: the composer's own rungs are tested in its package and at the
// ladder above, and this is the other half — Esc pressed anywhere else must
// not reach across and take the words with it.
//
// The draft is parked in a chat the reader has navigated away from, which is
// the state where losing it would be least noticed and hardest to explain.
func TestEscFromABrowsingPanelLeavesTheDraftAlone(t *testing.T) {
	for _, panel := range []FocusPanel{PanelChatList, PanelChatView} {
		t.Run(fmt.Sprint(panel), func(t *testing.T) {
			m := openChatModel(t, PanelComposer)
			m = typeIntoComposer(t, m, "half a thought")
			m.setFocus(panel)

			for range 3 {
				m = update(t, m, "\x1b")
			}

			if got := m.composer.Draft(); got != "half a thought" {
				t.Errorf("Draft = %q after three Escapes from %v", got, panel)
			}
		})
	}
}

// TestEscUnderAnOverlayLeavesTheDraftAlone: the overlays close on Esc, and
// closing one must not be a way to lose what is in the composer behind it.
func TestEscUnderAnOverlayLeavesTheDraftAlone(t *testing.T) {
	overlays := map[string]func(t *testing.T, m Model) Model{
		"contacts": func(t *testing.T, m Model) Model { return update(t, m, "c") },
		"help":     func(t *testing.T, m Model) Model { return update(t, m, "?") },
		"palette":  func(t *testing.T, m Model) Model { return update(t, m, ":") },
		"search": func(t *testing.T, m Model) Model {
			return update(t, m, "\x07") // ctrl+g
		},
	}
	for name, open := range overlays {
		t.Run(name, func(t *testing.T) {
			m := openChatModel(t, PanelComposer)
			m = typeIntoComposer(t, m, "half a thought")
			m.setFocus(PanelChatList)
			m = open(t, m)

			m = update(t, m, "\x1b")

			if got := m.composer.Draft(); got != "half a thought" {
				t.Errorf("Draft = %q after closing the %s overlay", got, name)
			}
		})
	}
}
