// Package mediaview is the full-pane image overlay: the thing `enter` opens
// on a photo, and `esc` closes again.
//
// It draws over the whole frame rather than inside the thread column, for
// the reason the design record gives — a photo shown at thread width is not
// shown, it is acknowledged — and it draws into the alternate screen the
// app already owns, so nothing it puts on the screen reaches the scrollback
// the user gets back on exit.
//
// # Nothing is emitted before an open
//
// The overlay renders no graphics sequence of any kind until [Model.Show]
// has been given a decoded file, and Show only runs after the key that asked
// for it. That is phase 8's exit criterion in the design record, and
// TestNoGraphicsBeforeAnOpen is what holds it: with `ui.inline_images` at
// its default a user who never presses enter never has an image protocol
// sequence written to their terminal by this component.
//
// # Cleaning up after a kitty image
//
// Sixel and half-block art are cell contents: the next frame overwrites
// them. A kitty image is not — it belongs to the terminal and outlives any
// number of text redraws, so closing the overlay has to say so explicitly.
// [Model.Close] returns the sequence that removes exactly this image, which
// the host emits on its next frame. Deleting by id matters: kitty reads a
// bare delete as "every placement on screen", which would take the thread's
// inline art with it.
package mediaview

import (
	"strings"

	"github.com/Ceesaxp/telegram-cli/internal/config"
	"github.com/Ceesaxp/telegram-cli/internal/media"
	"github.com/Ceesaxp/telegram-cli/internal/ui/cell"
	"github.com/Ceesaxp/telegram-cli/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
)

// chromeRows is the header and the hint row the art is inset between.
const chromeRows = 2

// Model is the overlay.
type Model struct {
	roles         theme.Roles
	width, height int

	open    bool
	caption string // what is being shown: "photo · nadia · 14:03"

	// art is the rendered image, already split into rows. status is what to
	// say instead when there is no art: "downloading…", or why not.
	art    []string
	status string

	// teardown is what the host must emit to remove the image currently on
	// screen. Held across the close so the host can put it in the next
	// frame, and cleared once it has.
	teardown string

	renderer *media.ImageRenderer
	protocol media.Protocol
	maxW     int
	maxH     int
}

func New(roles theme.Roles) Model {
	return Model{roles: roles, width: 80, height: 24}
}

// SetSize sets the overlay to the whole frame.
//
// The renderer is rebuilt on every resize rather than being told the new
// bounds, because an image already rendered at the old size cannot be
// re-fitted — it is a grid of cells or a blob of pixels by then, not a
// picture. Anything on screen is dropped for the same reason; the host
// re-shows it.
func (m *Model) SetSize(width, height int) {
	if width == m.width && height == m.height {
		return
	}
	m.width, m.height = width, height
	m.rebuildRenderer()
	m.art = nil
	if m.open && m.status == "" {
		m.status = "resized — press enter again"
	}
}

// ApplyMedia takes the [media] config: which protocol to draw with.
func (m *Model) ApplyMedia(cfg config.MediaConfig) {
	m.protocol = media.ResolveProtocol(cfg.ImageProtocol)
	m.rebuildRenderer()
}

func (m *Model) rebuildRenderer() {
	w, h := m.artWidth(), m.artHeight()
	if w < 1 || h < 1 {
		m.renderer = nil
		return
	}
	m.renderer = media.NewImageRenderer(m.protocol, w, h)
}

func (m Model) artWidth() int  { return m.width - 2 }
func (m Model) artHeight() int { return m.height - chromeRows }

// IsVisible reports whether the overlay owns the screen.
func (m Model) IsVisible() bool { return m.open }

// Open shows the overlay with a status line and no art yet. The caller
// starts the download; [Model.Show] finishes the job.
func (m *Model) Open(caption, status string) {
	m.open = true
	m.caption = caption
	m.status = status
	m.art = nil
}

