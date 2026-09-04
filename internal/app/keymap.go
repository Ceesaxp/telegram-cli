package app

import (
	"fmt"
	"strings"

	"github.com/Ceesaxp/telegram-cli/internal/keys"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/composer"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/help"
)

// The keymap lives in two places, and neither of them is here.
//
//   - docs/interaction-model.md is the authority on what a key means and
//     why. It is where a decision about the keyboard gets made.
//   - docs/keys.md is the table of which key, checked against helpSections
//     at the bottom of this file by TestKeymapDocMatchesHelpSections.
//
// There used to be a third copy in this comment. It was the one nothing
// checked, and by the time it was deleted it was already wrong — it still
// listed "c" for compose after "c" had been removed (decision I-15).
//
// If the Telegram client dies (see Model.fatalError), the UI is replaced by
// an error panel and every binding becomes inert except quit — there is
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
	// q is not on it any more (decision I-8): it closed the card and then
	// quit the application, so "?qq" was an exit nobody meant to type.
	return fmt.Sprintf("esc / %s to close · j k to scroll", m.keys.help)
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
		k.contacts, k.compose,
		k.nextChat, k.prevChat, k.nextUnread,
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
	// not advertised as if it had been accepted.
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
	// pair joins two bindings that are one row on the card — next and
	// previous of the same thing — skipping the half a configuration left
	// unbound.
	pair := func(a, b string) string {
		if a == "" {
			return bound(b)
		}
		if b == "" {
			return bound(a)
		}
		return a + " / " + b
	}

	return []help.Section{
		{Title: "Global", Bindings: []help.Binding{
			{Keys: quitKeys(k.quit), Desc: "Quit"},
			{Keys: k.help, Desc: "Toggle this help"},
			{Keys: "tab / shift+tab", Desc: "Cycle panel focus"},
			{Keys: "esc", Desc: "Close overlay, else step back"},
			{Keys: pair(k.nextChat, k.prevChat), Desc: "Open the next / previous chat"},
			{Keys: bound(k.nextUnread), Desc: "Open the next chat with unread messages"},
			{Keys: pair(k.prevFolder, k.nextFolder), Desc: "Previous / next folder"},
			{Keys: bound(k.contacts), Desc: "Contacts overlay"},
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
			// One spelling per action: ctrl+p/ctrl+n were taken off the
			// palette as a second way to do what the arrows already did,
			// and this card went on advertising them — a key that has to
			// be discovered by pressing it and finding it inert.
			{Keys: "up / down", Desc: "Move the selection"},
			{Keys: "tab", Desc: "Complete the highlighted command"},
			{Keys: "enter", Desc: "Run the command"},
			{Keys: "esc", Desc: "Cancel without running"},
			{Keys: "ctrl+u", Desc: "Clear the query"},
		}},
		// The attach picker is the palette's twin and its card says so: the
		// arrows and nothing else, for the same reason — a path may contain
		// any letter, so every printable has to reach it.
		{Title: "Attach picker", Bindings: []help.Binding{
			{Keys: "up / down", Desc: "Move the selection"},
			{Keys: "tab", Desc: "Complete the path to the highlighted entry"},
			{Keys: "enter", Desc: "Enter a directory, or attach a file"},
			{Keys: "backspace", Desc: "Delete a character; up a directory when there is none"},
			{Keys: "left", Desc: "Up one directory"},
			{Keys: "ctrl+t", Desc: "Send an image as a photo or as a document"},
			{Keys: "ctrl+u", Desc: "Clear the path"},
			{Keys: "esc", Desc: "Cancel without attaching"},
		}},
		{Title: "Chat list", Bindings: []help.Binding{
			{Keys: "j / k", Desc: "Move the cursor — opens nothing"},
			{Keys: "l", Desc: "Open the cursored chat and focus the chat view"},
			{Keys: "left / right", Desc: "Previous / next folder tab"},
			{Keys: "1-9", Desc: "Jump to folder N (1 = All)"},
			// Split in two rather than "g / G / home / end": the Keys
			// column pairs positionally with the Desc, and a four-key row
			// against a two-word description leaves the reader guessing
			// which end "home" is. Both spellings are the list widget's
			// (internal/ui/widgets/list.go), not chatlist's own.
			{Keys: "g / home", Desc: "First chat"},
			{Keys: "G / end", Desc: "Last chat"},
			{Keys: "enter", Desc: "Open the cursored chat — the same as l"},
			{Keys: bound(k.compose), Desc: "Open the cursored chat and compose"},
			{Keys: k.search, Desc: "Filter this chat list"},
			{Keys: k.quitBrowsing, Desc: "Quit — asks first if a message is half-written"},
			{Keys: "click", Desc: "Select a chat, or switch folder tab"},
		}},
		{Title: "Chat view", Bindings: []help.Binding{
			{Keys: "j / k", Desc: "Cursor to the next / previous message"},
			{Keys: "ctrl+e / ctrl+y", Desc: "Scroll the buffer one line down / up"},
			{Keys: "g / home", Desc: "Top"},
			{Keys: "G / end", Desc: "Bottom"},
			{Keys: "ctrl+d / ctrl+u", Desc: "Page down / up"},
			{Keys: "pgdown / pgup", Desc: "Page down / up, keeping a line of context"},
			{Keys: "h", Desc: "Focus the chat list"},
			{Keys: pair(k.search, "ctrl+f"), Desc: "Find in this chat"},
			{Keys: "n / N", Desc: "Next / previous match"},
			{Keys: "1-9", Desc: "Count prefix — 9k moves nine messages back"},
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
			{Keys: "t", Desc: "Open the discussion under a channel post"},
			{Keys: bound(cv.MarkRead), Desc: "Mark this chat read without moving"},
			{Keys: "x", Desc: "Reveal spoilers in the selected message"},
			{Keys: bound(k.compose), Desc: "Compose a message"},
			{Keys: k.quitBrowsing, Desc: "Quit — asks first if a message is half-written"},
		}},
		m.composerHelpSection(),
		{Title: "Overlays", Bindings: []help.Binding{
			{Keys: "esc", Desc: "Close — q closes no overlay"},
			{Keys: "enter", Desc: "Accept the selection"},
			{Keys: "j / k", Desc: "Move within a list (dialogs use the arrows)"},
			// The contacts overlay's own two. It draws a whole column and
			// narrows like the chat list it borrows the column from, which
			// is worth a row: a contact list long enough to need a filter
			// is the one you opened to find a name in.
			{Keys: bound(k.search), Desc: "Filter the contacts list"},
			{Keys: bound(k.contacts), Desc: "Close the contacts overlay"},
		}},
	}
}
