package telegram

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// chatIDTranslators are the two acceptable answers to "what does this
// method do with a chat ID it was handed": translate it (splitTopic) or
// refuse it (inputPeer, which refuses a synthetic ID outright).
var chatIDTranslators = map[string]bool{
	"splitTopic": true,
	"inputPeer":  true,
}

// topicUnawareMethods are the exported methods that today do neither, with
// the wave that will make each one translate.
//
// Every entry is a real hole: hand one of these a topic's chat ID and it
// will do something wrong quietly — look up a peer that does not exist,
// fetch the forum's history instead of the topic's, or mark the whole
// forum read. They are listed rather than fixed because fixing them is
// what the later waves ARE, and a guard that only holds the methods
// already fixed would let the next new method slip through unlisted.
//
// This list must only ever shrink. The test below fails on an entry that
// no longer needs to be here, so a wave that lands cannot leave its name
// behind.
var topicUnawareMethods = map[string]string{
	// Wave 1, history and message fetches. These three pick their RPC on
	// IsChannel, and a synthetic ID answers no, so they take the
	// peerless non-channel path: messages.getMessages /
	// messages.deleteMessages / messages.readMessageContents, which name
	// no chat at all and would act on the account's own numbering.
	"GetMessages":    "wave 1 — the non-channel branch never splits",
	"GetMessage":     "wave 1 — via GetMessages",
	"DeleteMessages": "wave 2 — the non-channel branch never splits",
	"ReadMentions":   "wave 2 — the non-channel branch never splits",
	// Wave 1: re-registering a file's message goes through GetMessages.
	"DownloadMessageFile": "wave 1 — via GetMessages, to re-register the file",
	// A topic is a supergroup's, so these two are about the forum and
	// want the forum's ID. Wave 3, when a topic's chat is complete
	// enough for the member picker and the group info panel to open on
	// one.
	"GetBasicGroupFullInfo": "wave 3 — a topic is never a basic group",
	"SearchChatMembers":     "wave 3 — members belong to the forum, not the topic",
}

// Every exported method that takes a chat ID either translates it or
// refuses it.
//
// A topic's chat ID is not a peer (see topics.go), and the whole of this
// package's safety rests on that ID either being split into the forum and
// the topic before anything is asked of the server, or stopping at
// inputPeer with an error that says what it is. A method that does neither
// is a method where a topic silently becomes the wrong request.
//
// "Reaches" is deliberately strict: the call has to be on every path
// through the method, not inside a branch. DeleteMessages resolves a peer
// only when the chat is a channel, and a synthetic ID is not a channel —
// so the branch that would have refused it is exactly the branch it does
// not take.
func TestEveryChatIDIsTranslatedOrRefused(t *testing.T) {
	pkg := parsePackageForGuard(t)

	methods := pkg.exportedClientMethodsTakingAChatID()
	if len(methods) < 20 {
		t.Fatalf("found only %d exported methods taking a chat ID — the scan is "+
			"looking at the wrong thing, and a guard that checks nothing passes",
			len(methods))
	}

	for _, name := range methods {
		switch {
		case pkg.alwaysReachesATranslator(name):
			if why, listed := topicUnawareMethods[name]; listed {
				t.Errorf("%s translates its chat ID now, but is still listed as "+
					"not doing so (%q) — delete the entry, the list only shrinks",
					name, why)
			}
		case topicUnawareMethods[name] != "":
			// Known hole, owned by a later wave.
		default:
			t.Errorf("%s takes a chat ID and never reaches splitTopic or inputPeer "+
				"on every path — a forum topic handed to it becomes the wrong "+
				"request. Translate it, or add it to topicUnawareMethods with the "+
				"wave that will", name)
		}
	}
}

// And the refusal itself is there, because everything above leans on it:
// "reaches inputPeer" is only a safe answer while inputPeer says no to a
// synthetic ID. Deleting that check would leave every method above passing
// this guard while topics went to the wire.
func TestTheRefusalIsStillInInputPeer(t *testing.T) {
	pkg := parsePackageForGuard(t)

	fn, ok := pkg.funcs["inputPeer"]
	if !ok {
		t.Fatal("inputPeer is gone — the guard above is checking that methods " +
			"reach a function that no longer exists")
	}

	var refuses bool
	ast.Inspect(fn.decl, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Name == "isSyntheticChatID" {
			refuses = true
		}
		return true
	})
	if !refuses {
		t.Error("inputPeer does not ask isSyntheticChatID, so a forum topic's " +
			"chat ID resolves as though it were a peer and reaches the wire")
	}
}

