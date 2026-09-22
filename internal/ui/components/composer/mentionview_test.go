package composer

import (
	"strings"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/Ceesaxp/telegram-cli/internal/ui/cell"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// picker renders the picker and checks what every row of it must hold
// whatever it shows: exactly width cells, no style left open, and a surface
// painted the whole way across — it is drawn over the thread, and a cell
// left unpainted shows the thread through it.
func picker(t *testing.T, m Model, width, maxRows int) []string {
	t.Helper()
	lines, ok := m.MentionPicker(width, maxRows)
	if !ok {
		t.Fatalf("MentionPicker(%d, %d): nothing to paint", width, maxRows)
	}
	if len(lines) > maxRows {
		t.Fatalf("MentionPicker(%d, %d) drew %d rows", width, maxRows, len(lines))
	}
	for i, line := range lines {
		if got := cell.Width(line); got != width {
			t.Errorf("row %d is %d cells, want %d: %q", i, got, width, ansi.Strip(line))
		}
		if open := cell.OpenStyle(line); open != "" {
			t.Errorf("row %d leaves %q open", i, open)
		}
		if p := cell.PaintedWidth(line); p != width {
			t.Errorf("row %d: painted %d of %d cells", i, p, width)
		}
	}
	return lines
}

// plain is the rows with their styling stripped.
func plain(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = ansi.Strip(l)
	}
	return out
}

// openWith is a completion open on "@" with users as the local candidates.
func openWith(t *testing.T, users ...*telegram.User) Model {
	t.Helper()
	m := mentionComposer(t)
	m.SetMentionCandidates(42, users)
	return chars(t, m, "@")
}

func TestAClosedPickerPaintsNothing(t *testing.T) {
	if lines, ok := mentionComposer(t).MentionPicker(60, 5); ok || lines != nil {
		t.Errorf("MentionPicker = %q, %v with nothing open", lines, ok)
	}
}

// One row per match, best first: the name, and the @username to type.
func TestThePickerListsTheMatches(t *testing.T) {
	m := openWith(t, nadiaUser, olegUser)
	rows := plain(picker(t, m, 50, 5))

	if len(rows) != 2 {
		t.Fatalf("drew %d rows, want one per match:\n%s", len(rows), strings.Join(rows, "\n"))
	}
	if !strings.Contains(rows[0], "Nadia Petrova") || !strings.Contains(rows[0], "@nadia") {
		t.Errorf("row 0 = %q, want Nadia's name and username", rows[0])
	}
	if !strings.Contains(rows[1], "Oleg") || strings.Contains(rows[1], "@") {
		t.Errorf("row 1 = %q, want Oleg's name and no username", rows[1])
	}
}

// background is the escape sequence that puts colour behind a cell.
func background(c lipgloss.Color) string {
	styled := lipgloss.NewStyle().Background(c).Render("x")
	return styled[:strings.Index(styled, "x")]
}

// The selected row is marked twice over: a bar at its edge, which reads
// without colour, and the selection surface behind it, which reads at a
// glance. Neither is on any other row.
func TestTheSelectedRowStandsOut(t *testing.T) {
	m := openWith(t, nadiaUser, olegUser)
	m = typeSeq(t, m, keyDown)
	lines := picker(t, m, 50, 5)
	rows := plain(lines)

	if !strings.HasPrefix(rows[1], "▌") || strings.HasPrefix(rows[0], "▌") {
		t.Errorf("bars: %q, want one on the selected row only", rows)
	}
	sel := background(m.roles.Sel)
	if !strings.Contains(lines[1], sel) || strings.Contains(lines[0], sel) {
		t.Errorf("the selection surface is not on the selected row alone")
	}
}

// seven is more members than the picker shows: "Member 1" @m1 to "Member 7"
// @m7, in that order of recency.
func seven() []*telegram.User {
	var out []*telegram.User
	for i := range 7 {
		n := string(rune('1' + i))
		out = append(out, user(int64(i+1), "m"+n, "Member", n))
	}
	return out
}

