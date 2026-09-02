package app

import (
	"fmt"
	"slices"
	"strings"

	"github.com/imtaqin/telegram-cli/internal/keys"
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
//	ctrl+q             quit                          ([keys.quit])
//	q                  quit, from the browsing panels only [keys.quit_browsing]
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
//	`                  toggle the context rail       (not while composing)
//	ctrl+v             paste clipboard image
//
// ## Chat list
//
//	j, k / down, up    next / previous chat
//	l                  focus the chat view           (h here is a no-op)
//	[, ] / left, right previous / next folder tab    (alt-free folder cycling)
//	1-9                jump to folder N (1 = All)
//	g, G / home, end   first / last chat
//	enter              open the selected chat
//	i, c               focus the composer
//	/                  filter this list              [keys.search]
//	q                  quit                          [keys.quit_browsing]
//	click              select a chat, or switch to a clicked folder tab
//	wheel              scroll
//
// ## Chat view
//
//	j, k / down, up    scroll down / up              [+keys.scroll_down, +keys.scroll_up]
//	g, G / home, end   top / bottom
//	ctrl+d, ctrl+u     page down / up
//	pgdown, pgup       page down / up                [+keys.page_down, +keys.page_up]
//	h                  focus the chat list           (l here is a no-op)
//	/, ctrl+f          find in this chat             [keys.search]
//	n, N               next / previous match
//	esc                close the find input, else leave the panel
//	r, e, d            reply / edit / delete message [keys.reply, keys.edit_message, keys.delete_message]
//	enter, o           open attachment
//	s                  save attachment
//	x                  reveal spoilers in the selected message
//	i, c               focus the composer
//	q                  quit                          [keys.quit_browsing]
//
// A [+keys.x] above is additive: the configured key is an extra spelling
// alongside the hardcoded one, never a replacement. The chat view's
// mnemonics (r/e/d) are the other way round — configuring one moves it.
// See chatview.Keys.
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
//	ctrl+p             expand the composer, and back
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
//	ctrl+q             quit                          ([keys.quit])
//	ctrl+v             paste a clipboard image
//	esc                only when the composer has nothing to cancel
//	tab, shift+tab     cycle panel focus            (including from the composer)
//	alt+1/2/3, f1-f3   focus a panel                 [keys.focus_chat_list, _chat_view, _composer]
//	alt+j/k, alt+h/l   chat and folder navigation    [keys.next_chat, prev_chat, next_folder, prev_folder]
//	alt+c, f4          contacts overlay              [keys.contacts, keys.contacts_alt]
//
// Every one of those is a modifier or function key that no line-editing
// keymap binds. Note "?" is absent, as is [keys.quit_browsing]: the
// composer owns printables, and a bare letter would be text here.
// No other ctrl+<letter> is claimed at app level — in particular
// ctrl+a/b/d/e/f/j/k/o/t/u/w all fall through to the composer.
//
// The [keys.x] tags on that list are load-bearing, not decoration. Those
// bindings are matched BEFORE the composer sees the event, so rebinding
// one to a printable character makes that character untypable in a
// message. [keys.quit] is the sharpest edge: it is matched before every
// focus gate in Update, so keys.quit = "x" means pressing x while writing
// quits the application instead of typing an x. Nothing rejects that
// configuration — [config.DetectKeyCollisions] only compares bindings
// against each other, not against "is this a character someone types".
// Prefer a chord or a function key for all of them.
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
// ## Deliberate departures from lazygit
//
// The layout is lazygit's — panels side by side, h/l between them, [ and ]
// for the tabs inside one, ? for the card, q for the way out. Three places
// deviate on purpose. They are listed here so they are read rather than
// discovered.
//
//   - Digits select folders, not panels. Lazygit gives 1-5 to its panels;
//     here 1-9 jump to a folder tab and alt+1/2/3 focus a panel. Folder
//     switching is the higher-frequency action in a chat client — a folder
//     is a whole different set of conversations, while the panels are two
//     halves of the one you already have open — and alt+digit for "tab N"
//     is browser muscle memory that costs nothing to honor. It is a
//     paradigm inversion, and it is the one an incoming lazygit user is
//     most likely to trip over.
//
//   - Enter in the chat view opens the attachment. A drill-in convention
//     would make enter focus or expand the item under the cursor; there is
//     nothing to drill into here, because the chat view is already the
//     bottom of the hierarchy. Opening the media is the only action the
//     key could usefully name, and o is its synonym.
//
//   - o means two different things by panel: open the attachment in the
//     chat view, open a line below in the composer's vi normal mode. That
//     is legitimate under the modal design — the composer owns its own
//     keymap and app-level dispatch claims almost nothing while it has
//     focus — but the collision is real, and someone comparing the two
//     halves of this table should find it stated rather than infer a bug.
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
)

