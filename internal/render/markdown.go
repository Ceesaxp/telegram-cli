package render

import (
	"fmt"
	"strings"

	"github.com/Ceesaxp/telegram-cli/internal/config"
	"github.com/Ceesaxp/telegram-cli/internal/media"
	"github.com/Ceesaxp/telegram-cli/internal/store"
	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/Ceesaxp/telegram-cli/internal/ui/cell"
	"github.com/Ceesaxp/telegram-cli/internal/ui/theme"
)

// renderedContent is a message's content taken apart into the pieces that
// have to be laid out differently.
//
//   - art is pre-rendered raster or block art. It is a grid of cells at
//     fixed column positions and is only ever CROPPED, never wrapped: a
//     word wrapper run over it reflows the rows into noise.
//   - blocks are already-laid-out lines drawn UNDER the text: a metadata
//     card, or a poll's question, options and footer. Under, because a
//     caption is what the sender wrote and the card is what this client
//     worked out about the file — and the sentence introducing an
//     attachment reads as a caption above it and as an afterthought below.
//   - text is the message body or an attachment's caption, with its
//     entities intact, so blocks and inline spans can be rendered from it.
//   - note is plain text with no entities: service events, and the honest
//     placeholders for content this client cannot render.
//   - trailer is already-laid-out lines drawn BELOW the text: a link
//     preview, which is a second reading of something the text already
//     says and so cannot precede it.
type renderedContent struct {
	art     string
	blocks  []string
	text    *telegram.FormattedText
	note    string
	trailer []string
}

func (rc renderedContent) empty() bool {
	return rc.art == "" && len(rc.blocks) == 0 && rc.note == "" &&
		len(rc.trailer) == 0 && (rc.text == nil || rc.text.Text == "")
}

// plain wraps a note in a FormattedText so it can go through the same
// rendering path as a message body.
func plain(text string) *telegram.FormattedText {
	return &telegram.FormattedText{Text: text}
}

// MessageRenderer turns message content into terminal lines. It holds the
// image renderer and its cache; everything else it does is stateless.
type MessageRenderer struct {
	roles    theme.Roles
	imgCache *media.Cache
	imgRend  *media.ImageRenderer

	// hyperlinks is whether link runs are wrapped in OSC 8. Resolved once
	// at startup from ui.hyperlinks and the environment (see
	// theme.SupportsHyperlinks); never re-derived per render, and never
	// asked of the terminal.
	hyperlinks bool

	// inlineImages is the resolved ui.inline_images policy. Defaults to the
	// permissive one so a renderer built without config behaves as it did
	// before the setting existed.
	inlineImages string
}

func NewMessageRenderer() *MessageRenderer {
	protocol := media.DetectProtocol()
	return &MessageRenderer{
		// A default palette, not a zero one: a renderer whose output
		// depends on the host remembering to call SetRoles is a renderer
		// with two behaviours, and its own tests construct it directly.
		roles:        theme.DarkRoles(false),
		inlineImages: config.InlineImagesOnOpen,
		imgCache:     media.NewCache(50),
		imgRend:      inlineRenderer(protocol, defaultImageCols, defaultImageRows),
	}
}

// SetHyperlinks turns OSC 8 hyperlinks on the message body on or off. The
// caller resolves the policy and the terminal's capability together, once,
// so the renderer never has to.
func (r *MessageRenderer) SetHyperlinks(on bool) {
	if r == nil {
		return
	}
	r.hyperlinks = on
}

// inlineArtRows is the height of the "always" preview, from the design
// record: "Always may use an eight-row card preview."
//
// A cap rather than a suggestion. It is what keeps a photo's height a
// property of the setting instead of a property of the image, which is what
// the chat view's line index needs to stay usable.
const inlineArtRows = 8

