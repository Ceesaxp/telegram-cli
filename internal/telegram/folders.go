package telegram

import (
	"context"
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

	// Raw InputPeers from the filter, kept so LoadFolderDialogs can
	// fetch include/pin chats that are not in the recency dialog page
	// (access hashes would be lost if we only stored IDs).
	pinnedPeers  []tg.InputPeerClass
	includePeers []tg.InputPeerClass
}

// NeedsPeerFetch reports whether this folder has explicit include/pin
// peers that must be loaded from the server to populate the list.
func (f *ChatFolder) NeedsPeerFetch() bool {
	return f != nil && (len(f.pinnedPeers) > 0 || len(f.includePeers) > 0)
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
			pinnedPeers:     f.PinnedPeers,
			includePeers:    f.IncludePeers,
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
			pinnedPeers:     f.PinnedPeers,
			includePeers:    f.IncludePeers,
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
// chat ID. InputPeerSelf and InputPeerEmpty carry no numeric ID here
// and are dropped from the ID lists (they are still sent to
// LoadFolderDialogs as raw peers).
func chatIDFromInputPeer(p tg.InputPeerClass) (int64, bool) {
	switch v := p.(type) {
	case *tg.InputPeerUser:
		return userChatID(v.UserID), true
	case *tg.InputPeerUserFromMessage:
		return userChatID(v.UserID), true
	case *tg.InputPeerChat:
		return basicGroupChatID(v.ChatID), true
	case *tg.InputPeerChannel:
		return channelChatID(v.ChannelID), true
	case *tg.InputPeerChannelFromMessage:
		return channelChatID(v.ChannelID), true
	default:
		return 0, false
	}
}

const folderPeerChunk = 100

// LoadFolderDialogs fetches dialogs for a folder's include and pin lists.
// Those chats often sit outside the recency window of MessagesGetDialogs,
// so filtering the first page alone under-counts folder membership.
func (c *Client) LoadFolderDialogs(folder *ChatFolder) ([]*Chat, error) {
	if folder == nil {
		return nil, nil
	}
	peers := uniqueInputPeers(folder.pinnedPeers, folder.includePeers)
	if len(peers) == 0 {
		return nil, nil
	}

	ctx, cancel := opCtx()
	defer cancel()

	var out []*Chat
	for i := 0; i < len(peers); i += folderPeerChunk {
		end := i + folderPeerChunk
		if end > len(peers) {
			end = len(peers)
		}
		req := make([]tg.InputDialogPeerClass, 0, end-i)
		for _, p := range peers[i:end] {
			req = append(req, &tg.InputDialogPeer{Peer: p})
		}
		res, err := c.api.MessagesGetPeerDialogs(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("get folder dialogs: %w", err)
		}
		chats, err := c.materializePeerDialogs(ctx, res)
		if err != nil {
			return nil, err
		}
		out = append(out, chats...)
	}
	return out, nil
}

func (c *Client) materializePeerDialogs(ctx context.Context, res *tg.MessagesPeerDialogs) ([]*Chat, error) {
	if res == nil {
		return nil, nil
	}
	if err := c.peers.Apply(ctx, res.Users, res.Chats); err != nil {
		return nil, fmt.Errorf("apply folder peers: %w", err)
	}
	chats, _, _ := c.chatsFromDialogParts(res.Dialogs, res.Messages, res.Users, res.Chats)
	return chats, nil
}

func uniqueInputPeers(lists ...[]tg.InputPeerClass) []tg.InputPeerClass {
	seen := make(map[int64]bool)
	var out []tg.InputPeerClass
	var noID []tg.InputPeerClass
	for _, list := range lists {
		for _, p := range list {
			if p == nil {
				continue
			}
			if _, ok := p.(*tg.InputPeerEmpty); ok {
				continue
			}
			if id, ok := chatIDFromInputPeer(p); ok {
				if seen[id] {
					continue
				}
				seen[id] = true
				out = append(out, p)
				continue
			}
			noID = append(noID, p)
		}
	}
	return append(out, noID...)
}