// ScreenState identifies the current top-level screen.
type ScreenState int

const (
	ScreenAuth ScreenState = iota
	ScreenLoading
	ScreenMain
)

// Key-event matching lives in internal/keys: the same matcher has to run
// in app.go and in the panels, and when it existed as two byte-identical
// private copies only one of them could be fixed at a time. See keys.Press
// for why Keystroke() is authoritative and String() is consulted only for
// unmodified keys.

// quitKeys renders the quit row. ctrl+q is hardcoded in Update and always
// works; a configured key joins it unless it is the same one, which it is by
// default.
func quitKeys(configured string) string {
	shown := []string{"ctrl+q"}
	if configured != "" && configured != "ctrl+q" {
		shown = append(shown, configured)
	}
	return strings.Join(shown, " / ")
}

// composerHelpSection describes the composer using the line-editing keymap
// that is actually active, rather than listing both and letting the reader
// guess. The two keymaps disagree about what esc does, which is exactly the
// kind of thing a help card exists to answer.
//
// ctrl+d and delete are emacs-only here for the same reason. Both reach
// the textarea's DeleteChar in vi mode too, but only from INSERT state:
// handleViNormal drops anything carrying ctrl before its switch, and has
// no "delete" case, so neither survives an Esc. Advertising them to a vi
// user would name keys that stop working the moment they leave insert.
// Normal mode's answer is "x", which the vi section already lists.
//
// home and end go the other way and appear in BOTH sections, because
// handleViNormal really does bind them ("0", "home" and "$", "end") — so
// unlike 0 and $, they keep working in insert state as well. That is why
// the vi rows pair them with 0/$ and qualify only the vi-specific half:
// the row has to say which of the two keys on it survives the mode
// change, or it is advertising a difference it does not name.
func (m Model) composerHelpSection() help.Section {
	common := []help.Binding{
		{Keys: "enter", Desc: "Send"},
		{Keys: "ctrl+j / shift+enter", Desc: "Insert a newline"},
		{Keys: "ctrl+t", Desc: "Attach a file"},
		{Keys: "ctrl+v", Desc: "Paste a clipboard image"},
		{Keys: "ctrl+o", Desc: "Edit the draft in $VISUAL/$EDITOR"},
		{Keys: "ctrl+p", Desc: "Expand the composer, and back"},
	}

	if m.composer.EditingMode() == composer.ModeVi {
		return help.Section{Title: "Composer (vi editing)", Bindings: append(common,
			help.Binding{Keys: "esc", Desc: "Leave insert mode; again to cancel reply/edit, then leave"},
			help.Binding{Keys: "i / a / A", Desc: "Insert before / after cursor, at end of line"},
			help.Binding{Keys: "o / O", Desc: "Open a line below / above and insert"},
			help.Binding{Keys: "h / l / j / k", Desc: "Move by character and line (normal mode)"},
			help.Binding{Keys: "w / b", Desc: "Move by word (normal mode)"},
			help.Binding{Keys: "0 / home", Desc: "Start of line (0 only in normal mode)"},
			help.Binding{Keys: "$ / end", Desc: "End of line ($ only in normal mode)"},
			help.Binding{Keys: "x / D / dd", Desc: "Delete character, to end of line, whole line"},
		)}
	}

	return help.Section{Title: "Composer (emacs editing)", Bindings: append(common,
		help.Binding{Keys: "esc", Desc: "Cancel reply/edit/attachment, then leave"},
		help.Binding{Keys: "ctrl+a / home", Desc: "Start of line"},
		help.Binding{Keys: "ctrl+e / end", Desc: "End of line"},
		help.Binding{Keys: "ctrl+b / ctrl+f", Desc: "Back / forward one character"},
		help.Binding{Keys: "ctrl+u / ctrl+k", Desc: "Kill to start / end of line"},
		help.Binding{Keys: "ctrl+w", Desc: "Kill the previous word"},
		help.Binding{Keys: "ctrl+d / delete", Desc: "Delete the character under the cursor"},
	)}
}

