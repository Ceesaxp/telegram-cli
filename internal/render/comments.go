package render

import (
	"strconv"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/Ceesaxp/telegram-cli/internal/ui/cell"
	"github.com/Ceesaxp/telegram-cli/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
)

// renderComments draws the discussion row under a channel post.
//
// A channel is a broadcast: nobody can answer a post in the channel itself,
// and the answers go to a group linked to it. A client that does not say so
// makes a channel look like a place where nothing can be said back — which
// is the opposite of what its author set up, and the reader has no way to
// find out they were wrong.
//
// Words rather than a glyph. The vocabulary of marks in this design is
// already spoken for — ↳ is a reply quote, ▪ a pin, ▹ a link — and a
// twelfth mark that has to be learned buys nothing over a sentence that can
// be read. It follows the code frame's "4 lines · y to yank": what there is,
// then the key that gets you to it.
func renderComments(comments *telegram.Comments, roles theme.Roles, width int) []string {
	if comments == nil || width < 1 {
		return nil
	}

	count := lipgloss.NewStyle().Foreground(roles.Fg)
	meta := lipgloss.NewStyle().Foreground(roles.Faint)
	fresh := lipgloss.NewStyle().Foreground(roles.Amber)

	head := count.Render(commentCount(comments.Count))
	if comments.Unread {
		// Amber, the same colour the unread divider uses, because it is
		// the same fact: there is something here you have not read.
		head = fresh.Render(commentCount(comments.Count) + " · new")
	}

	// The key is offered only where it leads somewhere. Telegram sometimes
	// reports a discussion without naming the group it is in, and a key
	// advertised on a row that cannot act is worse than a row that only
	// counts.
	line := head
	if comments.ChatID != 0 {
		line += meta.Render(" · t to open")
	}

	if cell.Width(line) > width {
		return []string{count.Render(cell.Truncate(commentCount(comments.Count), width))}
	}
	return []string{line}
}

// commentCount is the count in words, with zero spelled out: a discussion
// nobody has used yet is a real state, and "0 comments" reads as a broken
// counter where "no comments yet" reads as an invitation.
func commentCount(n int32) string {
	switch {
	case n <= 0:
		return "no comments yet"
	case n == 1:
		return "1 comment"
	default:
		return strconv.Itoa(int(n)) + " comments"
	}
}
