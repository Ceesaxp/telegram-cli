package telegram

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// chatIDTranslators are the calls that TRANSLATE a chat ID: splitTopic
// turns a topic's synthetic ID into the forum to name on the wire and the
// topic inside it, and everything reached through it does the same.
//
// Reaching one of these is what the guard below requires, and it is the
// default expectation of every exported method that takes a chat ID.
var chatIDTranslators = map[string]bool{
	"splitTopic": true,
}

// chatIDRefusers are the calls that REFUSE a synthetic chat ID with an
// error rather than translating it. They are the safety net under the
// translation, not a substitute for it: what a method that reaches one of
// these gives a topic is a sentence saying no.
//
// Both are held to actually refusing by TestARefuserStillRefuses below.
var chatIDRefusers = map[string]bool{
	"inputPeer":   true,
	"refuseTopic": true,
}

// topicRefusingMethods are the exported methods that refuse a topic ON
// PURPOSE, with the argument for each.
//
// Refusing is the rare answer and it has to be an argued one. Reaching a
// refusal used to count as handling a chat ID, and five methods sat here
// looking handled while a reader inside a topic simply could not react to a
// message, pin one, edit their own, forward in or out, or open a chat whose
// header asks how many members it has. A refusal that nobody argued for is
// indistinguishable from a hole, which is exactly what those five were.
//
// So: a method on this list must reach a refuser and not a translator, and
// every method NOT on it must translate. Both halves are checked, so the
// list can neither grow by accident nor rot — an entry whose method starts
// translating fails, and so does one whose method stops refusing.
var topicRefusingMethods = map[string]string{
	// Checked, and answering about the forum would be a different question:
	// messages.getFullChat answers about basic groups alone, and the forum
	// behind a topic is a supergroup, which GetSupergroupFullInfo answers
	// about. There is nothing here a topic could be given.
	"GetBasicGroupFullInfo": "a topic is never a basic group — its forum is a supergroup, and GetSupergroupFullInfo is the call that answers about one",

	// Comments hang off a broadcast channel's post, in the discussion group
	// linked to it. A topic is a thread in a supergroup, which is the other
	// side of that relationship and has no comments of its own.
	"DiscussionMessage": "comments belong to a broadcast post, and a topic is a supergroup thread: there is no discussion group behind one to open",
}

// Every exported method that takes a chat ID translates it, unless it is
// one of the few that refuse a topic on purpose.
//
// A topic's chat ID is not a peer (see topics.go), and the whole of this
// package's safety rests on that ID being split into the forum and the
// topic before anything is asked of the server. Stopping at a refusal keeps
// a topic off the wire, which is why it is allowed at all — but it is an
// answer of "no" to the reader, so it has to be one somebody decided on
// rather than one a method fell into.
//
// "Reaches" is deliberately strict: the call has to be on every path
// through the method, not inside a branch. DeleteMessages resolves a peer
// only when the chat is a channel, and a synthetic ID is not a channel —
// so the branch that would have refused it is exactly the branch it does
// not take.
func TestEveryChatIDIsTranslatedOrRefusedOnPurpose(t *testing.T) {
	pkg := parsePackageForGuard(t)

	methods := pkg.exportedClientMethodsTakingAChatID()
	if len(methods) < 20 {
		t.Fatalf("found only %d exported methods taking a chat ID — the scan is "+
			"looking at the wrong thing, and a guard that checks nothing passes",
			len(methods))
	}

	translates := pkg.reaching(chatIDTranslators)
	refuses := pkg.reaching(chatIDRefusers)

	for _, name := range methods {
		why, listed := topicRefusingMethods[name]
		switch {
		case translates.from(name):
			if listed {
				t.Errorf("%s translates its chat ID now, but is still listed as "+
					"refusing one on purpose (%q) — delete the entry, or the list "+
					"describes a decision the code no longer makes", name, why)
			}
		case refuses.from(name) && listed:
			// An argued refusal.
		case refuses.from(name):
			t.Errorf("%s refuses a forum topic rather than translating it, and "+
				"nobody said why. That is how five broken methods passed this "+
				"guard for a whole wave: a reader inside a topic gets an error "+
				"where a group gives an answer. Split the chat ID and use the "+
				"forum's, or add %s to topicRefusingMethods with the argument "+
				"for refusing", name, name)
		default:
			t.Errorf("%s takes a chat ID and never reaches splitTopic or a refusal "+
				"on every path — a forum topic handed to it becomes the wrong "+
				"request, quietly. Split it", name)
		}
	}
}

// And a refuser still refuses, because everything above leans on it:
// "reaches inputPeer" is only a safe answer while inputPeer says no to a
// synthetic ID. Deleting that check would leave every method above passing
// this guard while topics went to the wire.
func TestARefuserStillRefuses(t *testing.T) {
	pkg := parsePackageForGuard(t)

	for name := range chatIDRefusers {
		fn, ok := pkg.funcs[name]
		if !ok {
			t.Errorf("%s is gone — the guard above counts methods as safe for "+
				"reaching a function that no longer exists", name)
			continue
		}

		var refuses bool
		ast.Inspect(fn.decl, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && id.Name == "isSyntheticChatID" {
				refuses = true
			}
			return true
		})
		if !refuses {
			t.Errorf("%s does not ask isSyntheticChatID, so a forum topic's chat "+
				"ID passes through it as though it were an ordinary chat", name)
		}
	}
}

// And the translator is there too, for the same reason from the other side.
func TestTheTranslatorIsStillThere(t *testing.T) {
	pkg := parsePackageForGuard(t)

	for name := range chatIDTranslators {
		if _, ok := pkg.funcs[name]; !ok {
			t.Errorf("%s is gone — the guard above counts methods as safe for "+
				"reaching a function that no longer exists", name)
		}
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
}

func parsePackageForGuard(t *testing.T) *guardPackage {
	t.Helper()

	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}

	pkg := &guardPackage{
		fset:  token.NewFileSet(),
		funcs: make(map[string]*guardFunc),
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

// reaching asks one question of the package: which functions always end up
// calling one of targets. Two questions are asked of it — does a method
// translate, and does it refuse — and each keeps its own answers, because
// "reaches" means something different in each.
func (p *guardPackage) reaching(targets map[string]bool) *reachQuery {
	return &reachQuery{pkg: p, targets: targets, memo: make(map[string]bool)}
}

// reachQuery answers "does every path through this function reach one of
// targets", memoizing as it goes.
type reachQuery struct {
	pkg     *guardPackage
	targets map[string]bool

	// memo holds the answers; a name in progress maps to false, which makes
	// a cycle answer "no" rather than recurse.
	memo map[string]bool
}

// from reports whether every path through the named function calls one of
// the query's targets — directly, or through another function of this
// package that itself always does.
//
// Approximated by looking only at the statements at the top level of the
// body: a call inside an if, a loop, a switch or a closure is a call that
// some path does not make. The approximation errs towards "no", which costs
// an argument on topicRefusingMethods and never a missed hole.
func (q *reachQuery) from(name string) bool {
	if answer, known := q.memo[name]; known {
		return answer
	}
	fn, ok := q.pkg.funcs[name]
	if !ok {
		return false
	}
	q.memo[name] = false // in progress: a cycle reaches nothing

	for _, stmt := range fn.decl.Body.List {
		for _, called := range unconditionalCalls(stmt, fn.receiver) {
			if q.targets[called] || q.from(called) {
				q.memo[name] = true
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
