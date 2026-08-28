package render

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/glamour"
	glamourstyles "github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/imtaqin/telegram-cli/internal/media"
	"github.com/imtaqin/telegram-cli/internal/store"
	"github.com/imtaqin/telegram-cli/internal/telegram"
	"github.com/imtaqin/telegram-cli/internal/ui/theme"
)

// defaultGlamourWrap is the word-wrap width the glamour renderer is built
// with before the first call to RenderMessage supplies a real bubble width.
const defaultGlamourWrap = 80

// maxGlamourWrap caps how wide glamour will ever word-wrap code blocks to,
// even if the chat panel is very wide.
const maxGlamourWrap = 100

// bubbleFrameStyle mirrors the border+padding used by the actual message
// bubble styles below. It exists only so we can ask lipgloss for the exact
// horizontal frame size (border + padding) instead of hard-coding it.
var bubbleFrameStyle = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	PaddingLeft(1).PaddingRight(1)

// bubbleWidth computes the outer bubble width for a given panel maxWidth.
// It's a small named function (rather than inlined math duplicated at each
// call site, including tests) so RenderMessage and its tests always agree
// on what "the bubble width" means.
func bubbleWidth(maxWidth int) int {
	w := maxWidth * 65 / 100
	if w < 15 {
		w = 15
	}
	return w
}

// contentInnerWidth returns the usable content width inside a bubble of the
// given outer width, accounting for the real border+padding frame size.
func contentInnerWidth(bubbleW int) int {
	w := bubbleW - bubbleFrameStyle.GetHorizontalFrameSize()
	if w < 1 {
		w = 1
	}
	return w
}

// renderedContent separates a message's textual content (which should be
// word-wrapped to the bubble width) from any pre-rendered raster/block art
// (which must never be re-flowed — art is only ever cropped by lipgloss's
// MaxWidth safety net, never wrapped, or it comes out scrambled).
type renderedContent struct {
	text string
	art  string
}

func (rc renderedContent) empty() bool {
	return rc.text == "" && rc.art == ""
}

// glamourStyleName picks glamour's built-in "dark" or "light" standard
// style for th, without ever asking glamour.WithAutoStyle() to do so.
//
// WithAutoStyle() resolves to termenv.HasDarkBackground(), which queries the
// terminal by writing an OSC 11 "what's your background color?" escape
// sequence and then reading the reply off stdin. Under Bubble Tea's raw-mode
// input loop that reply (a fragment like ";1rgb:2020/2020/2020") has nowhere
// else to go — it gets delivered to the program as regular keystrokes and
// ends up typed into whatever's focused (the composer, on chat open). See
// glamourStyle's two call sites for where this bites.
//
// theme.Theme has no explicit dark/light flag, and we don't own that
// package here, so this compares against theme.LightTheme()'s canonical
// Background color — the only two Theme values ever constructed at runtime
// are theme.DarkTheme() and theme.LightTheme() (see theme.ForName), and
// LightTheme() is the only one that sets this particular Background, so the
// comparison is exact for how the app actually builds a Theme today. If
// that ever stops holding (theme starts exposing its own dark/light flag,
// or gains more variants), default to dark — the app's built-in default is
// dark — and thread the real signal through instead of extending this.
func glamourStyleName(th *theme.Theme) string {
	if th != nil && th.Background == theme.LightTheme().Background {
		return glamourstyles.LightStyle
	}
	return glamourstyles.DarkStyle
}

type MessageRenderer struct {
	theme     *theme.Theme
	glamour   *glamour.TermRenderer
	wrapWidth int // word-wrap width the current glamour renderer was built with
	imgCache  *media.Cache
	imgRend   *media.ImageRenderer
}

func NewMessageRenderer(th *theme.Theme) *MessageRenderer {
	// Deterministic style, not WithAutoStyle() — see glamourStyleName's doc
	// comment for why WithAutoStyle() is forbidden in this codebase.
	r, _ := glamour.NewTermRenderer(
		glamour.WithStandardStyle(glamourStyleName(th)),
		glamour.WithWordWrap(defaultGlamourWrap),
	)
	protocol := media.DetectProtocol()
	return &MessageRenderer{
		theme:     th,
		glamour:   r,
		wrapWidth: defaultGlamourWrap,
		imgCache:  media.NewCache(50),
		imgRend:   media.NewImageRenderer(protocol, 50, 25),
	}
}

