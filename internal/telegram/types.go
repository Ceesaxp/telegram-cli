package telegram

import (
	"fmt"
	"strings"

	"github.com/gotd/td/constant"
	"github.com/gotd/td/tg"
)

// ChatType classifies a chat.
type ChatType int

const (
	ChatTypePrivate ChatType = iota
	ChatTypeBasicGroup
	ChatTypeSupergroup
	ChatTypeChannel
)

// Chat is the domain representation of a Telegram dialog.
type Chat struct {
	ID       int64
	Type     ChatType
	Title    string
	Username string

	// Photo is the small avatar file; ID is a file registry key.
	Photo *File

	LastMessage *Message

	UnreadCount             int32
	LastReadInboxMessageID  int64
	LastReadOutboxMessageID int64

	// Pinned and Order define chat list ordering: pinned first,
	// then by Order descending (unix time of the last message).
	Pinned bool
	Order  int64

	// Muted mirrors the peer's notification settings: an explicit
	// silent flag or a mute-until date in the future.
	Muted bool
}

// MessageSender identifies who sent a message.
type MessageSender interface {
	messageSender()
}

// MessageSenderUser is a message sent by a user.
type MessageSenderUser struct {
	UserID int64
}

// MessageSenderChat is a message sent on behalf of a chat/channel.
type MessageSenderChat struct {
	ChatID int64
}

func (*MessageSenderUser) messageSender() {}
func (*MessageSenderChat) messageSender() {}

// Message is the domain representation of a Telegram message.
type Message struct {
	ID     int64
	ChatID int64

	SenderID MessageSender

	Date          int32
	EditDate      int32
	IsOutgoing    bool
	IsChannelPost bool
	IsForwarded   bool

	ReplyToMessageID int64

	Content MessageContent

	// Reactions are the emoji tallies on this message, in the order
	// Telegram ranks them. Nil when nobody has reacted.
	Reactions []*Reaction
}

// MessageContent is the payload of a message.
type MessageContent interface {
	messageContent()
}

// MessageText is a plain text message.
type MessageText struct {
	Text *FormattedText

	// WebPage is the link preview Telegram attached to the text, or nil.
	// It hangs off the text rather than replacing it: the preview is a
	// second reading of a link the sender already wrote out.
	WebPage *WebPage
}

// MessagePhoto is a photo message.
type MessagePhoto struct {
	Photo   *Photo
	Caption *FormattedText
}

// MessageVideo is a video message.
type MessageVideo struct {
	Video   *Video
	Caption *FormattedText
}

// MessageDocument is a generic file message.
type MessageDocument struct {
	Document *Document
	Caption  *FormattedText
}

// MessageVoiceNote is a voice message.
type MessageVoiceNote struct {
	VoiceNote *VoiceNote
	Caption   *FormattedText
}

// MessageVideoNote is a round video message.
type MessageVideoNote struct {
	VideoNote *VideoNote
}

// MessageSticker is a sticker message.
type MessageSticker struct {
	Sticker *Sticker
}

// MessageAnimation is a GIF message.
type MessageAnimation struct {
	Animation *Animation
	Caption   *FormattedText
}

// MessageAudio is an audio (music) message.
type MessageAudio struct {
	Audio   *Audio
	Caption *FormattedText
}

// MessageLocation is a geo point message.
type MessageLocation struct {
	Location *Location
}

// MessageContact is a shared contact message.
type MessageContact struct {
	Contact *Contact
}

// MessagePoll is a poll message.
type MessagePoll struct {
	Poll *Poll
}

// Service messages.

// MessagePinMessage is a service message about a pinned message.
type MessagePinMessage struct{}

// MessageChatAddMembers is a service message about added members.
type MessageChatAddMembers struct{}

// MessageChatDeleteMember is a service message about a removed member.
type MessageChatDeleteMember struct{}

// MessageChatChangeTitle is a service message about a title change.
type MessageChatChangeTitle struct {
	Title string
}

// MessageChatChangePhoto is a service message about a photo change.
type MessageChatChangePhoto struct{}

// MessageChatJoinByLink is a service message about joining via invite link.
type MessageChatJoinByLink struct{}

// MessageUnsupported is anything we don't map explicitly.
type MessageUnsupported struct {
	Type string
}

