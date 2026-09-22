package composer

import (
	"fmt"
	"math/rand"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Ceesaxp/telegram-cli/internal/telegram"
)

// A random walk of edits never moves a mention to another user (issue #41).
//
// Every rune the walk puts in the draft gets a tag of its own, kept in a
// shadow of the text that the walk edits the way each key will. When a
// mention is made, the tags under its label are recorded against its user.
// After every step the shadow must read exactly as the draft does — or the
// walk has misread a key, and the test is broken — and every span the
// composer still holds must lie over the very runes its user was mentioned
// on. A span over the same name somewhere else is the failure this exists
// to catch: two members called Alex are two different people.
//
// Losing a mention is allowed; the rules drop one whenever the text alone
// cannot say which occurrence survived.
//
// The draft is built to be ambiguous on purpose: labels that repeat and
// prefix one another, lines that start alike, a message loaded for editing
// with three mentions of one name, usernames and names that begin with @.
// Keys go through the real key path in both keymaps; so do pastes, the
// picker, chat switches, edit mode and the external editor's result.
//
// Deterministic: fixed seeds, the same run every time. MENTION_WALK_SEEDS
// runs more of them.
func TestARandomWalkOfEditsNeverMovesAMentionToAnotherUser(t *testing.T) {
	seeds, steps := 4000, 80
	if s, err := strconv.Atoi(os.Getenv("MENTION_WALK_SEEDS")); err == nil && s > 0 {
		seeds = s
	}
	for seed := range seeds {
		if !newWalk(t, int64(seed)).run(steps) {
			return
		}
	}
}

// walk is one seed's run: the composer, and the shadow the walk keeps of it.
type walk struct {
	t    *testing.T
	seed int64
	rng  *rand.Rand
	m    Model
	vi   bool

	text []rune
	tags []int
	next int // the last tag handed out

	// users are the tags each mentioned user's label was made of. Every
	// mention the walk makes is of a user of its own.
	users    map[int64][]int
	lastUser int64

	// parked is the draft edit mode displaced, which leaving it puts back.
	parked *walkDraft

	log []string
}

// walkDraft is a shadow draft, parked.
type walkDraft struct {
	text  []rune
	tags  []int
	users map[int64][]int
}

func newWalk(t *testing.T, seed int64) *walk {
	w := &walk{
		t:     t,
		seed:  seed,
		rng:   rand.New(rand.NewSource(seed)),
		m:     newFocused(),
		users: map[int64][]int{},
		// Well clear of anything a test or a candidate list uses.
		lastUser: 1000,
	}
	if w.rng.Intn(2) == 0 {
		w.vi = true
		w.m.SetEditingMode(ModeVi)
	}
	w.m.SetMentionsEnabled(true)
	return w
}

// The keys the walk presses: in emacs and vi's insert state, and in vi's
// normal state. Letters of the names are over-represented so that names
// get typed, and repeat.
var (
	walkTypingKeys = []string{
		"A", "l", "e", "x", " ", ",", "@", "@", "@",
		"\x7f", "\x7f", "\x04", "\x1b[3~", // backspace, ctrl+d, delete
		"\x15", "\x0b", "\x17", // ctrl+u, ctrl+k, ctrl+w
		"\x01", "\x05", "\x02", "\x06", // ctrl+a, ctrl+e, ctrl+b, ctrl+f
		"\x1b[A", "\x1b[B", "\x1b[C", "\x1b[C", "\x1b[D", "\x1b[H", "\x1b[F",
		"\n", // ctrl+j
		"\x1b", "\r", "\t",
	}
	walkNormalKeys = []string{
		"h", "l", "j", "k", "k", "w", "b", "0", "$",
		"x", "d", "d", "d", "D", "i", "a", "A", "o", "O",
		"\x1b[A", "\x1b[D", "\r",
	}
	walkPastes = []string{"Alex", "Alex, ", "@A", "x\nAlex", "Al", " ", "😀", "\nA"}
	// walkNames are the members the picker offers: names that repeat and
	// prefix one another, a name starting with @, and a username that is
	// one of the names. "e\u0301x" is a combining mark, two runes for one
	// letter, and "Alex," ends in a character after which @ opens the
	// picker, so an insert can land exactly at a mention's end.
	walkNames = []string{"Alex", "Al", "A", "Alex x", "Алекс😀", "e\u0301x", "@Alex", "@A", "Alex,"}
)

