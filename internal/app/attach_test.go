package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/imtaqin/telegram-cli/internal/ui/components/attach"
	"github.com/imtaqin/telegram-cli/internal/ui/components/composer"
)

// pickerModel is a model with a chat open and the attach picker up, pointed
// at a directory the test owns.
func pickerModel(t *testing.T, files ...string) (Model, string) {
	t.Helper()
	dir := t.TempDir()
	for _, name := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Through the configured download directory, which is the path the app
	// actually takes. Open's argument is a fallback rather than a
	// destination, so a second call would not move a picker that has
	// already been somewhere.
	// From the CHAT LIST, not the composer: focus has to have somewhere to
	// move from, or "attaching leaves you where the caption goes" is a
	// claim about a focus that never changed.
	m := openChatModel(t, PanelChatList)
	m.config.Storage.DownloadDir = dir

	next, _ := m.Update(composer.AttachRequestedMsg{})
	m, ok := next.(Model)
	if !ok {
		t.Fatalf("Update returned %T", next)
	}
	if !m.attach.IsVisible() {
		t.Fatal("the attach request did not open the picker")
	}
	return m, dir + "/"
}

// TestCtrlTOpensThePickerRatherThanADialog.
//
// The last centred box with an OK button in the client. It could not
// complete a path, and it could not tell you whether what you typed existed
// until it had already failed.
func TestCtrlTOpensThePickerRatherThanADialog(t *testing.T) {
	m, _ := pickerModel(t, "a.txt")

	if m.dialog != nil {
		t.Error("attaching still raises a dialog")
	}
	if !m.attach.IsVisible() {
		t.Error("the picker is not open")
	}
}

// TestThePickerOwnsTheKeyboard. Every overlay in this app does while it is
// up; one that let a key through would act on the panel behind it, invisibly.
func TestThePickerOwnsTheKeyboard(t *testing.T) {
	m, _ := pickerModel(t, "jk.txt")

	// j and k are chat-list movement everywhere else, and part of a
	// filename here.
	for _, seq := range []string{"j", "k"} {
		m = update(t, m, seq)
	}
	if !strings.HasSuffix(m.attach.Typed(), "jk") {
		t.Errorf("the path is %q — j and k did not reach the picker", m.attach.Typed())
	}
	if !m.attach.IsVisible() {
		t.Fatal("the picker closed on a printable key")
	}
}

// TestThePickerIsInsertNotCommand. Its printables build a path, which is
// what the mode badge has to report — the prompt dialog it replaced resolved
// the same way for the same reason.
func TestThePickerIsInsertNotCommand(t *testing.T) {
	m, _ := pickerModel(t, "a.txt")
	if got := m.Mode(); got != ModeInsert {
		t.Errorf("Mode() = %v with the picker open, want INSERT", got)
	}
}

// TestChoosingAFileStagesItAndLeavesYouWhereTheCaptionGoes.
func TestChoosingAFileStagesItAndLeavesYouWhereTheCaptionGoes(t *testing.T) {
	m, dir := pickerModel(t, "shot.png")

	m = update(t, m, "\r")
	if m.attach.IsVisible() {
		t.Error("the picker stayed open after attaching")
	}
	if got := m.composer.Attachment(); got != filepath.Join(dir, "shot.png") {
		t.Errorf("the composer holds %q, want the picked file", got)
	}
	if m.focus != PanelComposer {
		t.Errorf("focus is %v after attaching, want the composer", m.focus)
	}
	if got := m.Mode(); got != ModeInsert {
		t.Errorf("Mode() = %v after attaching, want INSERT — the caption is "+
			"the next thing to type", got)
	}

	// The send mode has to survive the handoff. The picker knowing an image
	// is an image is no use if the composer stages it as a document
	// anyway — which is exactly the defect this whole surface exists to fix.
	rows := m.composer.Rows()
	m.composer.SetSize(60, rows)
	if chip := ansi.Strip(m.composer.View()); !strings.Contains(chip, "▣") {
		t.Errorf("the composer staged the image as a document:\n%s", chip)
	}
}

// TestThePickerDoesNotExpandTheComposer.
//
// The spec asked for the expanded form so the staged chip would be visible
// while the caption is typed. It already is: the inline composer grows a row
// for the chip and draws it above the prompt, so expanding would spend eight
// rows of a twenty-four-row terminal showing something that is on screen
// either way. See divergence 50.
func TestThePickerDoesNotExpandTheComposer(t *testing.T) {
	m, _ := pickerModel(t, "shot.png")
	m = update(t, m, "\r")

	if m.composer.Expanded() {
		t.Error("attaching expanded the composer")
	}

	// Rows is what the layout budgets from, and it grew by one for the chip
	// — which is the whole reason expanding is unnecessary. View cuts from
	// the front to the height it was granted, so the chip is only on screen
	// once that row has actually been given to it.
	rows := m.composer.Rows()
	if rows < 2 {
		t.Fatalf("the composer wants %d rows with a file staged, want a row for the chip", rows)
	}
	m.composer.SetSize(60, rows)
	if view := m.composer.View(); !strings.Contains(view, "shot.png") {
		t.Errorf("the staged file is not visible in the inline composer:\n%s", view)
	}
}

// TestEscapeStagesNothing.
func TestEscapeStagesNothing(t *testing.T) {
	m, _ := pickerModel(t, "shot.png")

	m = update(t, m, "\x1b")
	if m.attach.IsVisible() {
		t.Error("esc did not close the picker")
	}
	if got := m.composer.Attachment(); got != "" {
		t.Errorf("esc staged %q", got)
	}
}

