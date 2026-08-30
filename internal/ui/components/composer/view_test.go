package composer

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/imtaqin/telegram-cli/internal/ui/cell"
	"github.com/imtaqin/telegram-cli/internal/ui/theme"
	"github.com/muesli/termenv"
)

// TestMain pins a colour profile. lipgloss probes the output for a terminal,
// finds none under `go test`, and resolves to Ascii — Render becomes the
// identity function and every style disappears, so an assertion on styled
// output passes whatever the style was, including no style at all.
func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	os.Exit(m.Run())
}

func sized(t *testing.T, width int) Model {
	t.Helper()
	m := New(theme.ForName("dark"))
	m.SetSize(width, 1)
	m.SetChatId(1)
	return m
}

// rows splits a view and checks the two invariants every row of every panel
// has to hold: exactly the width, and no style left open at the end.
func rows(t *testing.T, m Model) []string {
	t.Helper()
	out := strings.Split(m.View(), "\n")
	if len(out) != m.Rows() {
		t.Fatalf("View drew %d rows, Rows() promised %d", len(out), m.Rows())
	}
	for i, line := range out {
		if got := cell.Width(line); got != m.width {
			t.Errorf("row %d is %d cells, want %d: %q", i, got, m.width, ansi.Strip(line))
		}
		if open := cell.OpenStyle(line); open != "" {
			t.Errorf("row %d leaves %q open", i, open)
		}
	}
	return out
}

// TestInlineRowMatchesTheGolden. Every frame fixture draws this row; it is
// the one surface the composer contributes to them.
func TestInlineRowMatchesTheGolden(t *testing.T) {
	m := sized(t, 61)
	m.SetParseMarkdown(true)

	got := ansi.Strip(m.View())
	want := " NORMAL › i to compose · : for commands                   md "
	if got != want {
		t.Errorf("inline row:\n want %q\n  got %q", want, got)
	}
}

// TestRowsAndViewAgreeAtEverySize. The rows the composer draws come out of
// the thread's budget, so a promise it does not keep leaves a hole under the
// history or pushes the bottom of it off screen.
func TestRowsAndViewAgreeAtEverySize(t *testing.T) {
	for width := 20; width <= 140; width += 7 {
		for _, tc := range []struct {
			name  string
			setup func(*Model)
		}{
			{"empty", func(m *Model) {}},
			{"focused", func(m *Model) { m.SetFocused(true) }},
			{"draft", func(m *Model) {
				m.SetFocused(true)
				m.textarea.Value = strings.Repeat("word ", 30)
				m.textarea.Cursor = m.textarea.Len()
			}},
			{"reply", func(m *Model) { m.EnterReplyMode(7, strings.Repeat("quoted ", 20)) }},
			{"attachment", func(m *Model) { m.SetAttachment("/tmp/a-long-file-name.png", true) }},
			{"reply and attachment", func(m *Model) {
				m.EnterReplyMode(7, "nadia: rebased")
				m.SetAttachment("/tmp/a.png", false)
			}},
			{"notice", func(m *Model) { m.SetNotice("⚠ something happened") }},
			{"expanded", func(m *Model) {
				m.SetFocused(true)
				m.SetExpanded(true)
				m.textarea.Value = "Ship **0.4.2** tonight?\n\n`deploy.sh`"
				m.textarea.Cursor = m.textarea.Len()
			}},
			{"expanded with attachment", func(m *Model) {
				m.SetExpanded(true)
				m.SetAttachment("/tmp/a.png", false)
			}},
			{"wide runes", func(m *Model) {
				m.SetFocused(true)
				m.textarea.Value = strings.Repeat("你好世界🎉", 12)
				m.textarea.Cursor = m.textarea.Len()
			}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				m := sized(t, width)
				tc.setup(&m)
				rows(t, m)
			})
		}
	}
}

