package render

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/imtaqin/telegram-cli/internal/telegram"
	"github.com/imtaqin/telegram-cli/internal/ui/cell"
	"github.com/imtaqin/telegram-cli/internal/ui/theme"
)

// mediaCard is what is known about an attachment, in the order a reader
// wants it: what kind of thing it is, what it is called, and the facts that
// decide whether to open it.
//
// Every field is either present or omitted. Nothing here is inferred from a
// filename or filled with a plausible default: a card that guesses at a
// duration is worse than one that does not mention duration, because the
// reader cannot tell which fields were guessed.
type mediaCard struct {
	badge   string   // IMG, VID, DOC, AUD, GIF — three cells, so the box is fixed
	glyph   string   // the one-cell mark used by the collapsed form
	name    string   // filename, title, or a description of the kind
	facts   []string // dimensions, duration, size, extension
	actions string   // the keys that do something to this attachment
}

// cardBoxWidth is the badge box, "┌──────┐". Fixed, so the filenames of
// consecutive attachments line up down the thread.
const cardBoxWidth = 8

// cardGap separates the badge box from the text beside it.
const cardGap = 2

// mediaCardFor describes a message's attachment, or reports that it has
// none.
func mediaCardFor(content telegram.MessageContent) (mediaCard, bool) {
	switch c := content.(type) {
	case *telegram.MessagePhoto:
		card := mediaCard{badge: "IMG", glyph: "▣", name: "photo", actions: "enter open · s save"}
		if c.Photo != nil && len(c.Photo.Sizes) > 0 {
			best := c.Photo.Sizes[len(c.Photo.Sizes)-1]
			if best.Width > 0 && best.Height > 0 {
				card.facts = append(card.facts, fmt.Sprintf("%d×%d", best.Width, best.Height))
			}
			if best.File != nil && best.File.Size > 0 {
				card.facts = append(card.facts, fmtSize(best.File.Size))
			}
		}
		return card, true

	case *telegram.MessageVideo:
		card := mediaCard{badge: "VID", glyph: "▶", name: "video", actions: "enter play · s save"}
		if c.Video != nil {
			if c.Video.FileName != "" {
				card.name = c.Video.FileName
			}
			if c.Video.Width > 0 && c.Video.Height > 0 {
				card.facts = append(card.facts, fmt.Sprintf("%d×%d", c.Video.Width, c.Video.Height))
			}
			card.facts = append(card.facts, fmtDur(c.Video.Duration))
			if c.Video.File != nil && c.Video.File.Size > 0 {
				card.facts = append(card.facts, fmtSize(c.Video.File.Size))
			}
		}
		return card, true

	case *telegram.MessageAnimation:
		card := mediaCard{badge: "GIF", glyph: "▶", name: "animation", actions: "enter play · s save"}
		if c.Animation != nil {
			if c.Animation.FileName != "" {
				card.name = c.Animation.FileName
			}
			card.facts = append(card.facts, fmtDur(c.Animation.Duration))
			if c.Animation.File != nil && c.Animation.File.Size > 0 {
				card.facts = append(card.facts, fmtSize(c.Animation.File.Size))
			}
		}
		return card, true

	case *telegram.MessageDocument:
		card := mediaCard{badge: "DOC", glyph: "▤", name: "file", actions: "enter open · s save"}
		if c.Document != nil {
			if c.Document.FileName != "" {
				card.name = c.Document.FileName
			}
			if c.Document.File != nil && c.Document.File.Size > 0 {
				card.facts = append(card.facts, fmtSize(c.Document.File.Size))
			}
			if ext := extensionOf(c.Document.FileName); ext != "" {
				card.facts = append(card.facts, ext)
			}
		}
		return card, true

	case *telegram.MessageAudio:
		card := mediaCard{badge: "AUD", glyph: "♪", name: "audio", actions: "enter play · s save"}
		if c.Audio != nil {
			switch {
			case c.Audio.Title != "" && c.Audio.Performer != "":
				card.name = c.Audio.Performer + " — " + c.Audio.Title
			case c.Audio.Title != "":
				card.name = c.Audio.Title
			case c.Audio.FileName != "":
				card.name = c.Audio.FileName
			}
			card.facts = append(card.facts, fmtDur(c.Audio.Duration))
			if c.Audio.File != nil && c.Audio.File.Size > 0 {
				card.facts = append(card.facts, fmtSize(c.Audio.File.Size))
			}
		}
		return card, true

	case *telegram.MessageVoiceNote:
		// No waveform. The design record draws a 24-cell amplitude bar, and
		// there is no amplitude data on the domain type — a bar drawn from
		// nothing would be the one part of this card that looks like
		// measurement and is not. Duration is the fact that exists.
		card := mediaCard{badge: "AUD", glyph: "♪", name: "voice note", actions: "enter play · s save"}
		if c.VoiceNote != nil {
			card.facts = append(card.facts, fmtDur(c.VoiceNote.Duration))
			if c.VoiceNote.File != nil && c.VoiceNote.File.Size > 0 {
				card.facts = append(card.facts, fmtSize(c.VoiceNote.File.Size))
			}
		}
		return card, true

	case *telegram.MessageVideoNote:
		card := mediaCard{badge: "VID", glyph: "▶", name: "video message", actions: "enter play · s save"}
		if c.VideoNote != nil {
			card.facts = append(card.facts, fmtDur(c.VideoNote.Duration))
		}
		return card, true
	}
	return mediaCard{}, false
}