func (*MessageText) messageContent()             {}
func (*MessagePhoto) messageContent()            {}
func (*MessageVideo) messageContent()            {}
func (*MessageDocument) messageContent()         {}
func (*MessageVoiceNote) messageContent()        {}
func (*MessageVideoNote) messageContent()        {}
func (*MessageSticker) messageContent()          {}
func (*MessageAnimation) messageContent()        {}
func (*MessageAudio) messageContent()            {}
func (*MessageLocation) messageContent()         {}
func (*MessageContact) messageContent()          {}
func (*MessagePoll) messageContent()             {}
func (*MessagePinMessage) messageContent()       {}
func (*MessageChatAddMembers) messageContent()   {}
func (*MessageChatDeleteMember) messageContent() {}
func (*MessageChatChangeTitle) messageContent()  {}
func (*MessageChatChangePhoto) messageContent()  {}
func (*MessageChatJoinByLink) messageContent()   {}
func (*MessageUnsupported) messageContent()      {}

// Photo is a photo with several sizes.
type Photo struct {
	ID    int64
	Sizes []*PhotoSize
}

// PhotoSize is one size variant of a photo.
type PhotoSize struct {
	Type   string
	Width  int
	Height int
	File   *File
}

// Video is a video file.
type Video struct {
	FileName  string
	Duration  int32
	Width     int
	Height    int
	File      *File
	Thumbnail *File
}

// Document is a generic file.
type Document struct {
	FileName  string
	MimeType  string
	File      *File
	Thumbnail *File
}

// VoiceNote is a voice message file.
type VoiceNote struct {
	Duration int32
	File     *File

	// Waveform is one amplitude per sample, each 0–31, already unpacked
	// from the five-bit encoding Telegram sends. Nil when the sender's
	// client did not compute one.
	Waveform []byte
}

// VideoNote is a round video file.
type VideoNote struct {
	Duration int32
	File     *File
}

// Sticker is a sticker.
type Sticker struct {
	Emoji string
	File  *File
}

// Animation is a GIF file.
type Animation struct {
	FileName string
	Duration int32
	File     *File
}

// Audio is an audio file.
type Audio struct {
	Title     string
	Performer string
	FileName  string
	Duration  int32
	File      *File
}

// Location is a geo point.
type Location struct {
	Latitude  float64
	Longitude float64
}

// Contact is a shared contact.
type Contact struct {
	FirstName   string
	LastName    string
	PhoneNumber string
}

// Poll is a poll: its question, its answers, and whatever tallies the
// server sent with them.
type Poll struct {
	Question string
	Options  []*PollOption

	// TotalVoterCount is how many people have voted, which is not the sum
	// of the options: a multiple-choice poll counts one voter once and
	// their answers several times.
	TotalVoterCount int32

	// ResultsKnown is whether the server sent per-option tallies at all.
	// A poll that hides its results until it closes sends none, and its
	// options must then be drawn without bars — an empty bar is a result,
	// not the absence of one.
	ResultsKnown bool

	IsAnonymous    bool
	IsClosed       bool
	MultipleChoice bool
	IsQuiz         bool

	// CloseDate is the unix time the poll closes, 0 when it has no
	// scheduled end.
	CloseDate int32
}

// PollOption is one answer of a poll.
type PollOption struct {
	Text       string
	VoterCount int32

	// Percent is this option's share of the vote, apportioned by largest
	// remainder so that the options of a poll sum to exactly 100.
	Percent int32

	// Chosen is whether the local user picked this option. Correct is
	// whether a quiz counts it as the right answer — set only once the
	// user has answered, since that is when Telegram sends it.
	Chosen  bool
	Correct bool
}

// Reaction is one emoji's tally on a message.
type Reaction struct {
	// Emoji is the reaction's emoticon, empty for a custom one.
	Emoji string

	// CustomEmojiID identifies a custom emoji reaction, whose artwork is
	// a document this client does not fetch; 0 for a standard reaction.
	// A chip drawn from it says a reaction exists and admits it cannot
	// show which, rather than substituting an emoji nobody sent.
	CustomEmojiID int64

	Count int32

	// Chosen is whether the local user is one of the reactors. Telegram
	// omits it from the copies of a message it sends to everyone (the
	// "min" form), so a false here can mean "not known".
	Chosen bool
}

// WebPage is the link preview Telegram attaches to a message's text.
type WebPage struct {
	URL         string
	DisplayURL  string
	SiteName    string
	Title       string
	Description string
}

