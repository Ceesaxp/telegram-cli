package telegram

import (
	"fmt"

	"github.com/gotd/td/tg"
)

// MediaFilter selects which of a chat's messages a filtered search asks for.
//
// These are Telegram's own server-side indexes, not a scan of loaded history:
// asking for a chat's pinned messages or its files is one request that
// returns them however far back they are. Deriving the same lists from the
// pages this client happens to have loaded would produce a recent-files
// sample and call it the chat's files.
type MediaFilter int

const (
	// MediaFilterPinned is every pinned message, newest first.
	MediaFilterPinned MediaFilter = iota
	// MediaFilterFiles is documents: anything sent as a file rather than
	// as an inline photo.
	MediaFilterFiles
	// MediaFilterPhotos is photos and videos.
	MediaFilterPhotos
	// MediaFilterLinks is messages containing a URL.
	MediaFilterLinks
)

func (f MediaFilter) tg() tg.MessagesFilterClass {
	switch f {
	case MediaFilterFiles:
		return &tg.InputMessagesFilterDocument{}
	case MediaFilterPhotos:
		return &tg.InputMessagesFilterPhotoVideo{}
	case MediaFilterLinks:
		return &tg.InputMessagesFilterURL{}
	default:
		return &tg.InputMessagesFilterPinned{}
	}
}

// String names the filter for error messages and logs.
func (f MediaFilter) String() string {
	switch f {
	case MediaFilterFiles:
		return "files"
	case MediaFilterPhotos:
		return "photos"
	case MediaFilterLinks:
		return "links"
	default:
		return "pinned"
	}
}

// SearchChatMedia returns a chat's messages of one kind, newest first.
//
// This is the rail's data path. Unlike [SearchChatMessages] it takes no query
// — the filter IS the query, and MTProto answers a filter-only search with
// the whole server-side index for that kind. Requiring a query here would
// make "this chat's files" unaskable.
//
// limit is capped rather than rejected: the rail shows a handful of rows, and
// a caller asking for more than a screenful of them has made an arithmetic
// mistake, not a request worth failing.
func (c *Client) SearchChatMedia(chatID int64, filter MediaFilter, limit int32) ([]*Message, error) {
	ctx, cancel := opCtx()
	defer cancel()

	peer, err := c.inputPeer(ctx, chatID)
	if err != nil {
		return nil, fmt.Errorf("search chat %s: %w", filter, err)
	}
	if limit <= 0 || limit > 100 {
		limit = 100
	}

	res, err := c.api.MessagesSearch(ctx, &tg.MessagesSearchRequest{
		Peer:   peer,
		Q:      "",
		Filter: filter.tg(),
		Limit:  int(limit),
		// The remaining bounds are deliberately zero: no date range, no
		// offset, no min/max ID clamp, and no result hash. These are plain
		// (non-flag) fields, so zero reaches the wire as "unbounded" rather
		// than "absent" — the same reasoning as SearchChatMessages.
		OffsetID:  0,
		MinDate:   0,
		MaxDate:   0,
		AddOffset: 0,
		MaxID:     0,
		MinID:     0,
		Hash:      0,
	})
	if err != nil {
		return nil, fmt.Errorf("search chat %s: %w", filter, err)
	}

	messages := messagesFromMessagesClass(res)
	out := make([]*Message, 0, len(messages))
	for _, mc := range messages {
		if m := c.messageClassFromTG(mc); m != nil {
			out = append(out, m)
		}
	}
	return out, nil
}
