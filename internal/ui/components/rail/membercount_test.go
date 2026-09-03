package rail

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
)

// TestNobodyAsksForAMemberTotalTwice.
//
// The thread header asks how many members a chat has on every open, rail or
// no rail, and puts the answer in the store. The rail wants the same number
// for its "+192 more" row, and used to ask again — the same question twice
// on the way to the same screen, and two answers that could disagree.
//
// The two full-info calls have different budgets, and the difference is
// what each one BRINGS BACK:
//
//   - GetSupergroupFullInfo returns a count and nothing else, so it has
//     exactly one caller. The rail reads the store.
//   - GetBasicGroupFullInfo returns the MEMBERS as well, and a basic group
//     has no other route to them. The rail has to make it whatever the
//     header did, so it is allowed two callers — and it writes its count
//     through to the store, so the third surface to want one is free.
//
// Nothing else in this codebase may call either.
func TestNobodyAsksForAMemberTotalTwice(t *testing.T) {
	// Name → how many callers that call is allowed, and why.
	allowed := map[string]int{
		"GetSupergroupFullInfo": 1,
		"GetBasicGroupFullInfo": 2,
	}
	callers := map[string][]string{
		"GetSupergroupFullInfo": nil,
		"GetBasicGroupFullInfo": nil,
	}

	root := filepath.Join("..", "..", "..", "..", "internal")
	fset := token.NewFileSet()

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") ||
			strings.HasSuffix(path, "_test.go") {
			return err
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if _, tracked := callers[sel.Sel.Name]; tracked {
				callers[sel.Sel.Name] = append(callers[sel.Sel.Name],
					fset.Position(call.Pos()).String())
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	for name, where := range callers {
		if len(where) != allowed[name] {
			t.Errorf("%s has %d callers, want %d:\n  %s",
				name, len(where), allowed[name], strings.Join(where, "\n  "))
		}
	}
}

// TestTheSupergroupMembersCallCarriesNoTotal. The participants call returns
// a page, and the rail no longer follows it with a second request to find
// out how big the group really is.
func TestTheSupergroupMembersCallCarriesNoTotal(t *testing.T) {
	source := readSource(t, "fetch.go")

	members := source[strings.Index(source, "func (m Model) membersCmd"):]
	members = members[:strings.Index(members, "\n}\n")]

	if strings.Contains(members, "GetSupergroupFullInfo") {
		t.Error("membersCmd asks a supergroup how many members it has; " +
			"the thread header already did, and put it in the store")
	}
	if !strings.Contains(members, "GetBasicGroupFullInfo") {
		t.Error("a basic group's members and its total come in one call; " +
			"membersCmd should still make that one")
	}
}

// TestTheRemainderReadsTheStore, which is the other half: a total nobody
// has fetched leaves the row out rather than counting the page.
func TestTheRemainderReadsTheStore(t *testing.T) {
	m, s := railModel(t, telegram.ChatTypeSupergroup)
	m.Open(testChatID)
	d := m.data[testChatID]
	d.membersState = stateReady

	for i := range 3 {
		id := int64(100 + i)
		s.Users.Set(&telegram.User{ID: id, FirstName: "member"})
		d.members = append(d.members, &telegram.ChatMember{
			MemberID: &telegram.MessageSenderUser{UserID: id},
			Status:   &telegram.ChatMemberStatusMember{},
		})
	}

	joined := strings.Join(rows(t, m), "\n")
	if strings.Contains(joined, "more") {
		t.Fatalf("a total nobody has fetched produced a remainder row:\n%s", joined)
	}

	s.Chats.SetMemberCount(testChatID, 24)
	joined = strings.Join(rows(t, m), "\n")
	if !strings.Contains(joined, "+21 more") {
		t.Fatalf("the remainder did not come from the store:\n%s", joined)
	}
}

// TestABasicGroupsTotalIsWrittenThrough. It arrives with the members, for
// free, and it is the same number the thread header wants.
func TestABasicGroupsTotalIsWrittenThrough(t *testing.T) {
	m, s := railModel(t, telegram.ChatTypeBasicGroup)
	m.Open(testChatID)

	updated, _ := m.Update(membersResultMsg{
		gen: m.gen, chatID: testChatID, count: 12,
		members: []*telegram.ChatMember{{
			MemberID: &telegram.MessageSenderUser{UserID: 100},
			Status:   &telegram.ChatMemberStatusMember{},
		}},
	})
	m = updated

	if got := s.Chats.MemberCount(testChatID); got != 12 {
		t.Fatalf("store member count = %d, want 12", got)
	}
}

// readSource is one of this package's own files, for the tests that assert
// on what the code says rather than on what it does.
func readSource(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
