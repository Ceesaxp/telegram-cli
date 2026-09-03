package app

import (
	"strings"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/config"
	"github.com/Ceesaxp/telegram-cli/internal/store"
	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/composer"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/palette"
)

// --- The registry ---------------------------------------------------------

// TestRegistryHasNoDeferredCommands is the guard for decision 8. Secret chat
// and Markdown export are deferred; a palette entry that cannot run is worse
// than no entry, because the user learns the command exists and then finds it
// does nothing.
func TestRegistryHasNoDeferredCommands(t *testing.T) {
	m := mainModel(t, PanelChatList)
	for _, c := range m.commandRegistry() {
		switch c.Name {
		case "secret", "export":
			t.Errorf("%q is registered, but decision 8 defers it", c.Name)
		}
	}
}

// TestRegistryEntriesAreWellFormed catches the table mistakes that would
// otherwise only show up as a confusing palette row.
func TestRegistryEntriesAreWellFormed(t *testing.T) {
	m := mainModel(t, PanelChatList)
	seen := map[string]bool{}

	for _, c := range m.commandRegistry() {
		if c.Name == "" {
			t.Error("a command has no name")
			continue
		}
		if seen[c.Name] {
			t.Errorf("%q is registered twice; lookup would silently pick the first", c.Name)
		}
		seen[c.Name] = true

		if strings.HasPrefix(c.Name, ":") {
			t.Errorf("%q includes the colon; the registry stores bare names", c.Name)
		}
		if c.Description == "" {
			t.Errorf("%q has no description to show in the palette", c.Name)
		}
		if c.Run == nil {
			t.Errorf("%q has no Run", c.Name)
		}
		if c.Arg == ArgNone && c.Placeholder != "" {
			t.Errorf("%q takes no argument but names one (%q)", c.Name, c.Placeholder)
		}
		if c.Arg != ArgNone && c.Placeholder == "" {
			t.Errorf("%q takes an argument but does not name it", c.Name)
		}
	}
}

// TestRegistryKeysAreResolvedNotHardcoded: the palette teaches the keymap, so
// a rebound key has to show correctly there or it teaches the wrong thing.
func TestRegistryKeysAreResolvedNotHardcoded(t *testing.T) {
	t.Setenv("EDITOR", "")
	cfg := &config.Config{}
	cfg.Keys.Help = "f9"
	m := New(cfg, nil, store.NewStore(), telegram.NewTUIAuthorizer(cfg))

	for _, c := range m.commandRegistry() {
		if c.Name == "keymap" {
			if c.Key != "f9" {
				t.Errorf("keymap advertises %q, want the rebound f9", c.Key)
			}
			return
		}
	}
	t.Fatal("no keymap command in the registry")
}

func TestPaletteItemsAreSortedAndProjected(t *testing.T) {
	m := mainModel(t, PanelChatList)
	items := m.paletteItems()

	if len(items) != len(m.commandRegistry()) {
		t.Fatalf("projected %d items from %d commands", len(items), len(m.commandRegistry()))
	}
	for i := 1; i < len(items); i++ {
		if items[i-1].Name > items[i].Name {
			t.Errorf("items are not sorted: %q before %q", items[i-1].Name, items[i].Name)
		}
	}
}

// --- Parsing and validation ----------------------------------------------

func TestRunCommandLineRejectsUnknownCommands(t *testing.T) {
	m := mainModel(t, PanelChatList)
	_, _, notice := m.runCommandLine("nonsense")
	if !strings.Contains(notice, "nonsense") {
		t.Errorf("notice = %q, want it to name the unknown command", notice)
	}
}

// TestRunCommandLineRejectsSurplusArguments: silently discarding input is how
// ":quit now" turns into a bug report about a command that does nothing.
func TestRunCommandLineRejectsSurplusArguments(t *testing.T) {
	m := mainModel(t, PanelChatList)
	_, cmd, notice := m.runCommandLine("quit now")
	if cmd != nil {
		t.Error("a command with a surplus argument still ran")
	}
	if !strings.Contains(notice, "takes no argument") {
		t.Errorf("notice = %q, want it to explain the argument was not expected", notice)
	}
}

func TestRunCommandLineToleratesALeadingColon(t *testing.T) {
	m := mainModel(t, PanelChatList)
	if _, _, notice := m.runCommandLine(":quit"); notice != "" {
		t.Errorf("notice = %q, want a leading colon to be accepted", notice)
	}
}