// run takes steps random steps, and reports whether they all held.
func (w *walk) run(steps int) bool {
	for range steps {
		var ok bool
		switch r := w.rng.Intn(100); {
		case r < 6:
			ok = w.paste()
		case r < 9:
			ok = w.switchChats()
		case r < 11:
			ok = w.editor()
		case r < 13 && w.parked == nil:
			ok = w.editMode()
		default:
			ok = w.key()
		}
		if !ok {
			return false
		}
	}
	return true
}

// logf records a step, with the draft as it stood before it.
func (w *walk) logf(format string, args ...any) {
	w.log = append(w.log, fmt.Sprintf(format, args...)+
		fmt.Sprintf("  [draft %q, cursor %d, mentions %v]", w.m.textarea.Value, w.m.textarea.Cursor, w.m.mentions))
}

// check holds the draft against the shadow after op.
func (w *walk) check(op string) bool {
	w.t.Helper()
	if string(w.text) != w.m.textarea.Value {
		w.t.Fatalf("seed %d: the walk misread %s: it expected %q, the draft is %q\n%s",
			w.seed, op, string(w.text), w.m.textarea.Value, strings.Join(w.log, "\n"))
	}
	return w.holds(op, w.m.mentions)
}

// holds reports whether every span lies over the runes its user was
// mentioned on, in order and without overlapping.
func (w *walk) holds(op string, spans []MentionSpan) bool {
	w.t.Helper()
	end := 0
	for _, s := range spans {
		want, known := w.users[s.UserID]
		var under []int
		if s.Start >= 0 && s.Start < s.End && s.End <= len(w.tags) {
			under = w.tags[s.Start:s.End]
		}
		switch {
		case !known:
			w.t.Errorf("seed %d: after %s, a mention of user %d, who was never mentioned in this draft: %+v",
				w.seed, op, s.UserID, s)
		case s.Start < end:
			w.t.Errorf("seed %d: after %s, mentions overlap or are out of order: %+v", w.seed, op, spans)
		case !slices.Equal(under, want) || string(w.text[s.Start:s.End]) != s.Label:
			w.t.Errorf("seed %d: after %s, user %d's mention %+v lies over runes %v of %q, not the runes %v it was made on",
				w.seed, op, s.UserID, s, under, string(w.text), want)
		default:
			end = s.End
			continue
		}
		w.t.Logf("steps:\n%s", strings.Join(w.log, "\n"))
		return false
	}
	return true
}

// insert puts rs into the shadow at at, and returns their tags.
func (w *walk) insert(at int, rs []rune) []int {
	tags := make([]int, len(rs))
	for i := range tags {
		w.next++
		tags[i] = w.next
	}
	w.text = slices.Insert(w.text, at, rs...)
	w.tags = slices.Insert(w.tags, at, tags...)
	return tags
}

// remove takes n runes out of the shadow at at.
func (w *walk) remove(at, n int) {
	w.text = slices.Delete(w.text, at, at+n)
	w.tags = slices.Delete(w.tags, at, at+n)
}

// reset empties the shadow: the draft on screen is a new one.
func (w *walk) reset() {
	w.text, w.tags, w.users = nil, nil, map[int64][]int{}
}

func (w *walk) paste() bool {
	p := walkPastes[w.rng.Intn(len(walkPastes))]
	w.logf("paste %q", p)
	at := w.m.textarea.Cursor
	w.m, _ = w.m.Update(tea.PasteMsg{Content: p})
	w.insert(at, []rune(p))
	return w.check(fmt.Sprintf("pasting %q", p))
}