// guardFunc is one function or method of the package: its declaration, the
// receiver it was written with, and whether it is a method on *Client.
type guardFunc struct {
	decl     *ast.FuncDecl
	receiver string
	onClient bool
}

// guardPackage is the package's non-test sources, parsed.
type guardPackage struct {
	fset  *token.FileSet
	funcs map[string]*guardFunc

	// reaches memoizes alwaysReachesATranslator; a name in progress maps
	// to false, which makes a cycle answer "no" rather than recurse.
	reaches map[string]bool
}

func parsePackageForGuard(t *testing.T) *guardPackage {
	t.Helper()

	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}

	pkg := &guardPackage{
		fset:    token.NewFileSet(),
		funcs:   make(map[string]*guardFunc),
		reaches: make(map[string]bool),
	}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(pkg.fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			pkg.funcs[fn.Name.Name] = &guardFunc{
				decl:     fn,
				receiver: receiverName(fn),
				onClient: isClientMethod(fn),
			}
		}
	}
	return pkg
}

// exportedClientMethodsTakingAChatID names the methods this guard is about.
func (p *guardPackage) exportedClientMethodsTakingAChatID() []string {
	var out []string
	for name, fn := range p.funcs {
		if !fn.onClient || !fn.decl.Name.IsExported() || !takesAChatID(fn.decl) {
			continue
		}
		out = append(out, name)
	}
	return out
}

// alwaysReachesATranslator reports whether every path through the named
// function calls splitTopic or inputPeer — directly, or through another
// function of this package that itself always does.
//
// Approximated by looking only at the statements at the top level of the
// body: a call inside an if, a loop, a switch or a closure is a call that
// some path does not make. The approximation errs towards "no", which
// costs an entry on topicUnawareMethods and never a missed hole.
func (p *guardPackage) alwaysReachesATranslator(name string) bool {
	if answer, known := p.reaches[name]; known {
		return answer
	}
	fn, ok := p.funcs[name]
	if !ok {
		return false
	}
	p.reaches[name] = false // in progress: a cycle reaches nothing

	for _, stmt := range fn.decl.Body.List {
		for _, called := range unconditionalCalls(stmt, fn.receiver) {
			if chatIDTranslators[called] || p.alwaysReachesATranslator(called) {
				p.reaches[name] = true
				return true
			}
		}
	}
	return false
}

// unconditionalCalls names the functions stmt calls whatever happens.
//
// stmt is one statement from the top level of a body, so it runs on every
// path that gets that far; what is skipped is everything nested inside it
// that does not — the branches of an if or a switch, a loop body, a select
// and the body of a function literal, which may never be called at all.
func unconditionalCalls(stmt ast.Stmt, receiver string) []string {
	switch stmt.(type) {
	case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt,
		*ast.TypeSwitchStmt, *ast.SelectStmt, *ast.BlockStmt, *ast.LabeledStmt:
		return nil
	}

	var names []string
	ast.Inspect(stmt, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.CallExpr:
			if name, ok := calleeName(node, receiver); ok {
				names = append(names, name)
			}
		}
		return true
	})
	return names
}

// calleeName is the package-local name a call names: a plain function, or a
// method on the enclosing receiver. A call through anything else — c.api,
// c.peers, a package — is not this package's code and has no name here.
func calleeName(call *ast.CallExpr, receiver string) (string, bool) {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return fun.Name, true
	case *ast.SelectorExpr:
		if id, ok := fun.X.(*ast.Ident); ok && id.Name == receiver && receiver != "" {
			return fun.Sel.Name, true
		}
	}
	return "", false
}

func receiverName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 || len(fn.Recv.List[0].Names) == 0 {
		return ""
	}
	return fn.Recv.List[0].Names[0].Name
}

func isClientMethod(fn *ast.FuncDecl) bool {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return false
	}
	star, ok := fn.Recv.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	id, ok := star.X.(*ast.Ident)
	return ok && id.Name == "Client"
}

// takesAChatID reports whether any int64 parameter is named for a chat ID:
// chatID, fromChatID, toChatID.
func takesAChatID(fn *ast.FuncDecl) bool {
	for _, field := range fn.Type.Params.List {
		id, ok := field.Type.(*ast.Ident)
		if !ok || id.Name != "int64" {
			continue
		}
		for _, name := range field.Names {
			if strings.Contains(strings.ToLower(name.Name), "chatid") {
				return true
			}
		}
	}
	return false
}