// SetInlineImages sets the resolved ui.inline_images policy: whether a photo
// is drawn as art or as a metadata card.
//
// "never" is the setting that matters. A terminal whose image support was
// guessed wrong turns every photo into a screenful of garbage, and the user
// needs a way to say so that does not also turn off downloading.
func (r *MessageRenderer) SetInlineImages(policy string) {
	if r == nil {
		return
	}
	r.inlineImages = config.ResolveInlineImages(policy)
}

// SetRoles supplies the TUI 2.0 semantic palette used for entity styling,
// code frames, quotes and media cards.
func (r *MessageRenderer) SetRoles(roles theme.Roles) {
	if r == nil {
		return
	}
	r.roles = roles
}

const (
	defaultImageCols = 50
	defaultImageRows = 25
)

// SetImageProtocol replaces the image renderer used for photo bubbles.
// Zero cols/rows fall back to the historical 50x25 bubble size.
func (r *MessageRenderer) SetImageProtocol(protocol media.Protocol, maxCols, maxRows int) {
	if r == nil {
		return
	}
	if maxCols <= 0 {
		maxCols = defaultImageCols
	}
	if maxRows <= 0 {
		maxRows = defaultImageRows
	}
	r.imgRend = inlineRenderer(protocol, maxCols, maxRows)
}

// inlineRenderer builds the image renderer for art drawn INSIDE the history,
// with the row bound applied.
//
// One constructor for every path that makes one, so the bound cannot be
// applied on the configured path and missed on the default. It is capped
// rather than clamped afterwards because the renderer scales to its budget:
// give it eight rows and it returns a whole picture eight rows tall, where
// cutting twenty-five rows down to eight would return the top third of one.
//
// media.max_image_height is deliberately overridden rather than honoured.
// That setting sizes the picture a user OPENS; it must not size one sitting
// inside the history, because the height of a message in the history is what
// the chat view's line index is made of.
func inlineRenderer(protocol media.Protocol, maxCols, maxRows int) *media.ImageRenderer {
	if maxRows > inlineArtRows {
		maxRows = inlineArtRows
	}
	return media.NewImageRenderer(protocol, maxCols, maxRows)
}

// BodyOptions is how a message is to be laid out for the thread grid.
type BodyOptions struct {
	// Width is the body column's width in display cells.
	Width int

	// RevealSpoilers un-hides spoiler spans. True only for the message
	// under the cursor, and only after x.
	RevealSpoilers bool

	// ArmedLinkLo/ArmedLinkHi are the rune offsets of the link the reader
	// has armed with gx, from [Link.Lo] and [Link.Hi]. Set only for the
	// message under the cursor, the same as RevealSpoilers.
	//
	// Hi <= Lo means nothing is armed.
	ArmedLinkLo, ArmedLinkHi int
}

// RenderBody lays a message's content out for the TUI 2.0 thread grid's
// body column: one string per terminal line, with no frame of its own. The
// grid owns everything to the left of the body column, so this returns
// content and nothing else.
//
// The result is never empty — a message with nothing renderable still
// occupies one line, because a zero-line message would make the scroll
// index disagree with what is on screen.
func (r *MessageRenderer) RenderBody(msg *telegram.Message, s *store.Store, opts BodyOptions) []string {
	width := opts.Width
	if msg == nil || width < 1 {
		return []string{""}
	}

	rc := r.renderContent(msg.Content, s, width)
	if rc.empty() {
		rc.note = "[empty]"
	}

	var out []string
	if rc.art != "" {
		for _, line := range strings.Split(rc.art, "\n") {
			out = append(out, cell.Clamp(line, width))
		}
	}
	if rc.note != "" {
		// A note is this client's own words about the message — "poll ·",
		// "[unsupported]" — so it carries no entities and nothing to link.
		out = append(out, renderBlocks(plain(rc.note), r.roles, width, textOpts{})...)
	}
	if rc.text != nil && rc.text.Text != "" {
		out = append(out, renderBlocks(rc.text, r.roles, width, textOpts{
			reveal:  opts.RevealSpoilers,
			links:   r.hyperlinks,
			armedLo: opts.ArmedLinkLo,
			armedHi: opts.ArmedLinkHi,
		})...)
	}
	out = append(out, rc.blocks...)
	out = append(out, rc.trailer...)

	// The discussion under a channel post, then the reactions. Both belong
	// to the MESSAGE rather than to its content — a photo and a poll are
	// reacted to the same way — so both are appended once here rather than
	// by every branch of renderContent.
	//
	// Comments above reactions: the comments row is a way OUT of this
	// message and the chips are a fact about it, and a row that leads
	// somewhere reads as the end of the block.
	out = append(out, renderComments(msg.Comments, r.roles, width)...)
	out = append(out, renderReactions(msg.Reactions, r.roles, width)...)

	if len(out) == 0 {
		return []string{""}
	}
	return out
}

