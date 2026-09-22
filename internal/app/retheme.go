package app

import (
	"github.com/Ceesaxp/telegram-cli/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
)

// applyRoles makes roles the palette everything is drawn in, and senderRamp
// the colours people's names are hashed into.
//
// The one place that happens: New calls it once the components exist, and a
// theme switched under a running app calls it again. Two paths would be two
// lists of components, and the one a switch walked would be the one missing
// whatever was added last. TestEveryComponentFollowsARetheme is what notices
// if this one is.
//
// Every component re-derives what it built from the old palette — widget
// styles, bound row renderers, the thread's cache of drawn lines — inside
// its own SetRoles, so nothing here needs to know which of them do.
func (m *Model) applyRoles(roles theme.Roles, senderRamp []lipgloss.Color) {
	// The frame's column surfaces, the overlay surround and the fatal-error
	// panel are drawn from this copy, and so is every dialog opened from
	// here on.
	m.roles = roles

	m.auth.SetRoles(roles)
	m.chatList.SetRoles(roles)
	m.chatView.SetRoles(roles)
	m.composer.SetRoles(roles)
	m.contacts.SetRoles(roles)
	m.search.SetRoles(roles)
	m.help.SetRoles(roles)
	// The command palette is missing here only until its own SetRoles
	// lands; its case in TestEveryComponentFollowsARetheme is skipped until
	// then.
	m.attach.SetRoles(roles)
	m.reactions.SetRoles(roles)
	m.forward.SetRoles(roles)
	m.topBar.SetRoles(roles)
	m.hintBar.SetRoles(roles)
	m.rail.SetRoles(roles)
	m.mediaView.SetRoles(roles)
	// A dialog is built when it opens, from the palette of that moment, so
	// one already open has to be told.
	if m.dialog != nil {
		m.dialog.SetRoles(roles)
	}

	// The ramp is the theme's, like the palette: the thread and the rail
	// name the same people and have to agree on their colours.
	m.chatView.SetSenderRamp(senderRamp)
	m.rail.SetSenderRamp(senderRamp)
}