// switchChats goes to another chat and straight back, which parks the draft
// and restores it — unless there was nothing worth parking.
func (w *walk) switchChats() bool {
	w.logf("chat switch")
	d := draft{text: w.m.textarea.Value, mode: w.m.mode, attachment: w.m.attachment}
	w.m.SetChatId(43)
	w.m.SetChatId(42)
	w.m.SetMentionsEnabled(true)
	if d.empty() {
		w.reset()
	}
	return w.check("a chat switch")
}

// editor brings the draft back from the external editor, as it went or
// with a word added in front.
func (w *walk) editor() bool {
	changed := w.rng.Intn(2) == 0
	w.logf("editor, changed %v", changed)
	text := w.m.textarea.Value
	if changed {
		text = "Alex " + text
	}
	w.m, _ = w.m.Update(editorFinishedMsg{text: text + "\n", ok: true})

	// The editor's trailing newlines are trimmed, and so are the draft's
	// own.
	back := []rune(strings.TrimRight(text, "\n"))
	if !changed && slices.Equal(back, w.text[:len(back)]) {
		w.remove(len(back), len(w.text)-len(back))
	} else {
		// A new text altogether: no rune of it is one the walk made, so
		// no span may survive onto it.
		w.reset()
		w.insert(0, back)
	}
	return w.check(fmt.Sprintf("the editor (changed %v)", changed))
}

// walkMessages are the messages edit mode loads, each mentioning a
// different user at every Alex: the same name twice on a line, and lines
// that start alike or repeat outright.
var walkMessages = []string{"Alex, Alex\nAlex", "Alex x\nAlex x", "hi Alex!\nhi Alex!\nhi Alex!"}

// editMode loads a message mentioning several different users, all called
// Alex, for editing. The draft it displaces comes back when the edit ends.
func (w *walk) editMode() bool {
	text := walkMessages[w.rng.Intn(len(walkMessages))]
	w.logf("edit mode on %q", text)
	w.parked = &walkDraft{text: slices.Clone(w.text), tags: slices.Clone(w.tags), users: w.users}
	w.reset()

	runes := []rune(text)
	tags := w.insert(0, runes)
	var spans []MentionSpan
	for start := range len(runes) - 3 {
		if string(runes[start:start+4]) != "Alex" {
			continue
		}
		w.lastUser++
		w.users[w.lastUser] = tags[start : start+4]
		spans = append(spans, MentionSpan{Start: start, End: start + 4, UserID: w.lastUser, Label: "Alex"})
	}
	w.m.EnterEditMode(9, text, spans...)
	return w.check("entering edit mode")
}

// unpark puts back the draft edit mode displaced.
func (w *walk) unpark() {
	w.text, w.tags, w.users = w.parked.text, w.parked.tags, w.parked.users
	w.parked = nil
}

// candidates are fresh members for the picker to offer, one per name and a
// member whose username is Alex, in a random order.
func (w *walk) candidates() []*telegram.User {
	var out []*telegram.User
	for _, name := range walkNames {
		w.lastUser++
		out = append(out, &telegram.User{ID: w.lastUser, FirstName: name})
	}
	w.lastUser++
	out = append(out, &telegram.User{ID: w.lastUser, FirstName: "Nadia", Username: "Alex"})
	w.rng.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	return out
}