// SenderName is the display name of a message's sender, resolved through
// the store. It is exported because the thread grid gives the sender its
// own column and therefore has to measure the name before drawing it.
func SenderName(msg *telegram.Message, s *store.Store) string {
	switch sender := msg.SenderID.(type) {
	case *telegram.MessageSenderUser:
		name := s.Users.DisplayName(sender.UserID)
		if name != "Unknown" {
			return name
		}
		// Fallback: use first/last from message if available
		return fmt.Sprintf("User#%d", sender.UserID)
	case *telegram.MessageSenderChat:
		if entry, ok := s.Chats.Get(sender.ChatID); ok && entry.Chat != nil {
			return entry.Chat.Title
		}
		return fmt.Sprintf("Chat#%d", sender.ChatID)
	}
	return ""
}

// renderContent takes a message's payload apart into the pieces RenderBody
// lays out. It renders the attachment, if there is one, and hands back the
// text with its entities intact rather than a styled string — the block
// splitter needs the entities, and it runs after this.
func (r *MessageRenderer) renderContent(content telegram.MessageContent, s *store.Store, width int) renderedContent {
	if content == nil {
		return renderedContent{note: "[unsupported]"}
	}

	// Anything with an attachment gets a card. A photo gets art INSTEAD of
	// the card only under ui.inline_images = "always", and even then only a
	// bounded preview.
	//
	// This used to draw full-size art for any photo whose thumbnail had been
	// downloaded, at both "on_open" and "always". Two things were wrong with
	// that. The design record gives "on_open" a different meaning — the art
	// appears when you OPEN the photo, in the full-pane overlay, which is
	// what enter now does — and it gives "always" an eight-row card, not an
	// arbitrarily tall one.
	//
	// The height is the part that matters. A message whose height changes
	// when a thumbnail lands invalidates the chat view's line index under
	// the reader: scroll arithmetic and the }/{ motions are computed from
	// it, so a photo growing from one line to twenty makes the next motion
	// land somewhere unrelated. A bounded preview cannot do that, and a
	// card cannot either.
	if card, ok := mediaCardFor(content); ok {
		rc := renderedContent{text: captionOf(content)}
		if photo, isPhoto := content.(*telegram.MessagePhoto); isPhoto &&
			r.inlineImages == config.InlineImagesAlways {
			if art, isArt := r.renderPhoto(photo.Photo, s); isArt {
				rc.art = art
				return rc
			}
		}
		rc.blocks = card.render(r.roles, width)
		return rc
	}

	switch c := content.(type) {
	case *telegram.MessageText:
		preview := renderWebPage(c.WebPage, r.roles, width)
		if c.Text == nil || c.Text.Text == "" {
			if len(preview) == 0 {
				return renderedContent{note: "[empty]"}
			}
			return renderedContent{trailer: preview}
		}
		return renderedContent{text: c.Text, trailer: preview}

	case *telegram.MessageSticker:
		return renderedContent{note: c.Sticker.Emoji + " sticker"}

	case *telegram.MessageLocation:
		return renderedContent{note: fmt.Sprintf("location %.4f, %.4f",
			c.Location.Latitude, c.Location.Longitude)}

	case *telegram.MessageContact:
		return renderedContent{note: strings.TrimSpace(
			"contact " + c.Contact.FirstName + " " + c.Contact.LastName)}

	case *telegram.MessagePoll:
		if c.Poll == nil {
			return renderedContent{note: "[empty poll]"}
		}
		return renderedContent{blocks: renderPoll(c.Poll, r.roles, width)}

	case *telegram.MessagePinMessage:
		return renderedContent{note: "pinned a message"}
	case *telegram.MessageChatAddMembers:
		return renderedContent{note: "members added"}
	case *telegram.MessageChatDeleteMember:
		return renderedContent{note: "member left"}
	case *telegram.MessageChatChangeTitle:
		return renderedContent{note: "renamed the chat to " + c.Title}
	case *telegram.MessageChatChangePhoto:
		return renderedContent{note: "changed the chat photo"}
	case *telegram.MessageChatJoinByLink:
		return renderedContent{note: "joined via invite link"}
	case *telegram.MessageUnsupported:
		return renderedContent{note: fmt.Sprintf("[%s]", c.Type)}
	default:
		return renderedContent{note: "[unsupported]"}
	}
}

