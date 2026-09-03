package rail

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Ceesaxp/telegram-cli/internal/render"
	"github.com/Ceesaxp/telegram-cli/internal/store"
	"github.com/Ceesaxp/telegram-cli/internal/telegram"
)

// Row caps. The rail is a glance, not a browser: past a handful of rows it
// stops answering "who is in here" and starts being a list you have to read.
const (
	maxMemberRows = 8
	maxFileRows   = 6
	maxPinnedRows = 3
	maxLinkRows   = 6
)

// sectionState is what is known about one section's data.
type sectionState int

const (
	// stateIdle: never asked for. The rail has not been opened on this chat.
	stateIdle sectionState = iota
	// stateLoading: asked, no answer yet.
	stateLoading
	// stateReady: answered, rows are current.
	stateReady
	// stateFailed: Telegram said no, or could not be reached.
	stateFailed
)

// chatData is one chat's rail data, cached per chat and load generation.
type chatData struct {
	gen int

	pinnedState sectionState
	pinned      []*telegram.Message

	filesState sectionState
	files      []*telegram.Message

	linksState sectionState
	links      []*telegram.Message

	membersState sectionState
	members      []*telegram.ChatMember
}

// SetStore wires the data sources. A rail without them still renders its
// chrome and says every section is unavailable, which is what a test model
// and a disconnected client should both see.
func (m *Model) SetStore(s *store.Store, tg *telegram.Client) {
	m.store = s
	m.tg = tg
}

// SetDataForTest installs a chat's rail sections directly, as though every
// fetch had already answered.
//
// The three sections arrive from three commands this component starts
// itself, against a client a test does not have. Without a seam a rail in a
// test says "unavailable" in every section, which is the one state the
// goldens do not draw. Same reason and same shape as
// chatlist.MarkLoadedForTest.
func (m *Model) SetDataForTest(chatID int64, pinned, files []*telegram.Message,
	members []*telegram.ChatMember, memberCount int) {
	if m.data == nil {
		m.data = map[int64]*chatData{}
	}
	m.chatID = chatID
	m.data[chatID] = &chatData{
		gen:          m.gen,
		pinnedState:  stateReady,
		pinned:       pinned,
		filesState:   stateReady,
		files:        files,
		linksState:   stateReady,
		membersState: stateReady,
		members:      members,
	}
	// The total lives in the store, where every surface that draws this
	// chat reads it — see memberSection.
	if memberCount > 0 && m.store != nil {
		m.store.Chats.SetMemberCount(chatID, int32(memberCount))
	}
}

// Open points the rail at a chat and starts fetching what that chat's
// sections need (decision 6).
//
// Nothing is fetched until this is called, and this is only called when the
// rail is opened. Opening a chat costs no rail request at all: the primary
// history paint never competes with rail work, and a user who keeps the rail
// closed never pays for it.
//
// A chat whose data is already cached for this generation is not refetched —
// toggling the rail off and on is free.
func (m *Model) Open(chatID int64) tea.Cmd {
	m.chatID = chatID
	if m.data == nil {
		m.data = map[int64]*chatData{}
	}

	d, ok := m.data[chatID]
	if ok && d.gen == m.gen {
		return nil
	}
	d = &chatData{gen: m.gen}
	m.data[chatID] = d

	if m.tg == nil {
		// No client: every section says so rather than spinning forever.
		d.pinnedState, d.filesState = stateFailed, stateFailed
		d.linksState, d.membersState = stateFailed, stateFailed
		return nil
	}

	var cmds []tea.Cmd
	for _, want := range m.sectionsFor(chatID) {
		switch want {
		case sectionPinned:
			d.pinnedState = stateLoading
			cmds = append(cmds, m.searchCmd(chatID, telegram.MediaFilterPinned, maxPinnedRows))
		case sectionFiles:
			d.filesState = stateLoading
			cmds = append(cmds, m.searchCmd(chatID, telegram.MediaFilterFiles, maxFileRows))
		case sectionLinks:
			d.linksState = stateLoading
			cmds = append(cmds, m.searchCmd(chatID, telegram.MediaFilterLinks, maxLinkRows))
		case sectionMembers:
			d.membersState = stateLoading
			cmds = append(cmds, m.membersCmd(chatID))
		}
	}
	return tea.Batch(cmds...)
}

