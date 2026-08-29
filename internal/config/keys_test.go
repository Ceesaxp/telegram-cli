package config

import "testing"

func TestNormalizeKey(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"already normalized", "alt+l", "alt+l"},
		{"uppercase", "ALT+L", "alt+l"},
		{"mixed case", "Alt+H", "alt+h"},
		{"surrounding whitespace", "  ctrl+c  ", "ctrl+c"},
		{"escape alias", "escape", "esc"},
		{"escape alias uppercase", "ESCAPE", "esc"},
		{"esc unchanged", "esc", "esc"},
		{"function key", "F1", "f1"},

		// Modifier aliases. "option"/"opt" matter on macOS, where the Alt key
		// is labelled Option and users write it that way.
		{"option alias", "option+1", "alt+1"},
		{"opt alias", "Opt+C", "alt+c"},
		{"control alias", "Control+v", "ctrl+v"},
		{"ctl alias", "ctl+v", "ctrl+v"},
		{"cmd alias", "cmd+k", "super+k"},

		// Modifiers are re-emitted in Keystroke()'s fixed order
		// (ctrl, alt, shift, meta, hyper, super).
		{"modifier reorder", "shift+ctrl+a", "ctrl+shift+a"},
		{"modifier reorder with alt", "shift+alt+a", "alt+shift+a"},
		{"duplicate modifier", "alt+alt+l", "alt+l"},

		// Key-name aliases.
		{"return alias", "Return", "enter"},
		{"pageup alias", "PageUp", "pgup"},
		{"page_down alias", "page_down", "pgdown"},
		{"del alias", "del", "delete"},

		// A literal "+" binding must survive the modifier split.
		{"bare plus", "+", "+"},
		{"ctrl plus", "ctrl++", "ctrl++"},

		// An unrecognized leading token is part of the key, not a modifier.
		{"unknown modifier kept", "foo+a", "foo+a"},

		// Bare printables and unmodified keys pass through.
		{"slash", "/", "/"},
		{"question mark", "?", "?"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NormalizeKey(tc.in); got != tc.want {
				t.Errorf("NormalizeKey(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestDefaultConfigFolderKeys(t *testing.T) {
	cfg := defaultConfig()
	if cfg.Keys.NextFolder != "alt+l" {
		t.Errorf("default NextFolder = %q, want alt+l", cfg.Keys.NextFolder)
	}
	if cfg.Keys.PrevFolder != "alt+h" {
		t.Errorf("default PrevFolder = %q, want alt+h", cfg.Keys.PrevFolder)
	}
}

func TestResolveComposeEditing(t *testing.T) {
	cases := []struct {
		name    string
		setting string
		visual  string
		editor  string
		want    string
	}{
		// An explicit setting wins over the environment.
		{"explicit emacs", "emacs", "vim", "vim", ComposeEditingEmacs},
		{"explicit vi", "vi", "emacs", "emacs", ComposeEditingVi},
		{"explicit is case/space insensitive", "  VI  ", "emacs", "", ComposeEditingVi},

		// auto: $VISUAL first, then $EDITOR.
		{"auto visual vim", "auto", "vim", "emacs", ComposeEditingVi},
		{"auto visual emacs", "auto", "emacs", "vim", ComposeEditingEmacs},
		{"auto falls back to editor", "auto", "", "nvim", ComposeEditingVi},
		{"auto blank visual falls back", "auto", "   ", "vim", ComposeEditingVi},
		{"auto nothing set", "auto", "", "", ComposeEditingEmacs},

		// auto: the command name is what matters, not the path or args.
		{"auto absolute path", "auto", "", "/usr/local/bin/vim", ComposeEditingVi},
		{"auto with arguments", "auto", "", "nvim -u NONE", ComposeEditingVi},
		{"auto path with vi in a parent dir", "auto", "", "/opt/vim/bin/nano", ComposeEditingEmacs},
		{"auto gvim", "auto", "", "gvim", ComposeEditingVi},
		{"auto view", "auto", "", "view", ComposeEditingVi},
		{"auto nano", "auto", "", "nano", ComposeEditingEmacs},
		{"auto vscode", "auto", "", "code --wait", ComposeEditingEmacs},

		// An empty or unrecognized setting behaves like auto.
		{"empty is auto", "", "", "vim", ComposeEditingVi},
		{"empty is auto, no editor", "", "", "", ComposeEditingEmacs},
		{"typo is auto", "vmi", "", "vim", ComposeEditingVi},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("VISUAL", tc.visual)
			t.Setenv("EDITOR", tc.editor)
			if got := ResolveComposeEditing(tc.setting); got != tc.want {
				t.Errorf("ResolveComposeEditing(%q) with VISUAL=%q EDITOR=%q = %q, want %q",
					tc.setting, tc.visual, tc.editor, got, tc.want)
			}
		})
	}
}

func TestDefaultConfigComposeEditing(t *testing.T) {
	if got := defaultConfig().UI.ComposeEditing; got != ComposeEditingAuto {
		t.Errorf("default ComposeEditing = %q, want %q", got, ComposeEditingAuto)
	}
}

// TestExampleConfigKeysMatchDefaults keeps config.example.toml's [keys] block
// in step with defaultConfig. The example is what users copy, and it has
// silently fallen behind twice — every binding added in a wave has to be
// added here too, or new users start from a config that is missing them.
//
// Only [keys] is compared. Other sections deliberately diverge: the example
// sets parse_markdown = true to show the feature off, while the built-in
// default is false (see UIConfig.ParseMarkdown).
func TestExampleConfigKeysMatchDefaults(t *testing.T) {
	t.Setenv("TELETUI_CONFIG", "../../config.example.toml")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("loading config.example.toml: %v", err)
	}

	def := defaultConfig()
	for _, f := range keyFields {
		got, want := f.get(&cfg.Keys), f.get(&def.Keys)
		if got == "" {
			t.Errorf("config.example.toml is missing keys.%s (default %q)", f.name, want)
			continue
		}
		if got != want {
			t.Errorf("config.example.toml has keys.%s = %q, want the default %q",
				f.name, got, want)
		}
	}
}
