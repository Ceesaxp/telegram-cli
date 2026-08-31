package theme

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// allowedColourLiterals are the files permitted to name a colour, each with
// the reason. Adding an entry is a decision; leaving one undocumented is how
// the second palette started.
var allowedColourLiterals = map[string]string{
	"roles.go":      "the palette itself",
	"roles_test.go": "asserts on the palette's own values",

	// Fixtures built out of raw SGR sequences, where the subject IS the
	// escape sequence. "48;5;233" there is a byte pattern being asserted
	// on, not a colour being chosen.
	"fill_test.go": "constructs SGR fixtures by hand",
}

// One palette, and a guard so it stays one.
//
// Six components used to draw from a second theme — a struct of pre-built
// lipgloss styles with its own hard-coded bright blue 39 and green 42 — so
// every overlay was a different palette from the frame beneath it. The struct
// is gone; this is what stops a literal colour creeping back in to replace it
// one call at a time.
//
// It scans rather than asking anyone to remember the rule, which is the same
// shape as the keymap drift guards: a convention nothing checks is a
// convention with a half-life.
func TestNoColourLiteralsOutsideThePalette(t *testing.T) {
	offences, err := colourLiterals(uiRoot(t), allowedColourLiterals)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range offences {
		t.Errorf("colour literal outside the palette: %s — "+
			"use a theme.Roles field, or add a role if none fits", o)
	}
}

// The scanner is tested separately from the rule it enforces, against a tree
// built for the purpose.
//
// Without this, a guard that stopped reporting would go unnoticed: the only
// thing that fails when a scanner returns nothing is the scanner's own
// assertion, and an assertion that never fires looks exactly like a codebase
// that is clean.
func TestTheColourGuardFindsLiterals(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	write("offender.go", `package x

import "github.com/charmbracelet/lipgloss"

var s = lipgloss.NewStyle().Foreground(lipgloss.Color("#565F89"))
`)
	write("exempt.go", `package x

import "github.com/charmbracelet/lipgloss"

var s = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
`)
	write("clean.go", `package x

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/imtaqin/telegram-cli/internal/ui/theme"
)

func f(r theme.Roles) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(r.Cyan)
}
`)

	got, err := colourLiterals(dir, map[string]string{"exempt.go": "for the test"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("found %d offences, want exactly 1: %v", len(got), got)
	}
	if !strings.Contains(got[0], "offender.go") || !strings.Contains(got[0], "#565F89") {
		t.Errorf("offence does not name the file and the literal: %q", got[0])
	}
}

// colourLiterals reports every lipgloss.Color("...") under root, skipping the
// files named in allowed.
//
// A string literal only: lipgloss.Color(someVariable) is a role by the time
// it reaches the call, which is the whole point of having roles.
func colourLiterals(root string, allowed map[string]string) ([]string, error) {
	var offences []string

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		if _, ok := allowed[filepath.Base(path)]; ok {
			return nil
		}

		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}

		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 1 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Color" {
				return true
			}
			if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "lipgloss" {
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				rel = path
			}
			pos := fset.Position(lit.Pos())
			offences = append(offences,
				rel+":"+itoa(pos.Line)+":"+itoa(pos.Column)+" "+lit.Value)
			return true
		})
		return nil
	})
	return offences, err
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// uiRoot locates internal/ui from this package's own directory.
func uiRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Dir(wd) // .../internal/ui/theme -> .../internal/ui
}
