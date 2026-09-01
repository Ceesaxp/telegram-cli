package telegram

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// A chat built from a peer must be built in exactly one place.
//
// chatFromUser, chatFromBasicGroup and chatFromChannel turn a Telegram
// entity into a domain Chat, and they cannot fill in Muted: notify settings
// belong to the account's view of a peer and are a separate call. So a chat
// that leaves one of them without going through resolvedChat carries
// Muted=false — and ChatStore.Merge copies the mute flag, precisely so a
// fetch can update it, which means a chat that never asked will UNMUTE
// whatever the dialog list had learned.
//
// That is not hypothetical. CreatePrivateChat called chatFromUser directly,
// so opening a muted contact from the contact list unmuted it — the same bug
// the FromPeer flag exists to prevent, one layer down, three days after the
// flag was added. A flag that call sites have to remember is a flag one of
// them forgets, and the answer is to leave them nothing to remember.
//
// chatFromPeer is the other permitted caller: it is the DIALOG path, where
// the mute flag arrives in the dialog itself and a second call would be a
// wasted round trip per chat.
func TestPeerChatsAreBuiltInOnePlace(t *testing.T) {
	const (
		resolvedChatFn = "resolvedChat"
		dialogFn       = "chatFromPeer"
	)
	builders := map[string]bool{
		"chatFromUser":       true,
		"chatFromBasicGroup": true,
		"chatFromChannel":    true,
	}

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}

	fset := token.NewFileSet()
	found := 0
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			enclosing := fn.Name.Name

			ast.Inspect(fn, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || !builders[sel.Sel.Name] {
					return true
				}
				found++

				if enclosing != resolvedChatFn && enclosing != dialogFn {
					t.Errorf("%s calls %s directly — a chat built outside %s "+
						"carries Muted=false and will unmute the chat when the "+
						"store merges it (%s)",
						enclosing, sel.Sel.Name, resolvedChatFn,
						fset.Position(call.Pos()))
				}
				return true
			})
		}
	}

	// The scan has to have found something, or this passes by looking at
	// nothing — which is how a guard quietly stops guarding.
	if found < len(builders) {
		t.Errorf("found %d calls to the chat builders, want at least %d — "+
			"has something been renamed?", found, len(builders))
	}
}

// And the one place fills the flag in, rather than leaving it to the caller.
func TestResolvedChatAsksAboutMute(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "chats.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	var asks bool
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "resolvedChat" {
			continue
		}
		ast.Inspect(fn, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && id.Name == "peerMuted" {
				asks = true
			}
			return true
		})
	}
	if !asks {
		t.Error("resolvedChat does not call peerMuted, so every peer-derived " +
			"chat reports itself unmuted")
	}
}