// helpFooter is the hint strip along the bottom of the overlay. Built from
// the resolved bindings for the same reason the sections are: a rebound help
// key must not leave the card advertising "?" as the way out.
func (m Model) helpFooter() string {
	return fmt.Sprintf("esc / %s / q to close · j k to scroll", m.keys.help)
}

// reservedKeys is every key app-level dispatch takes before the focused
// browsing panel sees it: keys.AppFixed, plus whatever config resolved the
// configurable app-level bindings to.
//
// It is handed to chatview (SetReservedKeys, in New) so that panel can
// refuse a configured binding it would never receive. Without it, chatview
// accepted reply = "q", advertised it on the help card, and pressing "q"
// quit the application — the panel had no way to know app.go had claimed
// the key first.
//
// Passing the resolved values rather than the raw config is what makes it
// correct under rebinding: a user who moves quit_browsing to f9 frees "q"
// for the chat view, and this list says so.
//
// TestAppFixedMatchesDispatcher parses app.go and fails if a Matches call
// names a key this does not cover, so the list cannot fall behind the
// dispatcher the way the old (nonexistent) one did.
func (m Model) reservedKeys() []string {
	k := m.keys
	return keys.AppReserved(
		k.quit, k.quitBrowsing, k.help,
		k.search, k.globalSearch,
		k.contacts, k.contactsAlt,
		k.focusChatList, k.focusChatView, k.focusComposer,
		k.nextChat, k.prevChat,
		k.nextFolder, k.prevFolder,
	)
}

