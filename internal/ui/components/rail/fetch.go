package rail

import (
	tea "charm.land/bubbletea/v2"
	"github.com/Ceesaxp/telegram-cli/internal/telegram"
)

// searchResultMsg carries one filtered search back.
//
// Every result carries the chat and generation it was started for, and Update
// drops anything that no longer matches. Without that, switching chats while
// a search is in flight lands one chat's files in another chat's rail — and
// the rail is the surface most likely to have a request outstanding, because
// it starts three at once.
type searchResultMsg struct {
	gen    int
	chatID int64
	filter telegram.MediaFilter
	msgs   []*telegram.Message
	err    error
}

// membersResultMsg carries a member list back.
type membersResultMsg struct {
	gen     int
	chatID  int64
	members []*telegram.ChatMember
	// count is the chat's total member count, which the participants call
	// does not return and the remainder row needs.
	count int
	err   error
}

func (m Model) searchCmd(chatID int64, filter telegram.MediaFilter, limit int32) tea.Cmd {
	gen, tg := m.gen, m.tg
	return func() tea.Msg {
		msgs, err := tg.SearchChatMedia(chatID, filter, limit)
		return searchResultMsg{gen: gen, chatID: chatID, filter: filter, msgs: msgs, err: err}
	}
}

// membersCmd asks for a chat's members by the route its type supports.
//
// A basic group returns its members with its full info; a supergroup needs a
// separate participants call. Asking the wrong one returns an error rather
// than an empty list, which the rail would otherwise show as "none" — a group
// that says it has no members is worse than one that says it could not find
// out.
//
// It does NOT ask how many members there are. The participants call returns
// a page rather than a count, and "+192 more" computed from a page is a lie
// about a group of two hundred — but the thread header needs the same total
// and asks for it on every chat open, rail or no rail, so a second request
// here would be the same question twice on the way to the same screen. The
// count comes off the store; see memberSection.
//
// A basic group's total arrives with its members for free, so that one is
// written THROUGH to the store rather than thrown away: it is the same
// number the header wants, already paid for.
func (m Model) membersCmd(chatID int64) tea.Cmd {
	gen, tg := m.gen, m.tg
	basic := m.chatTypeOf(chatID) == telegram.ChatTypeBasicGroup
	return func() tea.Msg {
		if basic {
			info, err := tg.GetBasicGroupFullInfo(chatID)
			if err != nil {
				return membersResultMsg{gen: gen, chatID: chatID, err: err}
			}
			return membersResultMsg{
				gen: gen, chatID: chatID,
				members: info.Members, count: int(info.MemberCount),
			}
		}
		members, err := tg.GetSupergroupMembers(chatID, 0, maxMemberRows*4)
		if err != nil {
			return membersResultMsg{gen: gen, chatID: chatID, err: err}
		}
		return membersResultMsg{gen: gen, chatID: chatID, members: members}
	}
}

// Update folds results in, dropping anything stale.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case searchResultMsg:
		d := m.liveData(msg.gen, msg.chatID)
		if d == nil {
			return m, nil
		}
		state := stateReady
		if msg.err != nil {
			state = stateFailed
		}
		switch msg.filter {
		case telegram.MediaFilterPinned:
			d.pinnedState, d.pinned = state, msg.msgs
		case telegram.MediaFilterLinks:
			d.linksState, d.links = state, msg.msgs
		default:
			d.filesState, d.files = state, msg.msgs
		}

	case membersResultMsg:
		d := m.liveData(msg.gen, msg.chatID)
		if d == nil {
			return m, nil
		}
		d.membersState, d.members = stateReady, msg.members
		if msg.err != nil {
			d.membersState = stateFailed
		}
		// A count the members call happened to bring with it (a basic
		// group's full info) belongs to every surface that draws this
		// chat, not to the rail.
		if msg.count > 0 && m.store != nil {
			m.store.Chats.SetMemberCount(msg.chatID, int32(msg.count))
		}

	case telegram.NewMessageMsg:
		// Opportunistic refresh while the rail is open (decision 6): a file
		// posted into the chat you are looking at should appear in the rail
		// without a reopen. Only for the open chat, and only when the rail
		// has data there — otherwise this would be a fetch on every message
		// in every chat, which is the thing the whole policy avoids.
		if m.chatID != 0 && msg.Message != nil && msg.Message.ChatID == m.chatID {
			if cmd := m.refreshForNewMessage(msg.Message); cmd != nil {
				return m, cmd
			}
		}
	}
	return m, nil
}

// liveData returns the cache entry a result belongs to, or nil when the
// result is stale.
//
// The entry's own generation is the authority, and the only check needed:
// Invalidate replaces every entry, so a result from before it can never find
// one whose generation matches. Comparing against the model's current
// generation as well would be a second mechanism for the same rule, and only
// one of them can be the one that fires.
//
// Stale deliberately does NOT include "for a chat that is no longer open": a
// result
// that arrives after the user has switched away is still correct for the chat
// that asked, and caching it is what makes going back instant. Sections only
// ever reads the OPEN chat's entry, so a late answer cannot appear under the
// wrong heading.
func (m Model) liveData(gen int, chatID int64) *chatData {
	d := m.data[chatID]
	if d == nil || d.gen != gen {
		return nil
	}
	return d
}

// refreshForNewMessage re-asks for the one section a new message could have
// changed.
//
// One section, not all of them: a text message with a link does not change
// the file list, and re-fetching everything on every message would turn an
// open rail into a request per message.
func (m Model) refreshForNewMessage(msg *telegram.Message) tea.Cmd {
	d := m.data[m.chatID]
	if d == nil || m.tg == nil {
		return nil
	}

	switch msg.Content.(type) {
	case *telegram.MessageDocument, *telegram.MessagePhoto,
		*telegram.MessageVideo, *telegram.MessageAudio:
		if d.filesState == stateReady {
			return m.searchCmd(m.chatID, telegram.MediaFilterFiles, maxFileRows)
		}
	case *telegram.MessageText:
		if d.linksState == stateReady && linkSummary(msg) != "" {
			return m.searchCmd(m.chatID, telegram.MediaFilterLinks, maxLinkRows)
		}
	case *telegram.MessagePinMessage:
		if d.pinnedState == stateReady {
			return m.searchCmd(m.chatID, telegram.MediaFilterPinned, maxPinnedRows)
		}
	}
	return nil
}