// Five at most, however many match and however much room there is: the
// picker is for a glance, and a sixth row would be one more thing to read
// before the composer.
func TestThePickerShowsAtMostFive(t *testing.T) {
	if rows := picker(t, openWith(t, seven()...), 50, 10); len(rows) != 5 {
		t.Errorf("drew %d rows, want 5", len(rows))
	}
}

// The rows scroll with the selection, so the member Enter would insert is
// always one on screen.
func TestTheSelectionIsAlwaysOnScreen(t *testing.T) {
	m := openWith(t, seven()...)
	for range 6 {
		m = typeSeq(t, m, keyDown)
	}
	rows := plain(picker(t, m, 50, 5))
	last := rows[len(rows)-1]
	if !strings.HasPrefix(last, "▌") || !strings.Contains(last, "@m7") {
		t.Errorf("rows %q, want the selected @m7 on screen, last", rows)
	}
}

// On a short terminal the host offers fewer rows. The picker draws fewer
// rather than spill onto the composer — and still shows the selection.
func TestAShortBudgetDrawsFewerRows(t *testing.T) {
	m := openWith(t, seven()...)
	m = typeSeq(t, m, keyDown, keyDown, keyDown)

	rows := plain(picker(t, m, 50, 2))
	if len(rows) != 2 {
		t.Fatalf("drew %d rows in a budget of 2", len(rows))
	}
	if !strings.HasPrefix(rows[1], "▌") || !strings.Contains(rows[1], "@m4") {
		t.Errorf("rows %q, want the selected @m4 on screen", rows)
	}
}

// With nothing to offer yet, the picker says it is looking — and once the
// search has answered with nothing, says so. Either way there is a row on
// screen, which is what tells the reader Enter will close it rather than
// send.
func TestAnEmptyPickerSaysWhy(t *testing.T) {
	m, q := openAt(t, mentionComposer(t), "@zz")
	if rows := plain(picker(t, m, 40, 5)); len(rows) != 1 || !strings.Contains(rows[0], "searching") {
		t.Errorf("unanswered: %q, want one searching row", rows)
	}

	m, _ = m.Update(answer(q))
	if rows := plain(picker(t, m, 40, 5)); len(rows) != 1 || !strings.Contains(rows[0], "no matches") {
		t.Errorf("answered with nobody: %q, want one no-matches row", rows)
	}
}

// Waiting for the search adds no row under matches already on screen: the
// list would grow and shrink by one on every key.
func TestSearchingAddsNoRowUnderMatches(t *testing.T) {
	m := openWith(t, nadiaUser)
	if rows := plain(picker(t, m, 40, 5)); len(rows) != 1 {
		t.Errorf("rows %q, want the one match and nothing else", rows)
	}
}

// A failed search says so under the matches still on offer — when there is
// a row to spare. It never takes one a match could have had.
func TestAFailedSearchIsShownUnderTheMatches(t *testing.T) {
	m := mentionComposer(t)
	m.SetMentionCandidates(42, []*telegram.User{nadiaUser})
	m, q := openAt(t, m, "@na")
	res := answer(q)
	res.Err = errFlood
	m, _ = m.Update(res)

	rows := plain(picker(t, m, 40, 5))
	if len(rows) != 2 || !strings.Contains(rows[0], "@nadia") || !strings.Contains(rows[1], "failed") {
		t.Errorf("rows %q, want @nadia and then the failure", rows)
	}
	if rows = plain(picker(t, m, 40, 1)); len(rows) != 1 || !strings.Contains(rows[0], "@nadia") {
		t.Errorf("in one row: %q, want the match, not the failure", rows)
	}

	m = chars(t, mentionComposer(t), "@zz")
	m.mention.failed, m.mention.loading = true, false
	if rows = plain(picker(t, m, 40, 5)); len(rows) != 1 || !strings.Contains(rows[0], "failed") {
		t.Errorf("failed with nothing to offer: %q, want the failure", rows)
	}
}