// FormattedText is text with formatting entities.
type FormattedText struct {
	Text     string
	Entities []*TextEntity
}

// TextEntity is a formatting span. Offset and Length are RUNE indices
// into the owning FormattedText.Text, already converted from the UTF-16
// code units Telegram sends — see formattedTextFromTG. Consumers may
// slice []rune with them directly.
type TextEntity struct {
	Offset int32
	Length int32
	Type   TextEntityType
}

// TextEntityType classifies a formatting span.
type TextEntityType interface {
	textEntityType()
}

type TextEntityTypeBold struct{}
type TextEntityTypeItalic struct{}
type TextEntityTypeUnderline struct{}
type TextEntityTypeStrikethrough struct{}
type TextEntityTypeCode struct{}
type TextEntityTypePre struct{}
type TextEntityTypePreCode struct {
	Language string
}
type TextEntityTypeTextURL struct {
	URL string
}
type TextEntityTypeURL struct{}
type TextEntityTypeMention struct{}
type TextEntityTypeMentionName struct {
	UserID int64
}
type TextEntityTypeHashtag struct{}
type TextEntityTypeBotCommand struct{}
type TextEntityTypeEmailAddress struct{}
type TextEntityTypeSpoiler struct{}
type TextEntityTypeBlockQuote struct{}

func (*TextEntityTypeBold) textEntityType()          {}
func (*TextEntityTypeItalic) textEntityType()        {}
func (*TextEntityTypeUnderline) textEntityType()     {}
func (*TextEntityTypeStrikethrough) textEntityType() {}
func (*TextEntityTypeCode) textEntityType()          {}
func (*TextEntityTypePre) textEntityType()           {}
func (*TextEntityTypePreCode) textEntityType()       {}
func (*TextEntityTypeTextURL) textEntityType()       {}
func (*TextEntityTypeURL) textEntityType()           {}
func (*TextEntityTypeMention) textEntityType()       {}
func (*TextEntityTypeMentionName) textEntityType()   {}
func (*TextEntityTypeHashtag) textEntityType()       {}
func (*TextEntityTypeBotCommand) textEntityType()    {}
func (*TextEntityTypeEmailAddress) textEntityType()  {}
func (*TextEntityTypeSpoiler) textEntityType()       {}
func (*TextEntityTypeBlockQuote) textEntityType()    {}

// User is the domain representation of a Telegram user.
type User struct {
	ID          int64
	FirstName   string
	LastName    string
	Username    string
	PhoneNumber string
	IsBot       bool
	Status      UserStatus
}

// UserStatus describes last-seen state.
type UserStatus interface {
	userStatus()
}

type UserStatusOnline struct {
	Expires int32
}
type UserStatusOffline struct {
	WasOnline int32
}
type UserStatusRecently struct{}
type UserStatusLastWeek struct{}
type UserStatusLastMonth struct{}
type UserStatusEmpty struct{}

func (*UserStatusOnline) userStatus()    {}
func (*UserStatusOffline) userStatus()   {}
func (*UserStatusRecently) userStatus()  {}
func (*UserStatusLastWeek) userStatus()  {}
func (*UserStatusLastMonth) userStatus() {}
func (*UserStatusEmpty) userStatus()     {}

// ChatMember is a member of a group or channel.
type ChatMember struct {
	MemberID MessageSender
	Status   ChatMemberStatus
}

// ChatMemberStatus is the role of a chat member.
type ChatMemberStatus interface {
	chatMemberStatus()
}

type ChatMemberStatusCreator struct {
	CustomTitle string
}
type ChatMemberStatusAdministrator struct {
	CustomTitle string
}
type ChatMemberStatusMember struct{}
type ChatMemberStatusRestricted struct{}
type ChatMemberStatusBanned struct{}
type ChatMemberStatusLeft struct{}

func (*ChatMemberStatusCreator) chatMemberStatus()       {}
func (*ChatMemberStatusAdministrator) chatMemberStatus() {}
func (*ChatMemberStatusMember) chatMemberStatus()        {}
func (*ChatMemberStatusRestricted) chatMemberStatus()    {}
func (*ChatMemberStatusBanned) chatMemberStatus()        {}
func (*ChatMemberStatusLeft) chatMemberStatus()          {}

