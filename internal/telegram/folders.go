package telegram

import (
	"fmt"

	"github.com/gotd/td/tg"
)

// AllChatsFolderID is the ID Telegram reserves for the implicit
// "All chats" folder. The server only ever names it explicitly via
// dialogFilterDefault; otherwise the UI is expected to synthesize it.
const AllChatsFolderID int32 = 0

// ChatFolder is the domain representation of a Telegram folder
// (a "dialog filter" in the MTProto schema).
//
// Chat IDs are canonical TDLib-style IDs, the same namespace as Chat.ID.
type ChatFolder struct {
	ID       int32
	Title    string
	Emoticon string

	PinnedChatIDs   []int64
	IncludedChatIDs []int64
	ExcludedChatIDs []int64

	// Category flags. Only plain folders (dialogFilter) carry these;
	// shared chatlist folders leave them false.
	Contacts    bool
	NonContacts bool
	Groups      bool
	Channels    bool
	Bots        bool

	ExcludeMuted    bool
	ExcludeRead     bool
	ExcludeArchived bool
}

// GetChatFolders returns the user's chat folders in server order.
func (c *Client) GetChatFolders() ([]*ChatFolder, error) {
	ctx, cancel := opCtx()
	defer cancel()

	res, err := c.api.MessagesGetDialogFilters(ctx)
	if err != nil {
		return nil, fmt.Errorf("get chat folders: %w", err)
	}

	out := make([]*ChatFolder, 0, len(res.Filters))
	for _, fc := range res.Filters {
		if f := chatFolderFromTG(fc); f != nil {
			out = append(out, f)
		}
	}
	return out, nil
}

// chatFolderFromTG maps a dialog filter to the domain ChatFolder.
// Returns nil for filter kinds we do not represent.
func chatFolderFromTG(fc tg.DialogFilterClass) *ChatFolder {
	switch f := fc.(type) {
	case *tg.DialogFilter:
		emoticon, _ := f.GetEmoticon()
		return &ChatFolder{
			ID:              int32(f.ID),
			Title:           sanitizeTerminal(f.Title.Text),
			Emoticon:        sanitizeTerminal(emoticon),
			PinnedChatIDs:   chatIDsFromInputPeers(f.PinnedPeers),
			IncludedChatIDs: chatIDsFromInputPeers(f.IncludePeers),
			ExcludedChatIDs: chatIDsFromInputPeers(f.ExcludePeers),
			Contacts:        f.Contacts,
			NonContacts:     f.NonContacts,
			Groups:          f.Groups,
			Channels:        f.Broadcasts,
			Bots:            f.Bots,
			ExcludeMuted:    f.ExcludeMuted,
			ExcludeRead:     f.ExcludeRead,
			ExcludeArchived: f.ExcludeArchived,
		}

	case *tg.DialogFilterChatlist:
		// A folder imported from a chat-folder deep link: it has no
		// category flags and no exclusions, only an explicit include list.
		emoticon, _ := f.GetEmoticon()
		return &ChatFolder{
			ID:              int32(f.ID),
			Title:           sanitizeTerminal(f.Title.Text),
			Emoticon:        sanitizeTerminal(emoticon),
			PinnedChatIDs:   chatIDsFromInputPeers(f.PinnedPeers),
			IncludedChatIDs: chatIDsFromInputPeers(f.IncludePeers),
		}

	case *tg.DialogFilterDefault:
		// The implicit "All chats" entry. It only shows up in ordering
		// responses; represent it so its position is preserved.
		return &ChatFolder{ID: AllChatsFolderID}

	default:
		return nil
	}
}

// chatIDsFromInputPeers converts filter peers to canonical chat IDs,
// dropping the ones that carry no usable identity.
func chatIDsFromInputPeers(peers []tg.InputPeerClass) []int64 {
	if len(peers) == 0 {
		return nil
	}
	out := make([]int64, 0, len(peers))
	for _, p := range peers {
		if id, ok := chatIDFromInputPeer(p); ok {
			out = append(out, id)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// chatIDFromInputPeer converts an InputPeer to the canonical TDLib-style
// chat ID. InputPeerSelf, InputPeerEmpty and the *FromMessage variants
// carry no resolvable ID here and are dropped.
func chatIDFromInputPeer(p tg.InputPeerClass) (int64, bool) {
	switch v := p.(type) {
	case *tg.InputPeerUser:
		return userChatID(v.UserID), true
	case *tg.InputPeerChat:
		return basicGroupChatID(v.ChatID), true
	case *tg.InputPeerChannel:
		return channelChatID(v.ChannelID), true
	default:
		return 0, false
	}
}