// badgeColour is the accent for a card: attachments are amber
// (docs/tui-2.0.md's role table names amber for attachments), and the badge
// is the only part of the card that carries it.
func badgeColour(roles theme.Roles) lipgloss.Color { return roles.Amber }

// render draws the card at a body width, framed when there is room for it
// and on one line when there is not.
func (c mediaCard) render(roles theme.Roles, width int) []string {
	if width < minCardWidth {
		return []string{c.collapsed(roles, width)}
	}
	return c.framed(roles, width)
}

// framed is the three-row card: a badge box, the name, the facts, and the
// keys that act on it.
//
//	┌──────┐  ▣ auth-p95-2608.png
//	│ IMG  │  1440×720 · 184 KB · png
//	└──────┘  enter open · s save
func (c mediaCard) framed(roles theme.Roles, width int) []string {
	border := lipgloss.NewStyle().Foreground(roles.Border)
	badge := lipgloss.NewStyle().Foreground(badgeColour(roles)).Bold(true)
	name := lipgloss.NewStyle().Foreground(roles.Fg)
	facts := lipgloss.NewStyle().Foreground(roles.Faint)
	keys := lipgloss.NewStyle().Foreground(roles.Cyan)

	textW := width - cardBoxWidth - cardGap
	if textW < 1 {
		textW = 1
	}
	gap := strings.Repeat(" ", cardGap)

	rows := []string{
		badge.Render(c.glyph) + " " + name.Render(cell.Truncate(c.name, textW-2)),
		facts.Render(cell.Truncate(strings.Join(c.facts, " · "), textW)),
		keys.Render(cell.Truncate(c.actions, textW)),
	}

	return []string{
		border.Render("┌──────┐") + gap + rows[0],
		border.Render("│ ") + badge.Render(c.badge) + border.Render("  │") + gap + rows[1],
		border.Render("└──────┘") + gap + rows[2],
	}
}

// collapsed is the one-line card for a narrow pane: the mark, the name, and
// the facts.
//
// The ACTIONS are what gets dropped, not the facts. A reader who cannot see
// "enter open" can still press enter and find out; a reader who cannot see
// the size has no way to learn it from this screen at all.
func (c mediaCard) collapsed(roles theme.Roles, width int) string {
	badge := lipgloss.NewStyle().Foreground(badgeColour(roles)).Bold(true)
	name := lipgloss.NewStyle().Foreground(roles.Fg)
	facts := lipgloss.NewStyle().Foreground(roles.Faint)

	tail := strings.Join(c.facts, " · ")
	tailW := cell.Width(tail)
	if tailW > 0 {
		tailW += 2 // the gap before it
	}

	nameW := width - 2 - tailW
	if nameW < 1 {
		nameW = 1
	}

	out := badge.Render(c.glyph) + " " + name.Render(cell.Truncate(c.name, nameW))
	if tail != "" {
		out += "  " + facts.Render(tail)
	}
	return cell.Clamp(out, width)
}

// extensionOf is a filename's extension, lower case and without the dot.
// It is read off the name rather than the MIME type because the name is
// what the reader will see if they save it.
func extensionOf(filename string) string {
	i := strings.LastIndexByte(filename, '.')
	if i < 0 || i == len(filename)-1 {
		return ""
	}
	ext := strings.ToLower(filename[i+1:])
	if len(ext) > 8 || strings.ContainsAny(ext, " /\\") {
		return ""
	}
	return ext
}