// TestBadgeReportsWhatTheNextKeyWillDo. One glance has to answer "will this
// letter type or navigate" — the exit criterion for this whole phase.
func TestBadgeReportsWhatTheNextKeyWillDo(t *testing.T) {
	// Unfocused: another panel has the keyboard, and the host says so.
	m := sized(t, 61)
	if got := m.badge(); got != AppNormal {
		t.Errorf("unfocused badge = %v, want NORMAL", got)
	}

	// Focused emacs: the next letter is typed.
	m.SetFocused(true)
	if got := m.badge(); got != AppInsert {
		t.Errorf("focused emacs badge = %v, want INSERT", got)
	}

	// Focused vi in command state: the next letter is a command.
	vi := sized(t, 61)
	vi.SetEditingMode(ModeVi)
	vi.SetFocused(true)
	if got := vi.badge(); got != AppInsert {
		t.Errorf("vi insert badge = %v, want INSERT", got)
	}
	vi, _ = press(t, vi, "esc")
	if got := vi.badge(); got != AppNormal {
		t.Errorf("vi normal badge = %v, want NORMAL", got)
	}

	// The palette outranks everything, including a focused composer: it owns
	// the keyboard and only the host can see that it is up.
	m.SetMode(AppCommand)
	if got := m.badge(); got != AppCommand {
		t.Errorf("badge with the palette open = %v, want COMMAND", got)
	}
}

// TestBadgeDoesNotDependOnTheHostForWhatItCanSee. A component whose output
// depends on the host remembering a setter is a component with two
// behaviours; this one derives everything it can.
func TestBadgeDoesNotDependOnTheHostForWhatItCanSee(t *testing.T) {
	m := New(theme.ForName("dark"))
	m.SetFocused(true)
	if !strings.Contains(m.View(), "INSERT") {
		t.Errorf("a focused composer nobody told about reports:\n%s", ansi.Strip(m.View()))
	}
}

// TestRightLabelSaysWhatWillHappenToTheDraft: how long it is, or — while it
// is empty — whether markdown will be applied.
func TestRightLabelSaysWhatWillHappenToTheDraft(t *testing.T) {
	m := sized(t, 61)

	if got := m.rightLabel(); got != "" {
		t.Errorf("with markdown off and no draft: %q, want nothing", got)
	}

	m.SetParseMarkdown(true)
	if got := m.rightLabel(); got != "md" {
		t.Errorf("with markdown on and no draft: %q, want md", got)
	}

	m.textarea.Value = "hello"
	if got := m.rightLabel(); got != "5" {
		t.Errorf("with a draft: %q, want 5", got)
	}
}

// TestMultiLineDraftIsMarkedAsSuch. One row can only show one line, so it has
// to say there are others — otherwise a two-line draft looks like a one-line
// draft that lost its tail.
func TestMultiLineDraftIsMarkedAsSuch(t *testing.T) {
	m := sized(t, 61)
	m.SetFocused(true)

	m.textarea.Value = "one line"
	if strings.Contains(ansi.Strip(m.View()), "⌄") {
		t.Error("a one-line draft is marked as having more")
	}

	m.textarea.Value = "one\ntwo"
	m.textarea.Cursor = m.textarea.Len()
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "⌄") {
		t.Errorf("a two-line draft is not marked:\n%s", view)
	}
	if !strings.Contains(view, "two") {
		t.Errorf("the cursor's line is not the one shown:\n%s", view)
	}
}

// TestDraftLineFollowsTheCursor. Showing the head of a long line while the
// user types at its end would be showing them somebody else's text.
func TestDraftLineFollowsTheCursor(t *testing.T) {
	m := sized(t, 40)
	m.SetFocused(true)
	m.textarea.Value = "start " + strings.Repeat("middle ", 10) + "end"
	m.textarea.Cursor = m.textarea.Len()

	view := ansi.Strip(m.View())
	if !strings.Contains(view, "end") {
		t.Errorf("the cursor's end of the line is off screen:\n%s", view)
	}

	m.textarea.Cursor = 0
	if view := ansi.Strip(m.View()); !strings.Contains(view, "start") {
		t.Errorf("the cursor's start of the line is off screen:\n%s", view)
	}
}

// TestContextBarNamesTheMessageAndTheWayOut.
func TestContextBarNamesTheMessageAndTheWayOut(t *testing.T) {
	m := sized(t, 61)
	m.EnterReplyMode(7, "nadia: Rebased onto main, CI is green now")

	bar := ansi.Strip(rows(t, m)[0])
	for _, want := range []string{"reply", "↳", "nadia", "esc to drop"} {
		if !strings.Contains(bar, want) {
			t.Errorf("reply bar is missing %q: %q", want, bar)
		}
	}
}

