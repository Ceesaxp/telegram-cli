package app

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/imtaqin/telegram-cli/internal/ui/components/composer"
	"github.com/imtaqin/telegram-cli/internal/ui/components/help"
)

// # Keymap
//
// The app follows vi convention: motions are h/j/k/l, g/G jump to the ends,
// ctrl+u/ctrl+d move a half page, "/" searches the buffer in front of you and
// n/N step through the matches. Bindings marked [keys.x] are configurable in
// config.toml's [keys] table (see [config.KeyConfig]); the rest are owned by
// the component that implements them.
//
// Every alt+ binding has an alt-free alternative, because a terminal that does
// not report Option/Alt as a modifier makes alt bindings unreachable and
// undetectable — see the macOS notes on [config.KeyConfig].
//
// Typing is always entered deliberately: i, c, Tab, a focus key, or a click
// on the composer. Nothing is forwarded there implicitly, so a binding here
// never costs the ability to type that character.
//
// This table is prose for readers of the source; helpSections at the bottom
// of this file is the same map as data, and is what the user sees in the
// help overlay.
//
// ## Global — any panel
//
//	ctrl+c, ctrl+q     quit                          (plus [keys.quit])
//	tab, shift+tab     cycle panel focus            (including from the composer)
//	alt+1, f1          focus chat list               [keys.focus_chat_list]
//	alt+2, f2          focus chat view               [keys.focus_chat_view]
//	alt+3, f3          focus composer                [keys.focus_composer]
//	esc                close overlay, else step back
//	alt+j, alt+k       next / previous chat          [keys.next_chat, keys.prev_chat]
//	alt+l, alt+h       next / previous folder        [keys.next_folder, keys.prev_folder]
//	alt+c, f4          toggle contacts overlay       [keys.contacts, keys.contacts_alt]
//	ctrl+g             search all chats              [keys.global_search], not from the composer
//	?                  toggle the help overlay       [keys.help]
//	ctrl+v             paste clipboard image
//
// ## Chat list
//
//	j, k / down, up    next / previous chat
//	h, l / left, right previous / next folder tab    (alt-free folder cycling)
//	[, ]               previous / next folder tab    (lazygit spelling, chatlist)
//	1-9                jump to folder N (1 = All)
//	g, G / home, end   first / last chat
//	enter              open the selected chat
//	i, c               focus the composer
//	/                  search all chats              [keys.search]
//	click              select a chat, or switch to a clicked folder tab
//	wheel              scroll
//
// ## Chat view
//
//	j, k / down, up    scroll down / up
//	g, G / home, end   top / bottom
//	ctrl+d, ctrl+u     page down / up
//	pgdown, pgup       page down / up
//	/, ctrl+f          find in this chat
//	n, N               next / previous match
//	esc                close the find input, else leave the panel
//	r, e, d            reply / edit / delete message
//	enter, o           open attachment
//	s                  save attachment
//	i, c               focus the composer
//
// ## Composer
//
// Focus stays here after a send — a conversation is a run of messages, not
// one. Esc is how you leave.
//
//	enter              send
//	ctrl+j, shift+ent  insert a newline
//	esc                cancel reply/edit/attachment, then leave the panel
//	ctrl+t             attach a file
//	ctrl+v             paste a clipboard image
//	ctrl+o             edit the draft in $VISUAL/$EDITOR
//	ctrl+a, ctrl+e     start / end of line           (emacs mode)
//	ctrl+b, ctrl+f     back / forward one character  (emacs mode)
//	ctrl+u, ctrl+k     kill to start / end of line   (emacs mode)
//	ctrl+w, ctrl+d     kill previous word / delete   (emacs mode)
//
// The composer's line editing follows either the emacs (readline) or the vi
// keymap, selected by ui.compose_editing in config.toml — "emacs", "vi", or
// "auto" (the default), which infers it from $VISUAL/$EDITOR. See
// [config.ResolveComposeEditing].
//
// App-level dispatch deliberately claims almost nothing while the composer is
// focused, so neither keymap's chords are shadowed. The complete list of keys
// that still fire from a focused composer:
//
//	ctrl+c, ctrl+q     quit
//	ctrl+v             paste a clipboard image
//	esc                only when the composer has nothing to cancel
//	tab, shift+tab     cycle panel focus            (including from the composer)
//	alt+1/2/3, f1-f3   focus a panel
//	alt+j/k, alt+h/l   chat and folder navigation
//	alt+c, f4          contacts overlay
//
// Every one of those is a modifier or function key that no line-editing
// keymap binds. Note "?" is absent: the composer owns printables. No other ctrl+<letter> is claimed at app level — in
// particular ctrl+a/b/d/e/f/j/k/o/t/u/w all fall through to the composer.
//
// ## Overlays — search, contacts, dialogs
//
//	esc                close
//	enter              accept the selection
//	j, k / down, up    move
//
// ## Help overlay
//
//	?, esc, q          close
//	j, k / down, up    scroll
//	pgup, pgdown       page
//	g, G / home, end   top / bottom
//
// If the Telegram client dies (see Model.fatalError), the UI is replaced by
// an error panel and every binding above except quit becomes inert — there is
// nothing left to act on.

