package telegram

import (
	"cmp"
	"context"
	"log"
	"slices"
	"unicode/utf16"

	"github.com/gotd/td/tg"
)

// MentionSpan marks a run of the composer's text as a mention of one user.
//
// It is the contract the composer hands over at send time. Offset and
// Length count RUNES of the text exactly as the user typed it, before any
// Markdown is parsed; the send path follows the span through marker
// removal and converts it to the UTF-16 units the wire uses. The composer
// never has to know whether Markdown is on or what UTF-16 is.
//
// Only a user WITHOUT a username needs a span. A user with one is
// mentioned as plain "@username" text, which the server recognises on its
// own, so the composer sends no span for it.
type MentionSpan struct {
	Offset, Length int
	UserID         int64
}

// formatOutgoingWithMentions is formatOutgoing for text carrying mention
// spans. It returns the text to send, its entities with the mentions merged
// into the Markdown ones, and how many spans were dropped. Without spans it
// is exactly formatOutgoing.
//
// Each span is decided on its own. It is DROPPED, and the text still sent
// as typed, when:
//
//   - it falls outside the text, or its length is zero or negative;
//   - what it covers on the wire differs from what it covered in the draft,
//     which is what happens when it straddles a Markdown marker;
//   - it touches code or a code block, where text is literal;
//   - it overlaps a mention earlier in the text;
//   - its user cannot be resolved to an InputUser.
//
// Dropping costs only the link to the user, and it is always better than
// naming someone on text they were not picked for. A mention nested in
// bold, italic or any other formatting is legal in Telegram and is kept.
func (c *Client) formatOutgoingWithMentions(ctx context.Context, text string, spans []MentionSpan) (string, []tg.MessageEntityClass, int) {
	if len(spans) == 0 {
		body, entities := c.formatOutgoing(text)
		return body, entities, 0
	}

	body, entities, at := text, []tg.MessageEntityClass(nil), utf16Offsets(text)
	if c.markdownEnabled() {
		body, entities, at = parseMarkdownMapped(text)
	}
	out := &outgoing{src: []rune(text), units: utf16.Encode([]rune(body)), at: at, entities: entities}

	var mentions []tg.MessageEntityClass
	dropped, taken := 0, 0
	for _, s := range slices.SortedStableFunc(slices.Values(spans), func(a, b MentionSpan) int {
		return cmp.Compare(a.Offset, b.Offset)
	}) {
		mention, reason := c.mentionEntity(ctx, out, s, taken)
		if mention == nil {
			dropped++
			// No message text in the log: the span's position says enough.
			log.Printf("mention of user %d at runes %d+%d sent as plain text: %s",
				s.UserID, s.Offset, s.Length, reason)
			continue
		}
		mentions = append(mentions, mention)
		taken = mention.Offset + mention.Length
	}
	return body, mergeEntities(entities, mentions), dropped
}

// outgoing is one message on its way out: the source as typed, the text to
// send as UTF-16 units, the offset map between the two, and the Markdown
// entities already found.
type outgoing struct {
	src      []rune
	units    []uint16
	at       []int
	entities []tg.MessageEntityClass
}

// mentionEntity turns one span into its wire entity, or says why it cannot
// be sent. taken is where the previous mention ended on the wire. The
// checks that need no server run first, so a span that is going to be
// dropped anyway never costs a user lookup.
func (c *Client) mentionEntity(ctx context.Context, out *outgoing, s MentionSpan, taken int) (*tg.InputMessageEntityMentionName, string) {
	if s.Length <= 0 || s.Offset < 0 || s.Offset > len(out.src) || s.Length > len(out.src)-s.Offset {
		return nil, "outside the text, or empty"
	}
	start, end := out.at[s.Offset], out.at[s.Offset+s.Length]
	if string(utf16.Decode(out.units[start:end])) != string(out.src[s.Offset:s.Offset+s.Length]) {
		return nil, "its text did not survive Markdown intact"
	}
	if inCode(out.entities, start, end) {
		return nil, "inside code, which is literal"
	}
	if start < taken {
		return nil, "overlaps an earlier mention"
	}
	user, err := c.mentionedUser(ctx, s.UserID)
	if err != nil {
		return nil, "user cannot be resolved: " + err.Error()
	}
	return &tg.InputMessageEntityMentionName{
		Offset: start,
		Length: end - start,
		UserID: user,
	}, ""
}

// mentionedUser is the InputUser a mention of userID carries. It is built
// from the access hash the peer cache already holds, which the member
// search stored when it offered the user, so a mention costs no request.
// Only a user the cache does not know is looked up through the peers
// manager, and that is always a users.getUsers: gotd's manager keeps
// access hashes, not user objects, so it cannot answer from memory.
func (c *Client) mentionedUser(ctx context.Context, userID int64) (tg.InputUserClass, error) {
	if hasher := c.stores.userHasher(); hasher != nil {
		// The first ID names the account asking, which the peer cache's
		// key does not include (see peerUserHasher), so 0 serves.
		if hash, found, err := hasher.GetUserAccessHash(ctx, 0, userID); err == nil && found {
			return &tg.InputUser{UserID: userID, AccessHash: hash}, nil
		}
	}
	user, err := c.peers.ResolveUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	return user.InputUser(), nil
}

// inCode reports whether output units [start, end) touch a code or pre
// entity.
func inCode(entities []tg.MessageEntityClass, start, end int) bool {
	for _, e := range entities {
		switch e.(type) {
		case *tg.MessageEntityCode, *tg.MessageEntityPre:
			if start < e.GetOffset()+e.GetLength() && e.GetOffset() < end {
				return true
			}
		}
	}
	return false
}

// mergeEntities adds the mentions to the Markdown entities in offset order,
// the order the parser already emits its own in. The sort is stable, so at
// a shared offset the Markdown entity stays first; it is also never the
// shorter of the two, because a mention whose range held markup is dropped,
// so the outer entity always leads, as Telegram lists them.
func mergeEntities(markdown, mentions []tg.MessageEntityClass) []tg.MessageEntityClass {
	if len(mentions) == 0 {
		return markdown
	}
	all := append(slices.Clip(markdown), mentions...)
	slices.SortStableFunc(all, func(a, b tg.MessageEntityClass) int {
		return cmp.Compare(a.GetOffset(), b.GetOffset())
	})
	return all
}