// Close forgets which chat the rail is pointing at, without dropping the
// cache: reopening it on the same chat is instant.
func (m *Model) Close() { m.chatID = 0 }

// Invalidate drops everything and bumps the generation, so results already in
// flight are discarded when they land. Called when the account's data changes
// underneath the rail.
func (m *Model) Invalidate() {
	m.gen++
	m.data = nil
}

// sectionKind names a rail section.
type sectionKind int

const (
	sectionPinned sectionKind = iota
	sectionMembers
	sectionFiles
	sectionLinks
)

// sectionsFor is which sections a chat type gets (docs/tui-2.0.md, "Context
// rail").
//
// A DM has no members section — you and them, which the header already says —
// and gains links instead, because a two-person chat is where a link gets
// sent and then lost. Channels have members you cannot enumerate, so they get
// pinned and files.
func (m Model) sectionsFor(chatID int64) []sectionKind {
	switch m.chatTypeOf(chatID) {
	case telegram.ChatTypeBasicGroup, telegram.ChatTypeSupergroup:
		return []sectionKind{sectionPinned, sectionMembers, sectionFiles}
	case telegram.ChatTypeChannel:
		return []sectionKind{sectionPinned, sectionFiles}
	default:
		return []sectionKind{sectionFiles, sectionLinks}
	}
}

func (m Model) chatTypeOf(chatID int64) telegram.ChatType {
	if m.store == nil {
		return telegram.ChatTypePrivate
	}
	entry, ok := m.store.Chats.Get(chatID)
	if !ok || entry.Chat == nil {
		return telegram.ChatTypePrivate
	}
	return entry.Chat.Type
}

// Sections builds what View draws, from whatever is known right now.
//
// Every section is present whatever its state: a section that vanished while
// loading and reappeared when it finished would make the rail jump under the
// reader, and a section that vanished on failure would leave them thinking
// the chat has no files rather than that the request failed.
func (m Model) Sections() []Section {
	if m.chatID == 0 {
		return nil
	}
	d := m.data[m.chatID]
	if d == nil {
		d = &chatData{}
	}

	var out []Section
	for _, kind := range m.sectionsFor(m.chatID) {
		switch kind {
		case sectionPinned:
			out = append(out, m.messageSection("pinned", d.pinnedState, d.pinned,
				maxPinnedRows, RowPinned))
		case sectionFiles:
			out = append(out, m.messageSection("files", d.filesState, d.files,
				maxFileRows, RowFile))
		case sectionLinks:
			out = append(out, m.messageSection("links", d.linksState, d.links,
				maxLinkRows, RowLink))
		case sectionMembers:
			out = append(out, m.memberSection(d))
		}
	}
	return out
}

// messageSection turns a list of messages into rows of one kind.
func (m Model) messageSection(title string, state sectionState, msgs []*telegram.Message, cap int, kind RowKind) Section {
	s := Section{Title: title}

	if note, ok := stateNote(state, len(msgs)); ok {
		s.Rows = []Row{{Kind: RowNote, Text: note}}
		return s
	}

	shown := min(len(msgs), cap)
	for _, msg := range msgs[:shown] {
		s.Rows = append(s.Rows, m.messageRow(msg, kind))
	}
	// The total goes in the heading only when the rows do not add up to it.
	// "FILES · 3" above three files tells the reader something they can
	// already see; "MEMBERS · 24" above five of them does not.
	if rest := len(msgs) - shown; rest > 0 {
		s.Count = len(msgs)
		s.Rows = append(s.Rows, Row{Kind: RowMore, Text: "+" + strconv.Itoa(rest) + " more"})
	}
	return s
}