// File is a downloadable/downloaded file.
// ID is a registry key (e.g. "doc:123", "photo:456:y", "avatar:-100123").
type File struct {
	ID         string
	Path       string
	Size       int64
	Downloaded bool
}

// ChatAction is a user activity in a chat (typing etc).
type ChatAction interface {
	chatAction()
}

// ChatActionTyping means the user is typing (or recording/uploading).
type ChatActionTyping struct{}

// ChatActionCancel means the user stopped the action.
type ChatActionCancel struct{}

func (*ChatActionTyping) chatAction() {}
func (*ChatActionCancel) chatAction() {}

// ConnectionState is the simplified network state.
type ConnectionState int

const (
	ConnectionStateConnecting ConnectionState = iota
	ConnectionStateReady
	// ConnectionStateDisconnected means the client run loop has exited:
	// the connection is gone for good and will not recover on its own.
	// Appended last on purpose — the existing values keep their numbers,
	// so consumers comparing against them are unaffected.
	ConnectionStateDisconnected
)

// --- Conversion helpers ---

// sanitizeTerminal neutralises terminal control sequences in text that
// originates from a remote peer. Message text, captions, chat titles,
// user names and file names are eventually written raw to the terminal,
// where an embedded ESC/OSC sequence could retitle the window, move the
// cursor, or write the user's clipboard via OSC 52.
//
// Every rune below 0x20 except '\n' and '\t', plus DEL (0x7F) and the C1
// range 0x80–0x9F, is REPLACED (never deleted) with U+FFFD. Replacement
// is mandatory: FormattedText entity offsets are computed against the
// original string, so the rune — and UTF-16 code unit — count must not
// change. All replaced code points are one UTF-16 unit wide, as is
// U+FFFD, so entity offsets stay valid.
func sanitizeTerminal(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return r
		case r < 0x20, r == 0x7F, r >= 0x80 && r <= 0x9F:
			return '\uFFFD'
		default:
			return r
		}
	}, s)
}

// mutedFromNotifySettings reports whether notification settings mean the
// peer is muted: an explicit silent flag, or a mute-until date that has
// not passed yet. now is a unix timestamp.
func mutedFromNotifySettings(s tg.PeerNotifySettings, now int64) bool {
	if silent, ok := s.GetSilent(); ok && silent {
		return true
	}
	if until, ok := s.GetMuteUntil(); ok && int64(until) > now {
		return true
	}
	return false
}

// chatIDFromPeer converts a tg peer to the canonical TDLib-style chat ID
// (user → userID, chat → −chatID, channel → −100·channelID).
func chatIDFromPeer(p tg.PeerClass) int64 {
	var id constant.TDLibPeerID
	switch v := p.(type) {
	case *tg.PeerUser:
		id.User(v.UserID)
	case *tg.PeerChat:
		id.Chat(v.ChatID)
	case *tg.PeerChannel:
		id.Channel(v.ChannelID)
	default:
		return 0
	}
	return int64(id)
}

// channelChatID returns the canonical chat ID for a channel ID.
func channelChatID(channelID int64) int64 {
	var id constant.TDLibPeerID
	id.Channel(channelID)
	return int64(id)
}

// basicGroupChatID returns the canonical chat ID for a basic group ID.
func basicGroupChatID(chatID int64) int64 {
	var id constant.TDLibPeerID
	id.Chat(chatID)
	return int64(id)
}

// userChatID returns the canonical chat ID for a user ID.
func userChatID(userID int64) int64 {
	var id constant.TDLibPeerID
	id.User(userID)
	return int64(id)
}

// plainChatID extracts the bare peer ID from a canonical chat ID.
func plainChatID(chatID int64) int64 {
	return constant.TDLibPeerID(chatID).ToPlain()
}

// senderFromPeer converts a message sender peer to a domain MessageSender.
func senderFromPeer(p tg.PeerClass) MessageSender {
	switch v := p.(type) {
	case *tg.PeerUser:
		return &MessageSenderUser{UserID: v.UserID}
	default:
		return &MessageSenderChat{ChatID: chatIDFromPeer(v)}
	}
}

