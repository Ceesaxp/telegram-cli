package app

import tea "charm.land/bubbletea/v2"

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
// ## Global — any panel
//
//	ctrl+c, ctrl+q     quit                          [keys.quit]
//	tab, shift+tab     cycle panel focus
//	alt+1, f1          focus chat list               [keys.focus_chat_list]
//	alt+2, f2          focus chat view               [keys.focus_chat_view]
//	alt+3, f3          focus composer                [keys.focus_composer]
//	esc                close overlay, else step back
//	alt+j, alt+k       next / previous chat          [keys.next_chat, keys.prev_chat]
//	alt+l, alt+h       next / previous folder        [keys.next_folder, keys.prev_folder]
//	alt+c, f4          toggle contacts overlay       [keys.contacts, keys.contacts_alt]
//	ctrl+g             search all chats              [keys.global_search]
//	ctrl+v             paste clipboard image
//
// ## Chat list
//
//	j, k / down, up    next / previous chat
//	h, l / left, right previous / next folder tab    (alt-free folder cycling)
//	1-9                jump to folder N (1 = All)
//	g, G / home, end   first / last chat
//	enter              open the selected chat
//	/                  search all chats              [keys.search]
//	click              select a chat, or switch to a clicked folder tab
//	wheel              scroll
//
// ## Chat view
//
//	j, k / down, up    scroll down / up
//	g, G / home, end   top / bottom
//	ctrl+d, ctrl+u     half page down / up
//	pgdown, pgup       page down / up
//	/, ctrl+f          find in this chat             ("/" is forwarded as ctrl+f)
//	n, N               next / previous match
//	esc                close the find input, else leave the panel
//	r, e, d            reply / edit / delete message
//	enter, o           open attachment
//	s                  save attachment
//	i                  focus the composer
//	any other printable focus the composer and start typing (quick-type)
//
// ## Composer
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
//	tab, shift+tab     cycle panel focus
//	alt+1/2/3, f1-f3   focus a panel
//	alt+j/k, alt+h/l   chat and folder navigation
//	alt+c, f4          contacts overlay
//
// Every one of those is a modifier or function key that no line-editing
// keymap binds. No other ctrl+<letter> is claimed at app level — in
// particular ctrl+a/b/d/e/f/j/k/o/t/u/w all fall through to the composer.
//
// ## Overlays — search, contacts, dialogs
//
//	esc                close
//	enter              accept the selection
//	j, k / down, up    move
//
// If the Telegram client dies (see Model.fatalError), the UI is replaced by
// an error panel and every binding below except quit becomes inert — there is
// nothing left to act on.
//
// Quick-type: from the chat list and chat view, an unmodified printable key
// jumps to the composer and starts a message, so any key claimed above is a
// key that can no longer start a message from that panel. That is why app
// bindings prefer a modifier, and why the vi motions above are excluded from
// quick-type per panel (see quickTypeTarget).

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