// messageRow describes one message as a rail row.
func (m Model) messageRow(msg *telegram.Message, kind RowKind) Row {
	row := Row{Kind: kind}

	switch kind {
	case RowPinned:
		// What it says, and who said it. The author is the right-hand
		// field because "who pinned this" is the part a reader scans for
		// when several are pinned.
		row.Text = messageSummary(msg)
		row.Right = render.SenderName(msg, m.store)
	case RowFile:
		row.Text, row.Right = fileSummary(msg)
		if isImageFile(msg) {
			row.Kind = RowFileImage
		}
	case RowLink:
		row.Text = linkSummary(msg)
	}
	return row
}

// isImageFile reports whether a shared file is a picture, so its row can
// carry the same mark the media card gives one.
//
// The MIME type, not the extension: it is what the sender's client
// declared, and a screenshot saved as ".dat" is still a picture.
func isImageFile(msg *telegram.Message) bool {
	switch c := msg.Content.(type) {
	case *telegram.MessagePhoto:
		return true
	case *telegram.MessageDocument:
		return c.Document != nil && strings.HasPrefix(c.Document.MimeType, "image/")
	}
	return false
}

// memberSection lists people, online first.
func (m Model) memberSection(d *chatData) Section {
	s := Section{Title: "members"}

	if note, ok := stateNote(d.membersState, len(d.members)); ok {
		s.Rows = []Row{{Kind: RowNote, Text: note}}
		return s
	}

	shown := min(len(d.members), maxMemberRows)
	for _, member := range d.members[:shown] {
		s.Rows = append(s.Rows, m.memberRow(member))
	}

	// The total comes off the store, which is where whoever asked for it
	// put it — the thread header asks on every chat open, and a basic
	// group's members call writes the one it got for free. Zero means
	// nobody has been told yet, and the remainder row is simply absent
	// until they are: a section that says "+0 more" or counts the page it
	// happens to hold is worse than one that waits a beat.
	total := 0
	if m.store != nil {
		total = int(m.store.Chats.MemberCount(m.chatID))
	}
	if total > shown {
		s.Count = total
	}
	// The remainder counts against the chat's real member total, not
	// against how many the participants call happened to return: a group of
	// two hundred returns two hundred, and "+192 more" is the honest number
	// either way.
	if rest := total - shown; rest > 0 {
		s.Rows = append(s.Rows, Row{Kind: RowMore, Text: "+" + strconv.Itoa(rest) + " more"})
	}
	return s
}

// memberRow describes one member: presence, name, and either their role or
// when they were last seen.
//
// Role over last-seen when there is one, because a role is a fact about the
// chat and a last-seen is a fact about the person — and this is the chat's
// rail.
func (m Model) memberRow(member *telegram.ChatMember) Row {
	row := Row{Kind: RowMemberOffline}

	userID := int64(0)
	if u, ok := member.MemberID.(*telegram.MessageSenderUser); ok {
		userID = u.UserID
	}
	row.ID = userID

	if m.store != nil {
		row.Text = m.store.Users.DisplayName(userID)
		if m.store.Users.IsOnline(userID) {
			row.Kind = RowMemberOnline
		}
	}
	if row.Text == "" || row.Text == "Unknown" {
		row.Text = "user " + strconv.FormatInt(userID, 10)
	}

	if role := memberRole(member.Status); role != "" {
		row.Right = role
	} else if row.Kind == RowMemberOffline && m.store != nil {
		if u, ok := m.store.Users.Get(userID); ok {
			row.Right = lastSeenShort(u.Status)
		}
	}
	return row
}

// stateNote is the honest one-row explanation for a section with nothing to
// show, and whether one is needed.
//
// Four different situations that would otherwise all render as an empty
// section: not asked, asked and waiting, asked and refused, asked and there
// genuinely is nothing. Only the last of those means "this chat has no
// files", and a reader cannot tell them apart from blank space.
func stateNote(state sectionState, rows int) (string, bool) {
	switch state {
	case stateIdle:
		return "not loaded", true
	case stateLoading:
		return "loading…", true
	case stateFailed:
		return "unavailable", true
	}
	if rows == 0 {
		return "none", true
	}
	return "", false
}