// captionOf is an attachment's caption, or nil when it has none.
func captionOf(content telegram.MessageContent) *telegram.FormattedText {
	switch c := content.(type) {
	case *telegram.MessagePhoto:
		return c.Caption
	case *telegram.MessageVideo:
		return c.Caption
	case *telegram.MessageDocument:
		return c.Caption
	case *telegram.MessageVoiceNote:
		return c.Caption
	case *telegram.MessageAudio:
		// Audio carries a caption too. Omitted here for the same reason
		// animation was: this switch was written from the media-card list
		// rather than from the types that actually have the field.
		return c.Caption
	case *telegram.MessageAnimation:
		// mediaCardFor draws a card for an animation, and this did not
		// return its caption — so a GIF sent WITH something written under
		// it drew the card and dropped the words. Found while listing a
		// message's links, which reads captions through here too.
		return c.Caption
	}
	return nil
}

// renderPhoto returns the rendered representation of a photo, plus whether
// that representation is real pre-rendered raster/block art (true) as
// opposed to a plain-text placeholder like "— enter to open" (false). The
// caller must never word-wrap art — only plain text.
func (r *MessageRenderer) renderPhoto(photo *telegram.Photo, s *store.Store) (string, bool) {
	if photo == nil || len(photo.Sizes) == 0 {
		return "🖼  [Photo]", false
	}

	// Try to find a downloaded file in the photo sizes
	// Check smallest first (thumbnail), then larger
	for _, size := range photo.Sizes {
		if size.File == nil {
			continue
		}

		path := ""
		if fileState, ok := s.Files.Get(size.File.ID); ok && fileState.IsComplete {
			path = fileState.LocalPath
		} else if size.File.Downloaded {
			path = size.File.Path
		}
		if path == "" {
			continue
		}

		// Check cache first
		cacheKey := fmt.Sprintf("img:%s", size.File.ID)
		if cached, ok := r.imgCache.Get(cacheKey); ok {
			return cached, true
		}
		// Render image from local file
		rendered, err := r.imgRend.RenderFile(path)
		if err == nil && rendered != "" {
			r.imgCache.Set(cacheKey, rendered)
			return rendered, true
		}
	}

	// Not downloaded yet — show placeholder with size info
	best := photo.Sizes[len(photo.Sizes)-1]
	return fmt.Sprintf("🖼  [Photo %dx%d] — enter to open", best.Width, best.Height), false
}

func fmtDur(s int32) string {
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// fmtSize is a byte count at the precision a reader actually uses it at.
// Kilobytes are whole numbers — nobody decides anything on the strength of
// 184.3 versus 184 KB — and the larger units keep one decimal, where the
// difference between 1.2 and 1.9 GB is the difference between downloading
// it and not.
func fmtSize(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%d KB", b/(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