// ensureGlamourWidth rebuilds the glamour renderer only when the desired
// word-wrap width actually changed, so repeated calls with the same panel
// width are cheap.
func (r *MessageRenderer) ensureGlamourWidth(width int) {
	if width < 1 {
		width = 1
	}
	if r.glamour != nil && r.wrapWidth == width {
		return
	}
	// Deterministic style, not WithAutoStyle() — see glamourStyleName's doc
	// comment for why WithAutoStyle() is forbidden in this codebase.
	gr, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle(glamourStyleName(r.theme)),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return
	}
	r.glamour = gr
	r.wrapWidth = width
}

func (r *MessageRenderer) RenderMessage(msg *telegram.Message, s *store.Store, isOwn, isSelected bool, maxWidth int) string {
	if msg == nil {
		return ""
	}

	bubbleW := bubbleWidth(maxWidth)

	// innerWidth is the space available for content once the bubble's
	// border and padding are accounted for (verified against the style's
	// actual frame size rather than assumed).
	innerWidth := contentInnerWidth(bubbleW)

	r.ensureGlamourWidth(min(innerWidth, maxGlamourWrap))

	// Sender name
	senderName := r.getSenderName(msg, s)

	// Content
	rc := r.renderContent(msg.Content, s, innerWidth)
	if rc.empty() {
		rc.text = "[empty]"
	}

	timeStr := FormatTimestamp(msg.Date)

	var headerLines []string

	if msg.IsForwarded {
		headerLines = append(headerLines, lipgloss.NewStyle().Foreground(r.theme.TextMuted).Italic(true).Render("↪ Forwarded"))
	}

	if msg.ReplyToMessageID != 0 {
		headerLines = append(headerLines, lipgloss.NewStyle().Foreground(r.theme.Primary).Italic(true).Render(fmt.Sprintf("┃ reply #%d", msg.ReplyToMessageID)))
	}

	if !isOwn && senderName != "" {
		headerLines = append(headerLines, lipgloss.NewStyle().Foreground(r.theme.Accent).Bold(true).Render(senderName))
	}

	footer := lipgloss.NewStyle().Foreground(r.theme.TextMuted).Render(timeStr)
	if isOwn {
		if msg.ID == 0 {
			footer += " " + lipgloss.NewStyle().Foreground(r.theme.Warning).Render("⏳")
		} else {
			footer += " " + lipgloss.NewStyle().Foreground(r.theme.Success).Render("✓✓")
		}
	}

	// Real ANSI-aware wrapping to the bubble's inner width. ansi.Wrap word
	// wraps on whitespace but also hard-wraps any single unbroken token
	// (e.g. a long URL or a wall of text with no spaces) that would
	// otherwise overflow the line on its own, and it preserves the SGR
	// escape codes emitted by EntitiesToANSI/glamour across the wrap
	// points. lipgloss's MaxWidth below is kept only as a final safety
	// net.
	//
	// Pre-rendered image/block art (rc.art) is deliberately kept OUT of
	// this wrap: art is a grid of cells at fixed column positions, and
	// running a text wrapper over it reflows/scrambles the rows instead
	// of cropping them cleanly. Art is only ever cropped, never wrapped —
	// that's what the MaxWidth safety net below is for.
	var blocks []string
	if len(headerLines) > 0 {
		blocks = append(blocks, ansi.Wrap(strings.Join(headerLines, "\n"), innerWidth, ""))
	}
	if rc.art != "" {
		blocks = append(blocks, rc.art)
		if rc.text != "" {
			blocks = append(blocks, ansi.Wrap(rc.text, innerWidth, ""))
		}
	} else {
		blocks = append(blocks, ansi.Wrap(rc.text, innerWidth, ""))
	}
	blocks = append(blocks, ansi.Wrap(footer, innerWidth, ""))

	inner := strings.Join(blocks, "\n")

	var bubble string
	if isOwn {
		style := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("39")).
			Foreground(lipgloss.Color("252")).
			PaddingLeft(1).PaddingRight(1).
			MaxWidth(bubbleW)
		if isSelected {
			style = style.BorderForeground(lipgloss.Color("214"))
		}
		bubble = style.Render(inner)

		w := lipgloss.Width(bubble)
		pad := maxWidth - w
		if pad > 0 {
			var padded []string
			for _, line := range strings.Split(bubble, "\n") {
				padded = append(padded, strings.Repeat(" ", pad)+line)
			}
			bubble = strings.Join(padded, "\n")
		}
	} else {
		style := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("245")).
			Foreground(lipgloss.Color("252")).
			PaddingLeft(1).PaddingRight(1).
			MaxWidth(bubbleW)
		if isSelected {
			style = style.BorderForeground(lipgloss.Color("214"))
		}
		bubble = style.Render(inner)
	}

	return bubble
}