// messageSummary is a one-line description of a message for a pinned row.
func messageSummary(msg *telegram.Message) string {
	if msg == nil {
		return "—"
	}
	if text, ok := msg.Content.(*telegram.MessageText); ok && text.Text != nil {
		return strings.Join(strings.Fields(text.Text.Text), " ")
	}
	name, _ := fileSummary(msg)
	return name
}

// fileSummary is a shared file's name and size.
func fileSummary(msg *telegram.Message) (string, string) {
	if msg == nil {
		return "—", ""
	}
	switch c := msg.Content.(type) {
	case *telegram.MessageDocument:
		if c.Document == nil {
			return "file", ""
		}
		name := c.Document.FileName
		if name == "" {
			name = "file"
		}
		return name, shortSize(c.Document.File)
	case *telegram.MessagePhoto:
		if c.Photo != nil && len(c.Photo.Sizes) > 0 {
			return "photo", shortSize(c.Photo.Sizes[len(c.Photo.Sizes)-1].File)
		}
		return "photo", ""
	case *telegram.MessageVideo:
		if c.Video == nil {
			return "video", ""
		}
		name := c.Video.FileName
		if name == "" {
			name = "video"
		}
		return name, shortSize(c.Video.File)
	case *telegram.MessageAudio:
		if c.Audio == nil {
			return "audio", ""
		}
		name := c.Audio.Title
		if name == "" {
			name = c.Audio.FileName
		}
		return name, shortSize(c.Audio.File)
	case *telegram.MessageVoiceNote:
		return "voice note", ""
	}
	return "attachment", ""
}

// linkSummary is the host of the first URL in a message, which is what a
// reader recognises a link by. The full URL never fits in thirty columns and
// its tail is the least identifying part of it.
func linkSummary(msg *telegram.Message) string {
	if msg == nil {
		return "—"
	}
	text, ok := msg.Content.(*telegram.MessageText)
	if !ok || text.Text == nil {
		return "link"
	}
	for _, field := range strings.Fields(text.Text.Text) {
		if host := hostOf(field); host != "" {
			return host
		}
	}
	return strings.Join(strings.Fields(text.Text.Text), " ")
}

// hostOf extracts a host from a bare URL without parsing it: a rail row is
// not a place to be strict about a scheme, and net/url would accept plenty of
// things that are not links at all.
func hostOf(s string) string {
	for _, prefix := range []string{"https://", "http://"} {
		if rest, ok := strings.CutPrefix(s, prefix); ok {
			host, _, _ := strings.Cut(rest, "/")
			return strings.TrimPrefix(host, "www.")
		}
	}
	return ""
}

// shortSize is a byte count in the rail's four-cell budget: whole units, no
// space, because "184K" fits beside a filename and "184 KB" does not.
func shortSize(f *telegram.File) string {
	if f == nil || f.Size <= 0 {
		return ""
	}
	switch b := f.Size; {
	case b >= 1<<30:
		return strconv.FormatInt(b/(1<<30), 10) + "G"
	case b >= 1<<20:
		return strconv.FormatInt(b/(1<<20), 10) + "M"
	case b >= 1<<10:
		return strconv.FormatInt(b/(1<<10), 10) + "K"
	default:
		return strconv.FormatInt(b, 10) + "B"
	}
}

// memberRole is the word for a member's status, empty for an ordinary one.
func memberRole(status telegram.ChatMemberStatus) string {
	switch status.(type) {
	case *telegram.ChatMemberStatusCreator:
		return "owner"
	case *telegram.ChatMemberStatusAdministrator:
		return "admin"
	case *telegram.ChatMemberStatusRestricted:
		return "limited"
	case *telegram.ChatMemberStatusBanned:
		return "banned"
	}
	return ""
}

// lastSeenShort is a compact last-seen for the rail's right-hand field.
//
// The vaguer statuses get no text at all rather than a guess: Telegram's
// "recently" covers anything up to three days, and putting a number on it
// would be inventing precision the server deliberately withheld.
func lastSeenShort(status telegram.UserStatus) string {
	if off, ok := status.(*telegram.UserStatusOffline); ok && off.WasOnline > 0 {
		return render.FormatRelativeShort(off.WasOnline)
	}
	return ""
}