// key presses one random key, and edits the shadow the way it will edit
// the draft.
func (w *walk) key() bool {
	normal := w.m.IsViNormalMode()
	keys := walkTypingKeys
	if normal {
		keys = walkNormalKeys
	}
	key := keys[w.rng.Intn(len(keys))]
	op := fmt.Sprintf("key %q (vi normal %v, pending %q, picker open %v)", key, normal, w.m.viPending, w.m.mention.active)
	w.logf("%s", op)

	text, c := w.text, w.m.textarea.Cursor
	lineStart, lineEnd := c, c
	for lineStart > 0 && text[lineStart-1] != '\n' {
		lineStart--
	}
	for lineEnd < len(text) && text[lineEnd] != '\n' {
		lineEnd++
	}

	// What the key will do to the text: delete n runes at del, then insert
	// ins at at. A mention the picker makes is of user, over label.
	var (
		del, n, at int
		ins        []rune
		user       int64
		label      string
		submit     bool
		unpark     bool
	)
	switch {
	case w.m.mention.active && slices.Contains([]string{"\x1b[A", "\x1b[B", "\r", "\t", "\x1b"}, key):
		// The picker's own keys. Enter and Tab insert whoever is selected
		// among fresh candidates; the rest change nothing in the text.
		if key != "\r" && key != "\t" {
			break
		}
		w.m.SetMentionCandidates(42, w.candidates())
		s := w.m.mention
		if s.selected >= len(s.results) {
			break
		}
		u := s.results[s.selected]
		label, user = displayName(u), u.ID
		if u.Username != "" {
			label, user = "@"+u.Username, 0
		}
		del, n, at, ins = s.anchor, c-s.anchor, s.anchor, []rune(label+" ")

	case normal:
		switch {
		case key == "\r":
			submit = true
		case key == "\x1b":
			unpark = w.m.mode == ModeEdit
		case w.m.viPending == 'd':
			// Only a second d completes the operator; any other key
			// aborts it.
			if key != "d" {
				break
			}
			s, e := lineStart, lineEnd
			if e < len(text) {
				e++
			} else if s > 0 {
				s--
			}
			del, n = s, e-s
		case key == "x" && c < lineEnd:
			del, n = c, 1
		case key == "D":
			del, n = c, lineEnd-c
		case key == "o":
			at, ins = lineEnd, []rune("\n")
		case key == "O":
			at, ins = lineStart, []rune("\n")
		}

	default:
		switch key {
		case "\x7f":
			if c > 0 {
				del, n = c-1, 1
			}
		case "\x04", "\x1b[3~":
			if c < len(text) {
				del, n = c, 1
			}
		case "\x15":
			del, n = lineStart, c-lineStart
		case "\x0b":
			// At the end of a line ctrl+k takes the line break.
			del, n = c, lineEnd-c
			if lineEnd == c && lineEnd < len(text) {
				n = 1
			}
		case "\x17":
			s := c
			for s > 0 && strings.ContainsRune(" \t\n", text[s-1]) {
				s--
			}
			for s > 0 && !strings.ContainsRune(" \t\n", text[s-1]) {
				s--
			}
			del, n = s, c-s
		case "\n":
			at, ins = c, []rune("\n")
		case "\x1b":
			unpark = !w.vi && w.m.mode == ModeEdit
		case "\r":
			submit = true
		case "\t", "\x01", "\x05", "\x02", "\x06", "\x1b[A", "\x1b[B", "\x1b[C", "\x1b[D", "\x1b[H", "\x1b[F":
			// Motions, and a Tab the host would have taken.
		default:
			at, ins = c, []rune(key)
		}
	}
	submit = submit && len(text) > 0

	var msg tea.Msg
	var cmd tea.Cmd
	w.m, cmd = w.m.Update(decodeKey(w.t, key))
	if cmd != nil {
		msg = cmd()
	}

	switch {
	case submit:
		sent, ok := msg.(MessageSubmittedMsg)
		if !ok {
			w.t.Fatalf("seed %d: Enter sent nothing, want a submit\n%s", w.seed, strings.Join(w.log, "\n"))
		}
		if !w.holds("submitting", sent.Mentions) {
			return false
		}
		if sent.EditMessageId != 0 && w.parked != nil {
			w.unpark()
		} else {
			w.reset()
		}
	case unpark && w.parked != nil:
		w.unpark()
	default:
		if n > 0 {
			w.remove(del, n)
		}
		if len(ins) > 0 {
			tags := w.insert(at, ins)
			if user != 0 {
				w.users[user] = tags[:len([]rune(label))]
			}
		}
	}
	return w.check(op)
}