func (r *MessageRenderer) getSenderName(msg *telegram.Message, s *store.Store) string {
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

func (r *MessageRenderer) renderContent(content telegram.MessageContent, s *store.Store, maxWidth int) renderedContent {
	if content == nil {
		return renderedContent{text: "[unsupported]"}
	}

	switch c := content.(type) {
	case *telegram.MessageText:
		if c.Text == nil || c.Text.Text == "" {
			return renderedContent{text: "[empty]"}
		}
		md := EntitiesToMarkdown(c.Text)
		if r.glamour != nil && strings.Contains(md, "```") {
			rendered, err := r.glamour.Render(md)
			if err == nil {
				return renderedContent{text: strings.TrimSpace(rendered)}
			}
		}
		text := EntitiesToANSI(c.Text)
		if maxWidth > 0 {
			// Wrap here too (not just the final bubble-level wrap in
			// RenderMessage) so this function honors the maxWidth it is
			// given on its own. Wrapping twice at the same width is a
			// no-op for already-wrapped text, so this is safe.
			text = ansi.Wrap(text, maxWidth, "")
		}
		return renderedContent{text: text}

	case *telegram.MessagePhoto:
		art, isArt := r.renderPhoto(c.Photo, s)
		caption := ""
		if c.Caption != nil && c.Caption.Text != "" {
			caption = c.Caption.Text
		}
		if isArt {
			// art is pre-rendered block/raster art at fixed cell
			// positions: it must never be re-wrapped, only cropped.
			// The caption (plain text) is fine to wrap normally.
			return renderedContent{art: art, text: caption}
		}
		// Not real art yet (placeholder/"not downloaded" text) — treat as
		// ordinary wrappable text, same as before.
		text := art
		if caption != "" {
			text += "\n" + caption
		}
		return renderedContent{text: text}

	case *telegram.MessageVideo:
		s := fmt.Sprintf("🎥 Video [%s]", fmtDur(c.Video.Duration))
		if c.Caption != nil && c.Caption.Text != "" {
			s += "\n" + c.Caption.Text
		}
		return renderedContent{text: s}

	case *telegram.MessageDocument:
		s := fmt.Sprintf("📎 %s (%s)", c.Document.FileName, fmtSize(c.Document.File.Size))
		if c.Caption != nil && c.Caption.Text != "" {
			s += "\n" + c.Caption.Text
		}
		return renderedContent{text: s}

	case *telegram.MessageVoiceNote:
		s := fmt.Sprintf("🎤 Voice [%s]", fmtDur(c.VoiceNote.Duration))
		if c.Caption != nil && c.Caption.Text != "" {
			s += "\n" + c.Caption.Text
		}
		return renderedContent{text: s}

	case *telegram.MessageVideoNote:
		return renderedContent{text: fmt.Sprintf("📹 Video msg [%s]", fmtDur(c.VideoNote.Duration))}

	case *telegram.MessageSticker:
		return renderedContent{text: c.Sticker.Emoji + " Sticker"}

	case *telegram.MessageAnimation:
		return renderedContent{text: "🎬 GIF"}

	case *telegram.MessageAudio:
		title := c.Audio.Title
		if title == "" {
			title = c.Audio.FileName
		}
		return renderedContent{text: fmt.Sprintf("🎵 %s [%s]", title, fmtDur(c.Audio.Duration))}

	case *telegram.MessageLocation:
		return renderedContent{text: fmt.Sprintf("📍 %.4f, %.4f", c.Location.Latitude, c.Location.Longitude)}

	case *telegram.MessageContact:
		return renderedContent{text: fmt.Sprintf("👤 %s %s", c.Contact.FirstName, c.Contact.LastName)}

	case *telegram.MessagePoll:
		return renderedContent{text: fmt.Sprintf("📊 %s", c.Poll.Question)}

	case *telegram.MessagePinMessage:
		return renderedContent{text: "📌 Pinned"}
	case *telegram.MessageChatAddMembers:
		return renderedContent{text: "➕ Members added"}
	case *telegram.MessageChatDeleteMember:
		return renderedContent{text: "➖ Member left"}
	case *telegram.MessageChatChangeTitle:
		return renderedContent{text: "✏ " + c.Title}
	case *telegram.MessageChatChangePhoto:
		return renderedContent{text: "🖼 Photo changed"}
	case *telegram.MessageChatJoinByLink:
		return renderedContent{text: "🔗 Joined via link"}
	case *telegram.MessageUnsupported:
		return renderedContent{text: fmt.Sprintf("[%s]", c.Type)}
	default:
		return renderedContent{text: "[unsupported]"}
	}
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

func fmtSize(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1fGB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1fKB", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%dB", b)
	}
}