// FocusPanel identifies which UI panel has focus.
type FocusPanel int

const (
	PanelChatList FocusPanel = iota
	PanelChatView
	PanelComposer
	PanelSearch
	PanelContacts
	PanelGroupInfo
)

// ScreenState identifies the current top-level screen.
type ScreenState int

const (
	ScreenAuth ScreenState = iota
	ScreenLoading
	ScreenMain
)

// keyPress is the normalized view of a terminal key event that app-level
// binding dispatch matches against.
//
// Matching on tea.KeyPressMsg.String() alone is not sufficient. String()
// returns Key.Text whenever the terminal attached any, and only falls back to
// Keystroke() when it did not. Terminals speaking the Kitty keyboard protocol
// report alt-modified keys *with* their composed text on macOS — Option+1
// arrives as CSI 49;3;161u, i.e. Code='1', Mod=ModAlt, Text="¡" — so String()
// yields "¡" while Keystroke() yields "alt+1". Keystroke() is derived from
// Key.Code/Key.BaseCode and the modifier bits only, so it is stable across
// every encoding the decoder handles (legacy ESC-prefix, Kitty CSI-u, XTerm
// modifyOtherKeys).
//
// String() is still needed for the unmodified case, where it reports what the
// keyboard layout actually produced: shift+/ is "?" via String() but
// "shift+/" via Keystroke(), and a binding of "/" should match it.
type keyPress struct {
	// stroke is Keystroke(): "alt+1", "ctrl+v", "f1", "a", "shift+/".
	stroke string
	// text is String(): the characters the terminal reported ("?", "A", "¡"),
	// falling back to stroke for keys that produce no text.
	text string
	// modified reports whether a modifier beyond shift/caps-lock was held.
	// Such a key press must never be treated as text input.
	modified bool
}

// newKeyPress captures the two spellings of a key event once, so dispatch
// does not recompute them for every binding it tests.
func newKeyPress(msg tea.KeyPressMsg) keyPress {
	return keyPress{
		stroke:   msg.Keystroke(),
		text:     msg.String(),
		modified: msg.Mod&^(tea.ModShift|tea.ModCapsLock|tea.ModNumLock|tea.ModScrollLock) != 0,
	}
}

// matches reports whether the key press is any of the given bindings.
// Bindings are expected in config.NormalizeKey / Keystroke() form. Empty
// bindings never match, so an unset config field is inert.
//
// The Keystroke() spelling is authoritative. The String() spelling is only
// consulted for unmodified keys: allowing it for modified ones would let a
// Kitty-reported alt+/ (Text "/") fire a plain "/" binding.
func (k keyPress) matches(bindings ...string) bool {
	for _, b := range bindings {
		if b == "" {
			continue
		}
		if b == k.stroke {
			return true
		}
		if !k.modified && b == k.text {
			return true
		}
	}
	return false
}

// quitKeys renders the quit row. ctrl+c and ctrl+q are both hardcoded in
// Update and always work, so both are shown; a configured third key joins
// them unless it duplicates one.
func quitKeys(configured string) string {
	keys := []string{"ctrl+c", "ctrl+q"}
	if configured != "" && configured != "ctrl+c" && configured != "ctrl+q" {
		keys = append(keys, configured)
	}
	return strings.Join(keys, " / ")
}

// composerHelpSection describes the composer using the line-editing keymap
// that is actually active, rather than listing both and letting the reader
// guess. The two keymaps disagree about what esc does, which is exactly the
// kind of thing a help card exists to answer.
func (m Model) composerHelpSection() help.Section {
	common := []help.Binding{
		{Keys: "enter", Desc: "Send"},
		{Keys: "ctrl+j / shift+enter", Desc: "Insert a newline"},
		{Keys: "ctrl+t", Desc: "Attach a file"},
		{Keys: "ctrl+v", Desc: "Paste a clipboard image"},
		{Keys: "ctrl+o", Desc: "Edit the draft in $VISUAL/$EDITOR"},
	}

	if m.composer.EditingMode() == composer.ModeVi {
		return help.Section{Title: "Composer (vi editing)", Bindings: append(common,
			help.Binding{Keys: "esc", Desc: "Leave insert mode; again to cancel reply/edit, then leave"},
			help.Binding{Keys: "i / a / A", Desc: "Insert before / after cursor, at end of line"},
			help.Binding{Keys: "o / O", Desc: "Open a line below / above and insert"},
			help.Binding{Keys: "h / l / j / k", Desc: "Move by character and line (normal mode)"},
			help.Binding{Keys: "w / b", Desc: "Move by word (normal mode)"},
			help.Binding{Keys: "0 / $", Desc: "Start / end of line (normal mode)"},
			help.Binding{Keys: "x / D / dd", Desc: "Delete character, to end of line, whole line"},
		)}
	}

	return help.Section{Title: "Composer (emacs editing)", Bindings: append(common,
		help.Binding{Keys: "esc", Desc: "Cancel reply/edit/attachment, then leave"},
		help.Binding{Keys: "ctrl+a / ctrl+e", Desc: "Start / end of line"},
		help.Binding{Keys: "ctrl+b / ctrl+f", Desc: "Back / forward one character"},
		help.Binding{Keys: "ctrl+u / ctrl+k", Desc: "Kill to start / end of line"},
		help.Binding{Keys: "ctrl+w", Desc: "Kill the previous word"},
	)}
}