// TestAttachingIsStillRefusedDuringAnEdit — the guard the dialog had, which
// must survive its replacement. An edit cannot carry media, so a picker that
// opened would be collecting a path nothing could use.
func TestAttachingIsStillRefusedDuringAnEdit(t *testing.T) {
	m := openChatModel(t, PanelComposer)
	m.composer.EnterEditMode(42, "already sent")

	next, _ := m.Update(composer.AttachRequestedMsg{})
	m, ok := next.(Model)
	if !ok {
		t.Fatalf("Update returned %T", next)
	}
	if m.attach.IsVisible() {
		t.Error("the picker opened over an edit")
	}
}

// TestADroppedFileIsAttached, which is the gesture Ctrl+T exists to serve.
// A terminal delivers a drop as a paste of the path, not as keystrokes.
func TestADroppedFileIsAttached(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dropped.png")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("onto the picker", func(t *testing.T) {
		m, _ := pickerModel(t)
		next, _ := m.Update(tea.PasteMsg{Content: path})
		m = next.(Model)

		if !strings.HasSuffix(m.attach.Typed(), "dropped.png") {
			t.Errorf("the picker's path is %q after the drop", m.attach.Typed())
		}
	})

	t.Run("onto the chat, with no picker open", func(t *testing.T) {
		m := openChatModel(t, PanelComposer)
		next, _ := m.Update(tea.PasteMsg{Content: path})
		m = next.(Model)

		if got := m.composer.Attachment(); got != path {
			t.Errorf("the composer holds %q, want the dropped file", got)
		}
	})
}

// TestAPasteThatIsNotAPathIsStillAMessage.
//
// The rule that makes the drop safe: pasting prose into a chat must send
// prose. An attachment staged instead of a message is a silent substitution
// of one thing for another.
func TestAPasteThatIsNotAPathIsStillAMessage(t *testing.T) {
	m := openChatModel(t, PanelComposer)

	const prose = "have a look at /etc/hosts when you get a moment"
	next, _ := m.Update(tea.PasteMsg{Content: prose})
	m = next.(Model)

	if got := m.composer.Attachment(); got != "" {
		t.Errorf("pasting prose staged %q", got)
	}
	if !strings.Contains(m.composer.Draft(), prose) {
		t.Errorf("the pasted text did not reach the draft: %q", m.composer.Draft())
	}
}

// TestTheHelpCardDoesNotAdvertiseThePalettesRemovedChords.
//
// ctrl+p/ctrl+n were taken off the palette as a second spelling of the
// arrows — palette.TestTheEmacsChordsDoNotNavigate is what holds that — and
// this card went on listing them, which is a key you can only discover is
// inert by pressing it. Found while giving the picker its own section.
func TestTheHelpCardDoesNotAdvertiseThePalettesRemovedChords(t *testing.T) {
	m := mainModel(t, PanelChatList)

	for _, section := range m.helpSections() {
		if !strings.HasPrefix(section.Title, "Command palette") {
			continue
		}
		for _, binding := range section.Bindings {
			if strings.Contains(binding.Keys, "ctrl+p") || strings.Contains(binding.Keys, "ctrl+n") {
				t.Errorf("the palette's card advertises %q, which it does not honour", binding.Keys)
			}
		}
		return
	}
	t.Fatal("no command palette section in the help card")
}

// TestThePickerHasItsOwnHelpSection, since a surface with eight bindings
// that appears nowhere in the keymap is a surface people have to guess at.
func TestThePickerHasItsOwnHelpSection(t *testing.T) {
	m := mainModel(t, PanelChatList)

	for _, section := range m.helpSections() {
		if !strings.HasPrefix(section.Title, "Attach picker") {
			continue
		}
		keys := map[string]bool{}
		for _, binding := range section.Bindings {
			keys[binding.Keys] = true
		}
		for _, want := range []string{"enter", "tab", "backspace", "left", "ctrl+t", "esc"} {
			if !keys[want] {
				t.Errorf("the picker's card does not name %q", want)
			}
		}
		// The two the settled key list deliberately excludes.
		for _, forbidden := range []string{"ctrl+h", "ctrl+p"} {
			if keys[forbidden] {
				t.Errorf("the picker's card names %q, which is not bound", forbidden)
			}
		}
		return
	}
	t.Fatal("no attach picker section in the help card")
}

// TestThePickerIsDrawnOverTheFrame, and drawn where the palette is drawn so
// the chat it is attaching to stays visible underneath.
func TestThePickerIsDrawnOverTheFrame(t *testing.T) {
	m, _ := pickerModel(t, "unmistakable-name.png")
	m.width, m.height = 120, 40
	m.updateLayout()

	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "unmistakable-name.png") {
		t.Fatalf("the picker is not on screen:\n%s", view)
	}

	rows := strings.Split(view, "\n")
	for i, row := range rows {
		if strings.Contains(row, "unmistakable-name.png") {
			if i < 8 {
				t.Errorf("the picker starts at row %d, above the palette's anchor", i)
			}
			return
		}
	}
}

// TestThePickerIsAnOverlay, so the frame knows something is drawn over it.
func TestThePickerIsAnOverlay(t *testing.T) {
	m, _ := pickerModel(t, "a.txt")
	if !m.overlayOpen() {
		t.Error("the picker does not count as an overlay")
	}
}

// TestTheDropRuleIsTheComponentsOwn. The app must not grow a second reading
// of "is this a path", because two readings drift and the one that decides
// whether a message is sent is not the one to have two of.
func TestTheDropRuleIsTheComponentsOwn(t *testing.T) {
	source, err := os.ReadFile("app.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(source), "attach.LooksLikePath") {
		t.Error("the paste branch no longer asks the picker whether it is a path")
	}
	_ = attach.LooksLikePath("")
}