// A member online now gets a filled mark, everyone else a hollow one — the
// same ● the contact list uses. A bot says it is one after its username.
func TestTheRowsMarkPresenceAndBots(t *testing.T) {
	online := user(1, "nadia", "Nadia", "")
	online.Status = &telegram.UserStatusOnline{Expires: 1}
	away := user(2, "oleg", "Oleg", "")
	away.Status = &telegram.UserStatusOffline{WasOnline: 1}
	bot := user(3, "nadia_support", "Nadia Support", "")
	bot.IsBot = true

	rows := plain(picker(t, openWith(t, online, away, bot), 50, 5))
	if []rune(rows[0])[1] != '●' || []rune(rows[1])[1] != '◦' {
		t.Errorf("marks: %q, want ● for the member online and ◦ for the other", rows[:2])
	}
	if !strings.Contains(rows[2], "@nadia_support bot") {
		t.Errorf("row 2 = %q, want the bot marked as one", rows[2])
	}
	if strings.Contains(rows[0], "bot") || strings.Contains(rows[1], "bot") {
		t.Errorf("rows %q: a person marked as a bot", rows[:2])
	}
}

// Every row is exactly the width at every width, whatever the names are
// made of: CJK is two cells a character, and a wide character cannot be cut
// in half to fit an odd cell.
func TestThePickerFitsAnyWidth(t *testing.T) {
	cjk := user(1, "wang_xiaoming", "王", "小明")
	emoji := user(2, "", "Ольга 🌸", "Кузнецова-Сидоренко")
	long := user(3, "a_very_long_username_indeed", "Al", "")
	bot := user(4, "helper_bot", "助手", "")
	bot.IsBot = true

	m := openWith(t, cjk, emoji, long, bot)
	for width := 1; width <= 60; width++ {
		picker(t, m, width, 5)
	}

	// And at a width where it all fits, the CJK name is there whole.
	if rows := plain(picker(t, m, 60, 5)); !strings.Contains(rows[0], "王 小明") {
		t.Errorf("row 0 = %q, want the CJK name whole", rows[0])
	}
}

// The picker is painted over the thread by the host, and the composer's own
// rows know nothing about it: opening and closing it must not change how
// many rows the composer asks for or a single cell of what it draws. That is
// what keeps the layout from jumping under the reader.
func TestThePickerLeavesTheComposerAlone(t *testing.T) {
	for _, expanded := range []bool{false, true} {
		with := mentionComposer(t)
		with.SetExpanded(expanded)
		with.SetMentionCandidates(42, []*telegram.User{nadiaUser, olegUser})
		with = chars(t, with, "hi @na")
		if !with.MentionActive() {
			t.Fatal("precondition: completion not open")
		}

		without := newFocused()
		without.SetExpanded(expanded)
		without = chars(t, without, "hi @na")

		closed, _ := send(t, with, keyEsc)
		for name, other := range map[string]Model{"closed": closed, "never enabled": without} {
			if with.Rows() != other.Rows() {
				t.Errorf("expanded=%v: Rows = %d open, %d %s", expanded, with.Rows(), other.Rows(), name)
			}
			if with.View() != other.View() {
				t.Errorf("expanded=%v: View differs open and %s:\n%s\n---\n%s",
					expanded, name, with.View(), other.View())
			}
		}
	}
}

// A member known only by a username — a user built from what a message
// carried — is shown by it, at the name's place.
func TestAMemberWithOnlyAUsernameShowsIt(t *testing.T) {
	rows := plain(picker(t, openWith(t, user(1, "nadia", "", "")), 30, 5))
	if !strings.HasPrefix(rows[0], "▌◦ @nadia") {
		t.Errorf("row 0 = %q, want the username where the name would be", rows[0])
	}
}
