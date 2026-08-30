package render

import (
	"fmt"
	"strings"

	"github.com/imtaqin/telegram-cli/internal/config"
	"github.com/imtaqin/telegram-cli/internal/media"
	"github.com/imtaqin/telegram-cli/internal/store"
	"github.com/imtaqin/telegram-cli/internal/telegram"
	"github.com/imtaqin/telegram-cli/internal/ui/cell"
	"github.com/imtaqin/telegram-cli/internal/ui/theme"
)

// renderedContent is a message's content taken apart into the pieces that
// have to be laid out differently.
//
//   - art is pre-rendered raster or block art. It is a grid of cells at
//     fixed column positions and is only ever CROPPED, never wrapped: a
//     word wrapper run over it reflows the rows into noise.
//   - card is a metadata card, already exact-width.
//   - text is the message body or an attachment's caption, with its
//     entities intact, so blocks and inline spans can be rendered from it.
//   - note is plain text with no entities: service events, and the honest
//     placeholders for content this client cannot render.
type renderedContent struct {
	art  string
	card []string
	text *telegram.FormattedText
	note string
}

func (rc renderedContent) empty() bool {
	return rc.art == "" && len(rc.card) == 0 && rc.note == "" &&
		(rc.text == nil || rc.text.Text == "")
}

// plain wraps a note in a FormattedText so it can go through the same
// rendering path as a message body.
func plain(text string) *telegram.FormattedText {
	return &telegram.FormattedText{Text: text}
}

// MessageRenderer turns message content into terminal lines. It holds the
// image renderer and its cache; everything else it does is stateless.
type MessageRenderer struct {
	theme    *theme.Theme
	roles    theme.Roles
	imgCache *media.Cache
	imgRend  *media.ImageRenderer

	// inlineImages is the resolved ui.inline_images policy. Defaults to the
	// permissive one so a renderer built without config behaves as it did
	// before the setting existed.
	inlineImages string
}

func NewMessageRenderer(th *theme.Theme) *MessageRenderer {
	protocol := media.DetectProtocol()
	return &MessageRenderer{
		theme: th,
		// A default palette, not a zero one: a renderer whose output
		// depends on the host remembering to call SetRoles is a renderer
		// with two behaviours, and its own tests construct it directly.
		roles:        theme.DarkRoles(false),
		inlineImages: config.InlineImagesOnOpen,
		imgCache:     media.NewCache(50),
		imgRend:      media.NewImageRenderer(protocol, defaultImageCols, defaultImageRows),
	}
}

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
	r.imgRend = media.NewImageRenderer(protocol, maxCols, maxRows)
}

// BodyOptions is how a message is to be laid out for the thread grid.
type BodyOptions struct {
	// Width is the body column's width in display cells.
	Width int

	// RevealSpoilers un-hides spoiler spans. True only for the message
	// under the cursor, and only after x.
	RevealSpoilers bool
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
	out = append(out, rc.card...)
	if rc.note != "" {
		out = append(out, renderBlocks(plain(rc.note), r.roles, width, false)...)
	}
	if rc.text != nil && rc.text.Text != "" {
		out = append(out, renderBlocks(rc.text, r.roles, width, opts.RevealSpoilers)...)
	}
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

	// Anything with an attachment gets a card, except a photo whose art is
	// already downloaded: the picture is strictly more informative than a
	// description of it.
	if card, ok := mediaCardFor(content); ok {
		rc := renderedContent{text: captionOf(content)}
		if photo, isPhoto := content.(*telegram.MessagePhoto); isPhoto &&
			r.inlineImages != config.InlineImagesNever {
			if art, isArt := r.renderPhoto(photo.Photo, s); isArt {
				rc.art = art
				return rc
			}
		}
		rc.card = card.render(r.roles, width)
		return rc
	}

	switch c := content.(type) {
	case *telegram.MessageText:
		if c.Text == nil || c.Text.Text == "" {
			return renderedContent{note: "[empty]"}
		}
		return renderedContent{text: c.Text}

	case *telegram.MessageSticker:
		return renderedContent{note: c.Sticker.Emoji + " sticker"}

	case *telegram.MessageLocation:
		return renderedContent{note: fmt.Sprintf("location %.4f, %.4f",
			c.Location.Latitude, c.Location.Longitude)}

	case *telegram.MessageContact:
		return renderedContent{note: strings.TrimSpace(
			"contact " + c.Contact.FirstName + " " + c.Contact.LastName)}

	case *telegram.MessagePoll:
		// The question and nothing else. Telegram sends options, vote
		// counts and the closing time; the domain type carries none of
		// them, and a poll drawn with empty bars would state a result.
		// Recorded as a divergence — the goldens draw the full poll.
		return renderedContent{note: "poll · " + c.Poll.Question}

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