// TestContextBarIsOneRowWhateverTheQuoteIs. A quote that wrapped would push
// the history up by however many lines somebody else's message happened to
// take.
func TestContextBarIsOneRowWhateverTheQuoteIs(t *testing.T) {
	m := sized(t, 40)
	m.EnterReplyMode(7, "line one\nline two\nline three "+strings.Repeat("and more ", 20))

	got := rows(t, m)
	if len(got) != 2 {
		t.Fatalf("expected a bar and a prompt row, got %d rows", len(got))
	}
	if strings.Contains(ansi.Strip(got[0]), "line two") {
		// Not a wrap — the newline was collapsed, so the second line would
		// have to have been drawn on the same row to appear at all. It is
		// truncated away instead.
		t.Logf("bar: %q", ansi.Strip(got[0]))
	}
}

// TestExpandedShowsSourceAndWhatWillBeSent, side by side, because the
// question a preview answers is "did that asterisk do what I meant" and
// answering it by scrolling between two views is how a message goes out
// wrong.
func TestExpandedShowsSourceAndWhatWillBeSent(t *testing.T) {
	m := sized(t, 70)
	m.SetSize(70, expandedRows)
	m.SetFocused(true)
	m.SetParseMarkdown(true)
	m.SetExpanded(true)
	m.textarea.Value = "Ship **0.4.2** tonight?"
	m.textarea.Cursor = m.textarea.Len()

	got := rows(t, m)
	joined := ansi.Strip(strings.Join(got, "\n"))

	if !strings.Contains(joined, "Ship **0.4.2** tonight?") {
		t.Errorf("the source is missing:\n%s", joined)
	}
	if !strings.Contains(joined, "Ship 0.4.2 tonight?") {
		t.Errorf("the preview does not show what will be sent:\n%s", joined)
	}
	if !strings.Contains(joined, "compose") || !strings.Contains(joined, "sends as") {
		t.Errorf("the columns are not labelled:\n%s", joined)
	}
}

// TestExpandedPreviewSaysWhenMarkdownIsOff. With parsing off the right-hand
// column is the text verbatim, and calling it a preview of formatting would
// promise something the setting has switched off.
func TestExpandedPreviewSaysWhenMarkdownIsOff(t *testing.T) {
	m := sized(t, 70)
	m.SetSize(70, expandedRows)
	m.SetExpanded(true)
	m.textarea.Value = "Ship **0.4.2** tonight?"

	joined := ansi.Strip(strings.Join(rows(t, m), "\n"))
	if !strings.Contains(joined, "markdown off") {
		t.Errorf("the preview does not say parsing is off:\n%s", joined)
	}
	if !strings.Contains(joined, "**0.4.2**") {
		t.Errorf("with parsing off the preview must be verbatim:\n%s", joined)
	}
}

// TestCtrlPTogglesTheExpandedForm, and reports it so the host can give the
// composer the rows it now needs.
func TestCtrlPTogglesTheExpandedForm(t *testing.T) {
	m := sized(t, 61)
	m.SetFocused(true)
	m.textarea.Value = "keep me"

	m, cmd := m.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	if !m.Expanded() {
		t.Fatal("ctrl+p did not expand")
	}
	if cmd == nil {
		t.Fatal("expanding did not tell the host its row count changed")
	}
	if _, ok := cmd().(ResizedMsg); !ok {
		t.Fatalf("expected a ResizedMsg, got %T", cmd())
	}
	if m.Rows() != expandedRows {
		t.Errorf("Rows = %d while expanded, want %d", m.Rows(), expandedRows)
	}

	m, _ = m.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	if m.Expanded() {
		t.Error("ctrl+p did not collapse again")
	}
	if m.textarea.Value != "keep me" {
		t.Errorf("the toggle lost the draft: %q", m.textarea.Value)
	}
}

// TestNoticeTakesTheRow. The inline composer has one row, so a notice has
// nowhere else to go — and it is the thing that just happened.
func TestNoticeTakesTheRow(t *testing.T) {
	m := sized(t, 61)
	m.SetFocused(true)
	m.textarea.Value = "a draft"
	m.SetNotice("⚠ no image in clipboard")

	view := ansi.Strip(m.View())
	if !strings.Contains(view, "no image in clipboard") {
		t.Errorf("notice missing:\n%s", view)
	}
	if !strings.Contains(view, "INSERT") {
		t.Errorf("the notice hid the mode badge:\n%s", view)
	}
}