// Show renders a downloaded file into the overlay.
//
// An error is kept as the status rather than returned: by the time this runs
// the overlay is already on screen, and the reader needs to be told why it is
// empty inside the thing they opened, not in a notice behind it.
func (m *Model) Show(path string) {
	// A download outlives the keypress that started it. Content delivered
	// to a dismissed overlay is refused here rather than by the host,
	// because this is where "dismissed" is known and where a test can see
	// the refusal.
	if !m.open {
		return
	}
	if m.renderer == nil {
		m.rebuildRenderer()
	}
	if m.renderer == nil {
		m.status = "the pane is too small to draw into"
		return
	}

	placement, err := m.renderer.PlaceFile(path)
	if err != nil {
		m.status = "cannot draw this image: " + err.Error()
		m.art = nil
		return
	}

	// A previous image, if any, goes now rather than at close: two kitty
	// placements in the same overlay would otherwise stack, and only the
	// newer id would ever be deleted.
	m.teardown += placement.Teardown
	m.art = strings.Split(placement.Art, "\n")
	m.status = ""
}

// Fail replaces the overlay's contents with a reason. The overlay stays up:
// the user asked for this photo, and closing the window they just opened is
// not an answer to "it did not download".
func (m *Model) Fail(reason string) {
	if !m.open {
		return
	}
	m.status = reason
	m.art = nil
}

// Close hides the overlay and returns the sequence the host must emit on its
// next frame to remove any image the terminal is holding.
//
// The sequence is returned rather than written, because this component has
// no output of its own: everything it draws goes through the host's View,
// and a component writing to stdout beside the renderer is how a frame gets
// torn in half.
func (m *Model) Close() string {
	m.open = false
	m.art = nil
	m.status = ""
	m.caption = ""

	out := m.teardown
	m.teardown = ""
	return out
}

// PendingTeardown is Close's sequence for a host that renders before its next
// Update — it drains, so a second call returns "".
func (m *Model) PendingTeardown() string {
	out := m.teardown
	m.teardown = ""
	return out
}

// View draws the overlay at exactly the frame size.
func (m Model) View() string {
	if !m.open || m.width < 1 || m.height < 1 {
		return ""
	}
	r := m.roles

	rows := make([]string, 0, m.height)
	rows = append(rows, m.header())

	body := m.height - chromeRows
	art := m.art
	if len(art) == 0 {
		art = m.statusRows(body)
	}
	for i := range body {
		line := ""
		if i < len(art) {
			line = " " + art[i]
		}
		rows = append(rows, cell.Fill(r.Bg, line, m.width))
	}

	rows = append(rows, m.hints())
	return strings.Join(rows, "\n")
}

// header names what is on screen. Without it a full-pane image is a picture
// with no indication of which message it came from.
func (m Model) header() string {
	r := m.roles
	line := " " +
		lipgloss.NewStyle().Foreground(r.Amber).Render("▣") + " " +
		lipgloss.NewStyle().Foreground(r.Bright).Bold(true).
			Render(cell.Truncate(m.caption, max(m.width-4, 1)))
	return cell.Fill(r.Panel, line, m.width)
}

// hints is the way out, always. An overlay that covers the whole screen and
// does not say how to leave is a trap, and this one can be reached by a
// single keystroke.
func (m Model) hints() string {
	r := m.roles
	key := lipgloss.NewStyle().Foreground(r.Cyan)
	label := lipgloss.NewStyle().Foreground(r.Faint)

	line := " " + key.Render("esc") + " " + label.Render("close") +
		"  " + key.Render("s") + " " + label.Render("save") +
		"  " + key.Render("o") + " " + label.Render("open externally")
	return cell.Fill(r.Chrome, line, m.width)
}

// statusRows centres the status text in the body, so a download in progress
// or a refusal reads as the content of the overlay rather than as a stray
// line at the top of an empty box.
func (m Model) statusRows(body int) []string {
	if m.status == "" || body < 1 {
		return nil
	}
	style := lipgloss.NewStyle().Foreground(m.roles.Dim)

	lines := cell.WrapLines(m.status, max(m.width-4, 1))
	out := make([]string, body)
	top := max((body-len(lines))/2, 0)
	for i, line := range lines {
		if top+i >= body {
			break
		}
		pad := max((m.width-2-cell.Width(line))/2, 0)
		out[top+i] = strings.Repeat(" ", pad) + style.Render(line)
	}
	return out
}
