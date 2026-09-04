package keys

import "sort"

// AppFixed lists every key internal/app's dispatcher claims with a
// HARDCODED spelling while one of the browsing panels (chat list, chat
// view) has focus. App-level dispatch runs before the focused panel sees
// the event, so a component that binds one of these never fires: the key
// is gone by the time it gets there.
//
// This is the list that did not exist, and whose absence is why chatview
// could accept reply = "q" and quit the app when the user pressed it. It
// is deliberately a plain list rather than anything clever, because it has
// to be readable by the person deciding whether their rebind is safe — but
// it is not maintained by hand: TestAppFixedMatchesDispatcher in
// internal/app parses app.go's own source and fails when a Matches call
// names a key that is not here.
//
// Not included, on purpose:
//
//   - "n" and "N". app.go tests for them only to BREAK out of its own
//     dispatch and hand them to chatview's search-hit cycling. Testing a
//     key is not claiming it.
//   - Everything configurable. Those depend on config.toml and arrive
//     through AppReserved's arguments instead.
//   - "q" on its own. It closes the help overlay, but only while that
//     overlay is up — and while it is up the browsing panels are not
//     receiving keys at all. From a browsing panel "q" is claimed only
//     through keys.quit_browsing, which is configurable.
var AppFixed = []string{
	// Quit, matched before every other binding and before the focus gates.
	// One chord: ctrl+c was a third spelling of an action that already had
	// two, and is the chord a terminal user presses to abandon a command
	// rather than to close an application.
	"ctrl+q",
	// The focus ladder and the panel cycle. alt+1/2/3 and f1-f3 were here
	// and are gone (decision I-1): they only reached the app on a terminal
	// configured to report Option as a modifier, the failure was silent,
	// and h, l, i, Esc and Tab cover panel focus without them.
	"esc", "tab", "shift+tab",
	// Lazygit-style movement between the two browsing panels.
	"h", "l",
	// Clipboard paste, from whichever panel has focus.
	"ctrl+v",
	// The command palette. Claimed only from NORMAL and VI — a focused
	// emacs composer types a colon as text — but from a browsing panel it
	// is always the app's, so a component that bound it would never fire.
	":",
	// The context rail toggle. Same rule as the colon: claimed only from
	// NORMAL, so a focused composer types a backtick as text.
	"`",
}

// AppReserved returns the complete set of keys internal/app claims from a
// focused browsing panel: AppFixed plus whatever the user's config
// resolved the configurable app-level bindings to.
//
// Callers pass the resolved values — already defaulted and run through
// config.NormalizeKey — because this package cannot import internal/config
// (config imports this one) and must not carry a second copy of the
// defaults. Empty arguments are dropped, so an unset field reserves
// nothing. The result is sorted and deduplicated, so it is stable enough
// to compare in a test failure message.
func AppReserved(configured ...string) []string {
	seen := map[string]bool{}
	for _, k := range AppFixed {
		seen[k] = true
	}
	for _, k := range configured {
		if k != "" {
			seen[k] = true
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