// userFromTG converts a tg user to the domain User.
func userFromTG(u *tg.User) *User {
	lastName, _ := u.GetLastName()
	username, _ := u.GetUsername()
	phone, _ := u.GetPhone()
	return &User{
		ID:          u.ID,
		FirstName:   sanitizeTerminal(u.FirstName),
		LastName:    sanitizeTerminal(lastName),
		Username:    sanitizeTerminal(username),
		PhoneNumber: sanitizeTerminal(phone),
		IsBot:       u.Bot,
		Status:      userStatusFromTG(u.Status),
	}
}

// userStatusFromTG converts a tg status to the domain UserStatus.
func userStatusFromTG(s tg.UserStatusClass) UserStatus {
	switch v := s.(type) {
	case *tg.UserStatusOnline:
		return &UserStatusOnline{Expires: int32(v.Expires)}
	case *tg.UserStatusOffline:
		return &UserStatusOffline{WasOnline: int32(v.WasOnline)}
	case *tg.UserStatusRecently:
		return &UserStatusRecently{}
	case *tg.UserStatusLastWeek:
		return &UserStatusLastWeek{}
	case *tg.UserStatusLastMonth:
		return &UserStatusLastMonth{}
	default:
		return &UserStatusEmpty{}
	}
}

// formattedTextFromTG converts text + tg entities to FormattedText.
//
// Telegram measures entity offsets and lengths in UTF-16 code units, but
// every consumer in this codebase indexes []rune with them. The two
// disagree as soon as the text contains a non-BMP character: in
// "\U0001F600 bold" the emoji is one rune but two UTF-16 units, so the
// bold span arrives as offset 3 length 4 — runes[3:7] over six runes,
// which panics. Convert here, at the boundary, so nothing downstream
// ever sees a UTF-16 index.
func formattedTextFromTG(text string, entities []tg.MessageEntityClass) *FormattedText {
	// Remote text reaches the terminal verbatim; neutralise control
	// sequences here. sanitizeTerminal preserves both the rune count and
	// the UTF-16 length, so it commutes with the conversion below.
	ft := &FormattedText{Text: sanitizeTerminal(text)}
	if len(entities) == 0 {
		return ft
	}

	runeAt := utf16RuneIndex(ft.Text)
	lastUnit := len(runeAt) - 1

	for _, e := range entities {
		// Offsets are attacker-controlled: clamp instead of trusting
		// them to be in range. Arithmetic stays in int to avoid an
		// int32 overflow on a hostile length.
		start := clampOffset(e.GetOffset(), 0, lastUnit)
		end := clampOffset(e.GetOffset()+e.GetLength(), start, lastUnit)
		ft.Entities = append(ft.Entities, &TextEntity{
			Offset: runeAt[start],
			Length: runeAt[end] - runeAt[start],
			Type:   entityTypeFromTG(e),
		})
	}
	return ft
}

// utf16RuneIndex builds a lookup from UTF-16 code unit offset in s to the
// corresponding rune index. The table has one entry per code unit plus a
// terminator holding the total rune count, so both an entity's start and
// its end offset can be translated. An offset that lands on the low half
// of a surrogate pair resolves to the start of that rune.
func utf16RuneIndex(s string) []int32 {
	table := make([]int32, 0, len(s)+1)
	var idx int32
	for _, r := range s {
		table = append(table, idx)
		if r > 0xFFFF {
			// Non-BMP: encoded as a surrogate pair, two code units.
			table = append(table, idx)
		}
		idx++
	}
	return append(table, idx)
}