func TestRunCommandLineIgnoresAnEmptyLine(t *testing.T) {
	m := mainModel(t, PanelChatList)
	_, cmd, notice := m.runCommandLine("")
	if cmd != nil || notice != "" {
		t.Errorf("empty line produced cmd=%v notice=%q, want nothing", cmd != nil, notice)
	}
}

func TestValidateArg(t *testing.T) {
	tests := []struct {
		name    string
		cmd     Command
		arg     string
		wantErr bool
	}{
		{"none with none", Command{Name: "quit", Arg: ArgNone}, "", false},
		{"none with surplus", Command{Name: "quit", Arg: ArgNone}, "x", true},
		{"optional without", Command{Name: "search", Arg: ArgOptional, Placeholder: "<q>"}, "", false},
		{"optional with", Command{Name: "search", Arg: ArgOptional, Placeholder: "<q>"}, "x", false},
		{"required without", Command{Name: "jump", Arg: ArgRequired, Placeholder: "<date>"}, "", true},
		{"required with", Command{Name: "jump", Arg: ArgRequired, Placeholder: "<date>"}, "x", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := validateArg(tc.cmd, tc.arg)
			if (got != "") != tc.wantErr {
				t.Errorf("validateArg = %q, wantErr = %v", got, tc.wantErr)
			}
		})
	}
}

// --- Commands doing their jobs -------------------------------------------

func TestQuitCommandQuits(t *testing.T) {
	m := mainModel(t, PanelChatList)
	_, cmd, _ := m.runCommandLine("quit")
	if cmd == nil {
		t.Fatal(":quit returned no command")
	}
}

func TestKeymapCommandOpensHelp(t *testing.T) {
	m := mainModel(t, PanelChatList)
	m, _, _ = m.runCommandLine("keymap")
	if !m.help.IsVisible() {
		t.Error(":keymap did not open the help overlay")
	}
}

func TestSearchCommandOpensAndPrefills(t *testing.T) {
	m := mainModel(t, PanelChatList)
	m, _, _ = m.runCommandLine("search hello world")

	if !m.search.IsVisible() {
		t.Fatal(":search did not open the search overlay")
	}
	if m.focus != PanelSearch {
		t.Errorf("focus = %v, want PanelSearch", m.focus)
	}
	// The prefill must survive SetVisible, which resets the query — the
	// ordering trap this command exists to get right.
	if got := m.search.Query(); got != "hello world" {
		t.Errorf("search query = %q, want the whole argument", got)
	}
}

func TestSearchCommandWithoutAnArgumentJustOpens(t *testing.T) {
	m := mainModel(t, PanelChatList)
	m, _, notice := m.runCommandLine("search")
	if !m.search.IsVisible() {
		t.Error(":search with no argument should still open the overlay")
	}
	if notice != "" {
		t.Errorf("notice = %q, want none for an optional argument", notice)
	}
}

func TestMarkReadCommandNeedsAChat(t *testing.T) {
	m := mainModel(t, PanelChatList)
	_, cmd, notice := m.runCommandLine("mark-read")
	if cmd != nil {
		t.Error(":mark-read tried to act with no chat open")
	}
	if notice == "" {
		t.Error(":mark-read said nothing when it could not run")
	}
}

// --- Palette wiring through the app --------------------------------------

func TestColonOpensThePalette(t *testing.T) {
	m := mainModel(t, PanelChatList)
	updated, _ := m.Update(decodeKey(t, ":"))
	m = updated.(Model)

	if !m.palette.IsVisible() {
		t.Fatal("`:` did not open the palette from the chat list")
	}
	if got := m.Mode(); got != ModeCommand {
		t.Errorf("Mode() = %v with the palette open, want COMMAND", got)
	}
}

// TestColonTypesInAnEmacsComposer is the reason `:` consults the mode
// resolver rather than the focus panel: in INSERT it is text, not a command.
func TestColonTypesInAnEmacsComposer(t *testing.T) {
	m := openChatModel(t, PanelComposer)
	updated, _ := m.Update(decodeKey(t, ":"))
	m = updated.(Model)

	if m.palette.IsVisible() {
		t.Error("`:` opened the palette from a focused emacs composer; it must type")
	}
}

