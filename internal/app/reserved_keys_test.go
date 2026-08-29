package app

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"testing"

	"github.com/imtaqin/telegram-cli/internal/config"
	"github.com/imtaqin/telegram-cli/internal/keys"
	"github.com/imtaqin/telegram-cli/internal/store"
	"github.com/imtaqin/telegram-cli/internal/telegram"
)

// TestAppFixedMatchesDispatcher derives the reserved-key list from the
// dispatcher instead of trusting a human to keep the two in step.
//
// keys.AppFixed exists because chatview cannot see what app.go claims. A
// hand-maintained list would have exactly the failure mode that produced
// this whole package: someone adds a bare-letter binding to Update — this
// wave added three, h, l and q — and nothing anywhere notices that a
// component may now be configured to a key it will never receive.
//
// So this parses internal/app/app.go and walks every `key.Matches(...)`
// call in it. Each argument must be accounted for:
//
//   - a string literal must be in keys.AppFixed, or in yieldedNotClaimed
//     below with a reason;
//   - a m.keys.<field> reference must resolve to a value that
//     Model.reservedKeys() actually reports, which is what catches a new
//     configurable binding being dispatched but not passed to
//     keys.AppReserved.
//
// The limit worth naming: it sees calls on the `key` variable inside
// app.go only. A binding dispatched some other way — a bare
// msg.String() switch, a helper in another file — is invisible to it. That
// is a deliberate trade: app.go funnels every binding through one matcher
// today, and a test that tried to understand arbitrary key handling would
// be guessing rather than checking.
func TestAppFixedMatchesDispatcher(t *testing.T) {
	// Keys app.go tests for only so it can BREAK out of its own dispatch
	// and hand them to the focused panel. Testing a key is not claiming
	// it, and reserving these would wrongly forbid rebinding onto them.
	yieldedNotClaimed := map[string]string{
		"n": "yielded to chatview's search-hit cycling",
		"N": "yielded to chatview's search-hit cycling",
		"q": "closes the help overlay, and only while that overlay is up — " +
			"the browsing panels receive no keys then. From a browsing panel " +
			"\"q\" is claimed through the configurable keys.quit_browsing",
	}

	cfg := &config.Config{}
	m := New(cfg, nil, store.NewStore(), telegram.NewTUIAuthorizer(cfg))
	reserved := m.reservedKeys()
	resolved := reflect.ValueOf(m.keys)

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate this test's own source file")
	}
	src := filepath.Join(filepath.Dir(thisFile), "app.go")
	file, err := parser.ParseFile(token.NewFileSet(), src, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", src, err)
	}

	calls := 0
	ast.Inspect(file, func(n ast.Node) bool {
		call, isCall := n.(*ast.CallExpr)
		if !isCall {
			return true
		}
		sel, isSel := call.Fun.(*ast.SelectorExpr)
		if !isSel || sel.Sel.Name != "Matches" {
			return true
		}
		recv, isIdent := sel.X.(*ast.Ident)
		if !isIdent || recv.Name != "key" {
			return true
		}
		calls++

		for _, arg := range call.Args {
			switch a := arg.(type) {
			case *ast.BasicLit:
				if a.Kind != token.STRING {
					continue
				}
				lit, err := strconv.Unquote(a.Value)
				if err != nil {
					t.Errorf("unparsable binding literal %s in app.go", a.Value)
					continue
				}
				if slices.Contains(keys.AppFixed, lit) {
					continue
				}
				if _, exempt := yieldedNotClaimed[lit]; exempt {
					continue
				}
				t.Errorf("app.go dispatches the hardcoded key %q, but it is not "+
					"in keys.AppFixed — chatview can therefore be configured to "+
					"%q and will never receive it. Add it to keys.AppFixed, or to "+
					"yieldedNotClaimed here if app.go only tests it in order to "+
					"yield it", lit, lit)

			case *ast.SelectorExpr:
				// m.keys.<field>
				field := a.Sel.Name
				value := resolved.FieldByName(field)
				if !value.IsValid() || value.Kind() != reflect.String {
					t.Errorf("app.go dispatches on %q, which is not a string "+
						"field of resolvedKeys — this test needs teaching about it",
						exprText(a))
					continue
				}
				if value.String() == "" {
					continue // an unset binding is inert
				}
				if !slices.Contains(reserved, value.String()) {
					t.Errorf("app.go dispatches on m.keys.%s (%q), but "+
						"Model.reservedKeys() does not report it — pass it to "+
						"keys.AppReserved, or chatview may be configured to a key "+
						"app.go takes first", field, value.String())
				}
			}
		}
		return true
	})

	// A refactor that renamed the matcher or the `key` variable would
	// otherwise leave this test walking nothing and passing.
	if calls < 15 {
		t.Fatalf("found only %d key.Matches calls in app.go — the dispatcher "+
			"has been restructured and this test is no longer reading it", calls)
	}

	// And the reverse: an entry in AppFixed that app.go no longer
	// dispatches over-reserves, silently forbidding a rebind that would
	// have worked.
	claimed := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		call, isCall := n.(*ast.CallExpr)
		if !isCall {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Matches" {
			for _, arg := range call.Args {
				if lit, ok := arg.(*ast.BasicLit); ok && lit.Kind == token.STRING {
					if v, err := strconv.Unquote(lit.Value); err == nil {
						claimed[v] = true
					}
				}
			}
		}
		return true
	})
	for _, k := range keys.AppFixed {
		if !claimed[k] {
			t.Errorf("keys.AppFixed contains %q, but app.go no longer dispatches "+
				"it — remove it rather than reserving a key components could use", k)
		}
	}
}

// exprText renders a selector chain (m.keys.help) for a failure message.
func exprText(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		return exprText(v.X) + "." + v.Sel.Name
	}
	return "<expr>"
}

// TestReservedKeysFollowsConfig covers the half of the contract a source
// scan cannot: that rebinding actually moves the reservation, so a user who
// frees a key really can give it to a panel.
func TestReservedKeysFollowsConfig(t *testing.T) {
	cfg := &config.Config{}
	def := New(cfg, nil, store.NewStore(), telegram.NewTUIAuthorizer(cfg)).reservedKeys()
	for _, want := range []string{"h", "l", "q", "i", "c", "/", "?", "tab", "esc", "ctrl+c"} {
		if !slices.Contains(def, want) {
			t.Errorf("default reserved set omits %q: %v", want, def)
		}
	}

	moved := &config.Config{}
	moved.Keys.QuitBrowsing = "f9"
	moved.Keys.Search = "ctrl+s"
	got := New(moved, nil, store.NewStore(), telegram.NewTUIAuthorizer(moved)).reservedKeys()
	for _, want := range []string{"f9", "ctrl+s"} {
		if !slices.Contains(got, want) {
			t.Errorf("reserved set omits the rebound %q: %v", want, got)
		}
	}
	// The freed keys must be released, or rebinding buys the user nothing.
	for _, freed := range []string{"q", "/"} {
		if slices.Contains(got, freed) {
			t.Errorf("%q is still reserved after being rebound away: %v", freed, got)
		}
	}
	// The hardcoded claims are not config's to move.
	for _, fixed := range []string{"h", "l", "i", "c"} {
		if !slices.Contains(got, fixed) {
			t.Errorf("hardcoded claim %q went missing under a rebind: %v", fixed, got)
		}
	}
}