// clampOffset bounds v to [lo, hi].
func clampOffset(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// entityTypeFromTG maps a tg entity class to the domain type.
func entityTypeFromTG(e tg.MessageEntityClass) TextEntityType {
	switch v := e.(type) {
	case *tg.MessageEntityBold:
		return &TextEntityTypeBold{}
	case *tg.MessageEntityItalic:
		return &TextEntityTypeItalic{}
	case *tg.MessageEntityUnderline:
		return &TextEntityTypeUnderline{}
	case *tg.MessageEntityStrike:
		return &TextEntityTypeStrikethrough{}
	case *tg.MessageEntityCode:
		return &TextEntityTypeCode{}
	case *tg.MessageEntityPre:
		if v.Language != "" {
			return &TextEntityTypePreCode{Language: sanitizeTerminal(v.Language)}
		}
		return &TextEntityTypePre{}
	case *tg.MessageEntityTextURL:
		return &TextEntityTypeTextURL{URL: sanitizeTerminal(v.URL)}
	case *tg.MessageEntityURL:
		return &TextEntityTypeURL{}
	case *tg.MessageEntityMention:
		return &TextEntityTypeMention{}
	case *tg.MessageEntityMentionName:
		return &TextEntityTypeMentionName{UserID: v.UserID}
	case *tg.MessageEntityHashtag:
		return &TextEntityTypeHashtag{}
	case *tg.MessageEntityBotCommand:
		return &TextEntityTypeBotCommand{}
	case *tg.MessageEntityEmail:
		return &TextEntityTypeEmailAddress{}
	case *tg.MessageEntitySpoiler:
		return &TextEntityTypeSpoiler{}
	case *tg.MessageEntityBlockquote:
		return &TextEntityTypeBlockQuote{}
	default:
		return nil
	}
}

// messageFromTG converts a tg message to the domain Message.
// Media files are registered in the client's file registry as a side effect.
func (c *Client) messageFromTG(m *tg.Message) *Message {
	msg := &Message{
		ID:            int64(m.ID),
		ChatID:        chatIDFromPeer(m.PeerID),
		Date:          int32(m.Date),
		IsOutgoing:    m.Out,
		IsChannelPost: m.Post,
	}

	if from, ok := m.GetFromID(); ok {
		msg.SenderID = senderFromPeer(from)
	} else if p, ok := m.PeerID.(*tg.PeerUser); ok {
		msg.SenderID = &MessageSenderUser{UserID: p.UserID}
	} else {
		msg.SenderID = &MessageSenderChat{ChatID: msg.ChatID}
	}

	if rt, ok := m.GetReplyTo(); ok {
		if h, ok := rt.(*tg.MessageReplyHeader); ok {
			if id, ok := h.GetReplyToMsgID(); ok {
				msg.ReplyToMessageID = int64(id)
			}
		}
	}

	if editDate, ok := m.GetEditDate(); ok {
		msg.EditDate = int32(editDate)
	}

	if _, ok := m.GetFwdFrom(); ok {
		msg.IsForwarded = true
	}

	if reactions, ok := m.GetReactions(); ok {
		msg.Reactions = reactionsFromTG(reactions)
	}

	entities, _ := m.GetEntities()
	caption := formattedTextFromTG(m.Message, entities)

	if media, ok := m.GetMedia(); ok {
		if content := c.contentFromMedia(media, caption); content != nil {
			msg.Content = content
			return msg
		}
		// Media we intentionally fall through on (e.g. web page preview):
		// render the caption as plain text.
	}

	msg.Content = &MessageText{Text: caption}
	return msg
}

// messageFromTGService converts a service message to the domain Message.
func (c *Client) messageFromTGService(m *tg.MessageService) *Message {
	msg := &Message{
		ID:         int64(m.ID),
		ChatID:     chatIDFromPeer(m.PeerID),
		Date:       int32(m.Date),
		IsOutgoing: m.Out,
	}

	if from, ok := m.GetFromID(); ok {
		msg.SenderID = senderFromPeer(from)
	} else {
		msg.SenderID = &MessageSenderChat{ChatID: msg.ChatID}
	}

	switch a := m.Action.(type) {
	case *tg.MessageActionPinMessage:
		msg.Content = &MessagePinMessage{}
	case *tg.MessageActionChatAddUser:
		msg.Content = &MessageChatAddMembers{}
	case *tg.MessageActionChatDeleteUser:
		msg.Content = &MessageChatDeleteMember{}
	case *tg.MessageActionChatEditTitle:
		msg.Content = &MessageChatChangeTitle{Title: sanitizeTerminal(a.Title)}
	case *tg.MessageActionChatEditPhoto:
		msg.Content = &MessageChatChangePhoto{}
	case *tg.MessageActionChatJoinedByLink:
		msg.Content = &MessageChatJoinByLink{}
	default:
		msg.Content = &MessageUnsupported{Type: m.Action.TypeName()}
	}
	return msg
}

// messageClassFromTG converts any message class; nil for empty messages.
func (c *Client) messageClassFromTG(m tg.MessageClass) *Message {
	switch v := m.(type) {
	case *tg.Message:
		return c.messageFromTG(v)
	case *tg.MessageService:
		return c.messageFromTGService(v)
	default:
		return nil
	}
}

// contentFromMedia converts tg media to a domain content.
// Returns nil when the media should be ignored in favor of the text
// (e.g. web page preview).
func (c *Client) contentFromMedia(media tg.MessageMediaClass, caption *FormattedText) MessageContent {
	switch m := media.(type) {
	case *tg.MessageMediaPhoto:
		photo, ok := m.Photo.(*tg.Photo)
		if !ok {
			return &MessageUnsupported{Type: media.TypeName()}
		}
		return &MessagePhoto{Photo: c.photoFromTG(photo), Caption: caption}

	case *tg.MessageMediaDocument:
		doc, ok := m.Document.(*tg.Document)
		if !ok {
			return &MessageUnsupported{Type: media.TypeName()}
		}
		return c.contentFromDocument(doc, caption)

	case *tg.MessageMediaGeo:
		point, ok := m.Geo.(*tg.GeoPoint)
		if !ok {
			return &MessageUnsupported{Type: media.TypeName()}
		}
		return &MessageLocation{Location: &Location{
			Latitude:  point.Lat,
			Longitude: point.Long,
		}}

	case *tg.MessageMediaContact:
		return &MessageContact{Contact: &Contact{
			FirstName:   sanitizeTerminal(m.FirstName),
			LastName:    sanitizeTerminal(m.LastName),
			PhoneNumber: sanitizeTerminal(m.PhoneNumber),
		}}

	case *tg.MessageMediaPoll:
		return &MessagePoll{Poll: pollFromTG(m.Poll, m.Results)}

	case *tg.MessageMediaWebPage:
		// A preview is an attachment ON the text, not instead of it: the
		// message is still the sentence the sender wrote, with a second
		// reading of one of its links below.
		return &MessageText{Text: caption, WebPage: webPageFromTG(m.Webpage)}

	default:
		return &MessageUnsupported{Type: media.TypeName()}
	}
}

// contentFromDocument classifies a tg document into a domain content.
func (c *Client) contentFromDocument(doc *tg.Document, caption *FormattedText) MessageContent {
	var (
		fileName  string
		duration  int
		isVideo   bool
		isRound   bool
		isAudio   bool
		isVoice   bool
		isSticker bool
		animated  bool
		waveform  []byte
		sticker   string
		title     string
		performer string
		width     int
		height    int
	)

	for _, attr := range doc.Attributes {
		switch a := attr.(type) {
		case *tg.DocumentAttributeFilename:
			fileName = sanitizeTerminal(a.FileName)
		case *tg.DocumentAttributeVideo:
			isVideo = true
			isRound = a.RoundMessage
			duration = int(a.Duration)
			width = a.W
			height = a.H
		case *tg.DocumentAttributeAudio:
			isAudio = true
			isVoice = a.Voice
			duration = int(a.Duration)
			title, _ = a.GetTitle()
			performer, _ = a.GetPerformer()
			title = sanitizeTerminal(title)
			performer = sanitizeTerminal(performer)
			waveform, _ = a.GetWaveform()
		case *tg.DocumentAttributeSticker:
			isSticker = true
			sticker = sanitizeTerminal(a.Alt)
		case *tg.DocumentAttributeAnimated:
			animated = true
		}
	}

	file := c.registerDocument(doc, fileName)

	switch {
	case isSticker:
		return &MessageSticker{Sticker: &Sticker{Emoji: sticker, File: file}}
	case animated:
		return &MessageAnimation{Animation: &Animation{
			FileName: fileName,
			Duration: int32(duration),
			File:     file,
		}, Caption: caption}
	case isVideo && isRound:
		return &MessageVideoNote{VideoNote: &VideoNote{
			Duration: int32(duration),
			File:     file,
		}}
	case isVideo:
		return &MessageVideo{Video: &Video{
			FileName:  fileName,
			Duration:  int32(duration),
			Width:     width,
			Height:    height,
			File:      file,
			Thumbnail: c.registerDocumentThumb(doc),
		}, Caption: caption}
	case isAudio && isVoice:
		return &MessageVoiceNote{VoiceNote: &VoiceNote{
			Duration: int32(duration),
			File:     file,
			Waveform: decodeWaveform(waveform),
		}, Caption: caption}
	case isAudio:
		return &MessageAudio{Audio: &Audio{
			Title:     title,
			Performer: performer,
			FileName:  fileName,
			Duration:  int32(duration),
			File:      file,
		}, Caption: caption}
	default:
		return &MessageDocument{Document: &Document{
			FileName:  fileName,
			MimeType:  doc.MimeType,
			File:      file,
			Thumbnail: c.registerDocumentThumb(doc),
		}, Caption: caption}
	}
}

// photoFromTG converts a tg photo and registers its sizes.
func (c *Client) photoFromTG(p *tg.Photo) *Photo {
	photo := &Photo{ID: p.ID}
	for _, s := range p.Sizes {
		var (
			typ    string
			width  int
			height int
			size   int
		)
		switch sz := s.(type) {
		case *tg.PhotoSize:
			typ, width, height, size = sz.Type, sz.W, sz.H, sz.Size
		case *tg.PhotoSizeProgressive:
			typ, width, height = sz.Type, sz.W, sz.H
			if len(sz.Sizes) > 0 {
				size = sz.Sizes[len(sz.Sizes)-1]
			}
		default:
			continue // cached/stripped sizes are not downloadable
		}
		photo.Sizes = append(photo.Sizes, &PhotoSize{
			Type:   typ,
			Width:  width,
			Height: height,
			File:   c.registerPhotoSize(p, typ, int64(size)),
		})
	}
	return photo
}

// chatFromUser builds a (synthetic) private chat entry from a user.
func (c *Client) chatFromUser(u *tg.User) *Chat {
	name := u.FirstName
	if last, ok := u.GetLastName(); ok && last != "" {
		if name != "" {
			name += " "
		}
		name += last
	}
	username, _ := u.GetUsername()
	var id constant.TDLibPeerID
	id.User(u.ID)
	chat := &Chat{
		ID:       int64(id),
		Type:     ChatTypePrivate,
		Title:    sanitizeTerminal(name),
		Username: sanitizeTerminal(username),
	}
	if photo, ok := u.GetPhoto(); ok {
		if p, ok := photo.(*tg.UserProfilePhoto); ok {
			chat.Photo = c.registerAvatar(chat.ID, p.PhotoID)
		}
	}
	return chat
}

// chatFromBasicGroup builds a chat entry from a basic group.
func (c *Client) chatFromBasicGroup(ch *tg.Chat) *Chat {
	var id constant.TDLibPeerID
	id.Chat(ch.ID)
	chat := &Chat{
		ID:    int64(id),
		Type:  ChatTypeBasicGroup,
		Title: sanitizeTerminal(ch.Title),
	}
	if p, ok := ch.GetPhoto().(*tg.ChatPhoto); ok {
		chat.Photo = c.registerAvatar(chat.ID, p.PhotoID)
	}
	return chat
}

// chatFromChannel builds a chat entry from a channel/supergroup.
func (c *Client) chatFromChannel(ch *tg.Channel) *Chat {
	var id constant.TDLibPeerID
	id.Channel(ch.ID)
	chatType := ChatTypeSupergroup
	if ch.Broadcast {
		chatType = ChatTypeChannel
	}
	username, _ := ch.GetUsername()
	chat := &Chat{
		ID:       int64(id),
		Type:     chatType,
		Title:    sanitizeTerminal(ch.Title),
		Username: sanitizeTerminal(username),
	}
	if p, ok := ch.GetPhoto().(*tg.ChatPhoto); ok {
		chat.Photo = c.registerAvatar(chat.ID, p.PhotoID)
	}
	return chat
}

// chatFromPeer looks the peer up in the update entities and converts it.
func (c *Client) chatFromPeer(peer tg.PeerClass, e tg.Entities) (*Chat, error) {
	switch p := peer.(type) {
	case *tg.PeerUser:
		if u, ok := e.Users[p.UserID]; ok {
			return c.chatFromUser(u), nil
		}
		return nil, fmt.Errorf("user %d not in entities", p.UserID)
	case *tg.PeerChat:
		if ch, ok := e.Chats[p.ChatID]; ok {
			return c.chatFromBasicGroup(ch), nil
		}
		return nil, fmt.Errorf("chat %d not in entities", p.ChatID)
	case *tg.PeerChannel:
		if ch, ok := e.Channels[p.ChannelID]; ok {
			return c.chatFromChannel(ch), nil
		}
		return nil, fmt.Errorf("channel %d not in entities", p.ChannelID)
	default:
		return nil, fmt.Errorf("unknown peer type %T", peer)
	}
}