// unboundKey is what the help card shows where a binding should be but
// there is none — a mnemonic whose key a configuration handed to something
// else, leaving the action with no way to reach it (see
// chatview.ActiveKeys). It is spelled as a word rather than left blank
// because an empty cell in a key column reads as a rendering fault, and
// the reader's next move is to file a bug about the card instead of fixing
// the config that caused it.
//
// It is deliberately not a plausible key: nothing a terminal can send
// looks like this, so it cannot be mistaken for something to press. The
// documentation drift tests skip it by name for the same reason.
const unboundKey = "(unbound)"

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
//
// The chat view's rows are the one place the resolved value is read back
// out of the component rather than out of resolvedKeys: chatview resolves
// what it is handed against its own fixed keys and drops a binding that
// would shadow one (see chatview.SetKeys), so ActiveKeys is the only
// spelling that is guaranteed to be the one handleKey matches. Reading
// resolvedKeys here instead would advertise a colliding keys.reply that
// the panel silently refused.
func (m Model) helpSections() []help.Section {
	k := m.keys
	// Post-resolution, so a configured binding the chat view refused is
	// not advertised as if it had been accepted. The motion fields carry
	// only the extra accepted spelling ("" when there is none); the
	// built-in j/k, arrows and pgup/pgdown are always live and are named
	// by alsoBound below.
	cv := m.chatView.ActiveKeys()
	// A mnemonic can come back empty: a configuration that points reply at
	// edit's letter leaves edit with nothing, and chatview reports that
	// rather than pretending. Say so on the card — the row is the only
	// place the user finds out the action has become unreachable.
	bound := func(key string) string {
		if key == "" {
			return unboundKey
		}
		return key
	}
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
	// alsoBound renders a motion row: the spellings chatview hardcodes,
	// plus any configured extra that is not already one of them. Motions
	// there are additive — a configured scroll_up is an ADDITIONAL way to
	// scroll up, never a replacement for k or the arrow — so a row that
	// showed only the configured key would be advertising the removal of
	// keys that still work. See chatview.Keys's doc comment.
	alsoBound := func(fixed string, extras ...string) string {
		out := fixed
		for _, e := range extras {
			if e == "" {
				continue
			}
			if slices.Contains(strings.Split(out, " / "), e) {
				continue
			}
			out += " / " + e
		}
		return out
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
			{Keys: ":", Desc: "Command palette (not while composing)"},
			{Keys: "`", Desc: "Toggle the context rail (not while composing)"},
			{Keys: "ctrl+v", Desc: "Paste a clipboard image"},
		}},
		// The palette's own keys. `:` itself is a Global binding (it is
		// the opener, like ctrl+g for search); these are what it accepts
		// once it is up. Movement is deliberately arrows-only — every
		// printable key has to reach the query, or ":keymap" and
		// ":mark-read" could not be typed.
		{Title: "Command palette", Bindings: []help.Binding{
			{Keys: "up / down", Desc: "Move the selection"},
			{Keys: "ctrl+p / ctrl+n", Desc: "Move the selection"},
			{Keys: "tab", Desc: "Complete the highlighted command"},
			{Keys: "enter", Desc: "Run the command"},
			{Keys: "esc", Desc: "Cancel without running"},
			{Keys: "ctrl+u", Desc: "Clear the query"},
		}},
		{Title: "Chat list", Bindings: []help.Binding{
			{Keys: "j / k", Desc: "Next / previous chat"},
			{Keys: "l", Desc: "Focus the chat view"},
			{Keys: "[ / ]", Desc: "Previous / next folder tab"},
			{Keys: "left / right", Desc: "Previous / next folder tab"},
			{Keys: "1-9", Desc: "Jump to folder N (1 = All)"},
			// Split in two rather than "g / G / home / end": the Keys
			// column pairs positionally with the Desc, and a four-key row
			// against a two-word description leaves the reader guessing
			// which end "home" is. Both spellings are the list widget's
			// (internal/ui/widgets/list.go), not chatlist's own.
			{Keys: "g / home", Desc: "First chat"},
			{Keys: "G / end", Desc: "Last chat"},
			{Keys: "enter", Desc: "Open the selected chat"},
			{Keys: "i", Desc: "Compose a message"},
			{Keys: k.search, Desc: "Filter this chat list"},
			{Keys: k.quitBrowsing, Desc: "Quit — asks first if a message is half-written"},
			{Keys: "click", Desc: "Select a chat, or switch folder tab"},
		}},
		{Title: "Chat view", Bindings: []help.Binding{
			{Keys: alsoBound("j / k", cv.ScrollDown, cv.ScrollUp), Desc: "Scroll down / up"},
			{Keys: "g / home", Desc: "Top"},
			{Keys: "G / end", Desc: "Bottom"},
			{Keys: "ctrl+d / ctrl+u", Desc: "Page down / up"},
			{Keys: alsoBound("pgdown / pgup", cv.PageDown, cv.PageUp),
				Desc: "Page down / up, keeping a line of context"},
			{Keys: "h", Desc: "Focus the chat list"},
			{Keys: or(k.search, "ctrl+f"), Desc: "Find in this chat"},
			{Keys: "n / N", Desc: "Next / previous match"},
			{Keys: "} / {", Desc: "Next / previous message (j/k scroll lines)"},
			{Keys: "1-9", Desc: "Count prefix — 9{ moves nine messages back"},
			{Keys: strings.Join([]string{
				bound(cv.Reply), bound(cv.Edit), bound(cv.Delete),
			}, " / "), Desc: "Reply / edit / delete message"},
			{Keys: "enter", Desc: "Open attachment — a photo opens in the pane"},
			{Keys: "o", Desc: "Open attachment in the system viewer"},
			{Keys: "s", Desc: "Save attachment"},
			{Keys: "space", Desc: "Play the selected voice note"},
			{Keys: "y", Desc: "Copy the selected message's text"},
			{Keys: "+", Desc: "React — pick one, or press it again to take it off"},
			{Keys: "p", Desc: "Pin or unpin the selected message"},
			{Keys: "M", Desc: "Mark this chat read without moving"},
			{Keys: "x", Desc: "Reveal spoilers in the selected message"},
			{Keys: "i", Desc: "Compose a message"},
			{Keys: k.quitBrowsing, Desc: "Quit — asks first if a message is half-written"},
		}},
		m.composerHelpSection(),
		{Title: "Overlays", Bindings: []help.Binding{
			{Keys: "esc", Desc: "Close"},
			{Keys: "enter", Desc: "Accept the selection"},
			{Keys: "j / k", Desc: "Move within a list (dialogs use the arrows)"},
		}},
	}
}