// helpFooter is the hint strip along the bottom of the overlay. Built from
// the resolved bindings for the same reason the sections are: a rebound help
// key must not leave the card advertising "?" as the way out.
func (m Model) helpFooter() string {
	return fmt.Sprintf("esc / %s / q to close · j k to scroll", m.keys.help)
}

// helpSections builds the content of the help overlay.
//
// It is the single source of truth for what the overlay shows, and the
// configurable rows are read from the same resolvedKeys the dispatcher
// matches against — so a rebound key is described correctly and a binding
// cannot drift out of sync with its documentation. Rows for keys owned by a
// component (chatlist's arrows and digits, chatview's motions, the
// composer's line editing) are static, because those packages hardcode them.
//
// The doc comment at the top of this file is the same map in prose, kept for
// readers of the source; this is the version the user sees.
func (m Model) helpSections() []help.Section {
	k := m.keys
	// or joins the app-level spelling of a binding with its alt-free
	// alternative, skipping the alternative when a user has configured both
	// to the same key.
	or := func(a, b string) string {
		if a == b || b == "" {
			return a
		}
		if a == "" {
			return b
		}
		return a + " / " + b
	}

	return []help.Section{
		{Title: "Global", Bindings: []help.Binding{
			{Keys: quitKeys(k.quit), Desc: "Quit"},
			{Keys: k.help, Desc: "Toggle this help"},
			{Keys: "tab / shift+tab", Desc: "Cycle panel focus"},
			{Keys: or("alt+1", k.focusChatList), Desc: "Focus chat list"},
			{Keys: or("alt+2", k.focusChatView), Desc: "Focus chat view"},
			{Keys: or("alt+3", k.focusComposer), Desc: "Focus composer"},
			{Keys: "esc", Desc: "Close overlay, else step back"},
			{Keys: or(k.nextChat, k.prevChat), Desc: "Next / previous chat"},
			{Keys: or(k.nextFolder, k.prevFolder), Desc: "Next / previous folder"},
			{Keys: or(k.contacts, k.contactsAlt), Desc: "Contacts overlay"},
			{Keys: k.globalSearch, Desc: "Search all chats (not while composing)"},
			{Keys: "ctrl+v", Desc: "Paste a clipboard image"},
		}},
		{Title: "Chat list", Bindings: []help.Binding{
			{Keys: "j / k", Desc: "Next / previous chat"},
			{Keys: "h / l", Desc: "Previous / next folder tab"},
			{Keys: "[ / ]", Desc: "Previous / next folder tab"},
			{Keys: "left / right", Desc: "Previous / next folder tab"},
			{Keys: "1-9", Desc: "Jump to folder N (1 = All)"},
			{Keys: "g / G", Desc: "First / last chat"},
			{Keys: "enter", Desc: "Open the selected chat"},
			{Keys: "i / c", Desc: "Compose a message"},
			{Keys: k.search, Desc: "Search all chats"},
			{Keys: "click", Desc: "Select a chat, or switch folder tab"},
		}},
		{Title: "Chat view", Bindings: []help.Binding{
			{Keys: "j / k", Desc: "Scroll down / up"},
			{Keys: "g / G", Desc: "Top / bottom"},
			{Keys: "ctrl+d / ctrl+u", Desc: "Page down / up"},
			{Keys: "pgdown / pgup", Desc: "Page down / up, keeping a line of context"},
			{Keys: or(k.search, "ctrl+f"), Desc: "Find in this chat"},
			{Keys: "n / N", Desc: "Next / previous match"},
			{Keys: "r / e / d", Desc: "Reply / edit / delete message"},
			{Keys: "enter / o", Desc: "Open attachment"},
			{Keys: "s", Desc: "Save attachment"},
			{Keys: "i / c", Desc: "Compose a message"},
		}},
		m.composerHelpSection(),
		{Title: "Overlays", Bindings: []help.Binding{
			{Keys: "esc", Desc: "Close"},
			{Keys: "enter", Desc: "Accept the selection"},
			{Keys: "j / k", Desc: "Move"},
		}},
	}
}
