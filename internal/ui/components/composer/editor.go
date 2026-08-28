package composer

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// External-editor escape hatch (ctrl+o).
//
// ctrl+o decodes cleanly and identically in every encoding the terminal may
// use — byte 0x0F and Kitty's CSI 111;5u both yield Keystroke "ctrl+o" — and
// nothing else in the app binds it (app.go claims only ctrl+c/q, ctrl+g and
// ctrl+v globally; chatview's ctrl+d/u/f are reached only when the chat view
// has focus).
//
// The program is suspended for the duration via tea.ExecProcess, which hands
// the terminal to the child and restores the alt screen and input modes when
// it exits. Its callback runs after the process finishes, so reading the file
// back and deleting it there means the temp file cannot outlive the edit.

// editorFinishedMsg carries the outcome of an external-editor session back
// into Update. It is package-private: app.go relays every non-key message to
// every component, and the composer is the only one that has any use for it.
type editorFinishedMsg struct {
	// text is the edited draft, only meaningful when ok is true.
	text string
	// ok reports that the editor exited cleanly and the file was read back.
	ok bool
	// err explains why ok is false.
	err error
}

// editorSession is a prepared external-editor invocation.
type editorSession struct {
	// cmd is the process to run under tea.ExecProcess.
	cmd *exec.Cmd
	// path is the temp file holding the draft, to be read back and deleted.
	path string
	// name is what to call the editor in the status notice.
	name string
}

// editInEditor writes the current draft to a temp file and returns a command
// that suspends the program while $VISUAL/$EDITOR edits it.
//
// Failures that happen before the editor launches (no editor configured, a
// temp file that cannot be written) set a notice and return a nil command —
// the draft is never touched, so there is nothing to roll back.
func (m *Model) editInEditor() tea.Cmd {
	s, err := m.prepareEditor()
	if err != nil {
		m.notice = err.Error()
		return nil
	}
	m.notice = fmt.Sprintf("editing in %s...", s.name)
	return tea.ExecProcess(s.cmd, editorResult(s.path))
}

// prepareEditor resolves the editor command and spools the draft to a file.
// It is separate from editInEditor so tests can run the command directly
// instead of going through the Bubble Tea runtime.
func (m Model) prepareEditor() (editorSession, error) {
	// $VISUAL wins over $EDITOR: that is the whole point of the two
	// variables — $VISUAL names the full-screen editor.
	name := strings.TrimSpace(os.Getenv("VISUAL"))
	if name == "" {
		name = strings.TrimSpace(os.Getenv("EDITOR"))
	}
	if name == "" {
		return editorSession{}, fmt.Errorf("%s", noticeNoEditor)
	}

	f, err := os.CreateTemp("", "teletui-compose-*.md")
	if err != nil {
		return editorSession{}, fmt.Errorf("⚠ editor: %v", err)
	}
	path := f.Name()
	if _, err := f.WriteString(m.textarea.Value); err != nil {
		f.Close()
		os.Remove(path)
		return editorSession{}, fmt.Errorf("⚠ editor: %v", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return editorSession{}, fmt.Errorf("⚠ editor: %v", err)
	}

	cmd, display := editorCommand(name, path)
	return editorSession{cmd: cmd, path: path, name: display}, nil
}

// editorCommand builds the process that edits path, and the name to show
// while it runs.
//
// $EDITOR is a *shell command line*, not a filename: it routinely carries
// flags ("code -w", "nvim -u NONE") and may carry quoting, so it is handed to
// sh with the file appended as a positional parameter — the same thing git
// does. That means a path containing spaces has to be quoted by the user
// (EDITOR='"/Applications/Sublime Text/subl" -w'), exactly as it does for git;
// an unquoted one would be word-split by the shell.
//
// The one case the shell cannot get right on its own is an unquoted bare path
// with spaces and no flags, which is a plausible thing to find in a $EDITOR
// and unambiguous to interpret. So it is checked first: if the whole variable
// names an executable file, it is run directly with no splitting at all.
//
// Windows has no sh, so it keeps whitespace splitting. Nothing in this
// program targets Windows interactively, and a wrong guess there is a failed
// launch with a notice, not a lost draft.
func editorCommand(name, path string) (*exec.Cmd, string) {
	// Bare executable, spaces and all.
	if info, err := os.Stat(name); err == nil && !info.IsDir() {
		return exec.Command(name, path), filepath.Base(name)
	}

	display := name
	if fields := strings.Fields(name); len(fields) > 0 {
		display = filepath.Base(fields[0])
	}

	if runtime.GOOS == "windows" {
		fields := strings.Fields(name)
		return exec.Command(fields[0], append(fields[1:], path)...), display
	}

	// sh -c '<editor> "$@"' sh <path>: the extra "sh" fills $0 so the file
	// lands in $1, and "$@" passes it through without a second round of word
	// splitting whatever it contains.
	return exec.Command("sh", "-c", name+` "$@"`, "sh", path), display
}

// editorResult builds the tea.ExecCallback that runs once the editor exits.
// It always removes the temp file, whether the edit succeeded or not.
func editorResult(path string) func(error) tea.Msg {
	return func(runErr error) tea.Msg {
		defer os.Remove(path)
		if runErr != nil {
			return editorFinishedMsg{err: runErr}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return editorFinishedMsg{err: err}
		}
		return editorFinishedMsg{text: string(data), ok: true}
	}
}

// applyEditorResult folds an editor session back into the composer. Only the
// text changes: the reply/edit mode, the target chat and any pending
// attachment are exactly as they were before the editor opened.
func (m *Model) applyEditorResult(msg editorFinishedMsg) {
	if !msg.ok {
		// A non-zero exit is how every vi user aborts an edit (:cq, or a
		// crash). Keeping the original draft is the only safe reading.
		m.notice = fmt.Sprintf("⚠ editor failed — draft kept: %v", msg.err)
		return
	}

	// Editors add a trailing newline; a chat message should not carry one.
	text := strings.ReplaceAll(msg.text, "\r\n", "\n")
	m.textarea.Value = strings.TrimRight(text, "\n")
	m.textarea.Cursor = m.textarea.Len()
	m.notice = ""
	// Coming back from the editor, the user is composing again.
	if m.editing == ModeVi {
		m.vi = viInsert
	}
}