// TestColonOpensThePaletteFromAViComposerInCommandState is the other half:
// the resolver reports NORMAL there, so the colon is a command again.
func TestColonOpensThePaletteFromAViComposerInCommandState(t *testing.T) {
	m := openChatModel(t, PanelComposer)
	m.composer.SetEditingMode(composer.ModeVi)

	updated, _ := m.Update(decodeKey(t, "\x1b")) // leave vi insert
	m = updated.(Model)
	if m.Mode() != ModeNormal {
		t.Fatalf("precondition: Mode() = %v, want NORMAL", m.Mode())
	}

	updated, _ = m.Update(decodeKey(t, ":"))
	m = updated.(Model)
	if !m.palette.IsVisible() {
		t.Error("`:` did not open the palette from a vi composer in command state")
	}
}

// TestPaletteOwnsTheKeyboard: while it is open nothing behind it may act, or
// a command name containing a bound letter could never be typed.
func TestPaletteOwnsTheKeyboard(t *testing.T) {
	m := mainModel(t, PanelChatList)
	updated, _ := m.Update(decodeKey(t, ":"))
	m = updated.(Model)

	// "?" is the help key; inside the palette it must be query text.
	updated, _ = m.Update(decodeKey(t, "?"))
	m = updated.(Model)

	if m.help.IsVisible() {
		t.Error("`?` opened help from inside the palette; the palette must own input")
	}
	if m.palette.Query() != "?" {
		t.Errorf("palette query = %q, want %q", m.palette.Query(), "?")
	}
}

func TestEscapeClosesThePaletteWithoutRunning(t *testing.T) {
	m := mainModel(t, PanelChatList)
	updated, _ := m.Update(decodeKey(t, ":"))
	m = updated.(Model)
	for _, r := range "keymap" {
		updated, _ = m.Update(decodeKey(t, string(r)))
		m = updated.(Model)
	}

	updated, _ = m.Update(decodeKey(t, "\x1b"))
	m = updated.(Model)

	if m.palette.IsVisible() {
		t.Error("esc did not close the palette")
	}
	if m.help.IsVisible() {
		t.Error("esc ran the command instead of cancelling")
	}
	if got := m.Mode(); got != ModeNormal {
		t.Errorf("Mode() = %v after closing the palette, want NORMAL", got)
	}
}

func TestEnterRunsTheTypedCommand(t *testing.T) {
	m := mainModel(t, PanelChatList)
	updated, _ := m.Update(decodeKey(t, ":"))
	m = updated.(Model)
	for _, r := range "keymap" {
		updated, _ = m.Update(decodeKey(t, string(r)))
		m = updated.(Model)
	}
	updated, _ = m.Update(decodeKey(t, "\r"))
	m = updated.(Model)

	if m.palette.IsVisible() {
		t.Error("the palette stayed open after running a command")
	}
	if !m.help.IsVisible() {
		t.Error("enter did not run :keymap")
	}
}

// TestAnInvalidCommandReportsRatherThanFailingSilently: the palette closes on
// Enter, so the notice is the only feedback the user gets.
func TestAnInvalidCommandReportsRatherThanFailingSilently(t *testing.T) {
	m := mainModel(t, PanelChatList)
	updated, _ := m.Update(decodeKey(t, ":"))
	m = updated.(Model)
	for _, r := range "nope" {
		updated, _ = m.Update(decodeKey(t, string(r)))
		m = updated.(Model)
	}
	updated, _ = m.Update(decodeKey(t, "\r"))
	m = updated.(Model)

	if m.palette.IsVisible() {
		t.Error("the palette stayed open after an unknown command")
	}
	if !strings.Contains(m.composer.View(), "nope") {
		t.Error("an unknown command produced no visible notice")
	}
}

// TestPaletteItemsMatchTheRegistry guards the single-source claim: the
// palette must be showing what the registry can actually run.
func TestPaletteItemsMatchTheRegistry(t *testing.T) {
	m := mainModel(t, PanelChatList)

	registered := map[string]bool{}
	for _, c := range m.commandRegistry() {
		registered[c.Name] = true
	}
	for _, it := range m.paletteItems() {
		if !registered[it.Name] {
			t.Errorf("palette offers %q, which the registry cannot run", it.Name)
		}
	}

	// And the palette the app actually holds was populated, not left empty.
	var shown []palette.Item
	for _, it := range m.palette.Matches() {
		shown = append(shown, it)
	}
	if len(shown) != len(registered) {
		t.Errorf("the live palette shows %d commands, registry has %d",
			len(shown), len(registered))
	}
}
