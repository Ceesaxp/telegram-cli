package telegram

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

// sendRoot is a directory to send from, holding file.txt, and a file
// outside it, secret.txt: the one a remote caller must never get.
func sendRoot(t *testing.T) (root, file, secret string) {
	t.Helper()
	root = t.TempDir()
	file = filepath.Join(root, "file.txt")
	if err := os.WriteFile(file, []byte("the file"), 0o600); err != nil {
		t.Fatal(err)
	}
	secret = filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(secret, []byte("the secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	return root, file, secret
}

// openAllowed opens path through a SendRoots made from roots, the way a
// server that started with those roots would.
func openAllowed(path string, roots ...string) (*os.File, error) {
	return OpenSendRoots(roots...).Open(path)
}

// contents reads what f holds and closes it.
func contents(t *testing.T, f *os.File) string {
	t.Helper()
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("read %s: %v", f.Name(), err)
	}
	return string(b)
}

func TestSendRootsOpensAFileInsideARoot(t *testing.T) {
	root, file, _ := sendRoot(t)

	f, err := openAllowed(file, root)
	if err != nil {
		t.Fatalf("Open(%s): %v", file, err)
	}
	// The os.Root it was opened through is closed by now, and the file
	// still reads: it is not tied to the root.
	if got := contents(t, f); got != "the file" {
		t.Fatalf("read %q, want %q", got, "the file")
	}
}

// Every root is searched, in order, past a blank one and past one that
// does not hold the path.
func TestSendRootsSearchesEveryRoot(t *testing.T) {
	root, file, _ := sendRoot(t)

	f, err := openAllowed(file, "", t.TempDir(), root)
	if err != nil {
		t.Fatalf("Open(%s): %v", file, err)
	}
	if got := contents(t, f); got != "the file" {
		t.Fatalf("read %q, want %q", got, "the file")
	}
}

// wantRefused checks that opening path was refused as outside the roots,
// with nothing opened, and that the refusal names every root it searched.
func wantRefused(t *testing.T, f *os.File, err error, path string, roots ...string) {
	t.Helper()
	if err == nil {
		f.Close()
		t.Fatalf("opened %s, want it refused", path)
	}
	if f != nil {
		t.Errorf("refusal returned a file as well as %v", err)
	}
	if !strings.Contains(err.Error(), "outside the allowed directories") {
		t.Fatalf("error = %v, want an out-of-root refusal", err)
	}
	if want := fmt.Sprintf("path %q is outside", path); !strings.Contains(err.Error(), want) {
		t.Errorf("error = %v, want it to name the path as %s", err, want)
	}
	for _, root := range roots {
		if !strings.Contains(err.Error(), root) {
			t.Errorf("error = %v, does not name root %q", err, root)
		}
	}
}

func TestSendRootsRefusesAPathOutsideEveryRoot(t *testing.T) {
	root, _, secret := sendRoot(t)
	other := t.TempDir()

	f, err := openAllowed(secret, root, "", other)

	wantRefused(t, f, err, secret, root, other)
}

// A link is a name inside the root for a file outside it. Written either
// way — absolute, or relative and climbing out — it must not be followed.
func TestSendRootsRefusesASymlinkThatLeavesTheRoot(t *testing.T) {
	root, _, secret := sendRoot(t)
	relative, err := filepath.Rel(root, secret)
	if err != nil {
		t.Fatal(err)
	}

	for name, target := range map[string]string{"absolute": secret, "relative": relative} {
		t.Run(name, func(t *testing.T) {
			link := filepath.Join(root, name+".txt")
			if err := os.Symlink(target, link); err != nil {
				t.Fatal(err)
			}

			f, err := openAllowed(link, root)

			wantRefused(t, f, err, link, root)
		})
	}
}

// A relative link that stays inside the root is followed, through a
// subdirectory and back out of it.
func TestSendRootsFollowsASymlinkThatStaysInside(t *testing.T) {
	root, _, _ := sendRoot(t)
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(sub, "link.txt")
	if err := os.Symlink(filepath.Join("..", "file.txt"), link); err != nil {
		t.Fatal(err)
	}

	f, err := openAllowed(link, root)
	if err != nil {
		t.Fatalf("Open(%s): %v", link, err)
	}
	if got := contents(t, f); got != "the file" {
		t.Fatalf("read %q, want %q", got, "the file")
	}
}

// os.Root refuses every absolute link, including one that names a file
// inside the root. That is its rule rather than this function's, and it
// is pinned here so the narrowing is a decision rather than a surprise.
func TestSendRootsRefusesAnAbsoluteSymlinkEvenIntoTheRoot(t *testing.T) {
	root, file, _ := sendRoot(t)
	link := filepath.Join(root, "link.txt")
	if err := os.Symlink(file, link); err != nil {
		t.Fatal(err)
	}

	f, err := openAllowed(link, root)

	wantRefused(t, f, err, link, root)
}

// wantNotRegular checks that opening path was refused because it is not a
// regular file, with nothing left open.
func wantNotRegular(t *testing.T, f *os.File, err error, path string) {
	t.Helper()
	if err == nil {
		f.Close()
		t.Fatalf("opened %s, want it refused", path)
	}
	if f != nil {
		t.Errorf("refusal returned a file as well as %v", err)
	}
	if want := fmt.Sprintf("%q: not a regular file", path); !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want %s", err, want)
	}
}

// Only a regular file can be sent. A directory used to pass the root check
// and fail later, at the upload; now it fails where the file is opened.
func TestSendRootsRefusesADirectory(t *testing.T) {
	root, _, _ := sendRoot(t)
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}

	for name, path := range map[string]string{"a subdirectory": sub, "the root itself": root} {
		t.Run(name, func(t *testing.T) {
			f, err := openAllowed(path, root)

			wantNotRegular(t, f, err, path)
		})
	}
}

// A configured root may be a link: /tmp is one on macOS, to /private/tmp.
// A path is accepted under the root as written and under where it really
// is, because a caller handed the resolved form of a root path (by a
// shell, or by EvalSymlinks) still means the same directory. The root is
// configuration, not something a caller can swap, so resolving it is not
// the race that resolving the file would be.
func TestSendRootsAcceptsARootThatIsASymlink(t *testing.T) {
	target, _, _ := sendRoot(t)
	root := filepath.Join(t.TempDir(), "root")
	if err := os.Symlink(target, root); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatal(err)
	}

	for name, path := range map[string]string{
		"under the root as configured": filepath.Join(root, "file.txt"),
		"under where it really is":     filepath.Join(resolved, "file.txt"),
	} {
		t.Run(name, func(t *testing.T) {
			f, err := openAllowed(path, root)
			if err != nil {
				t.Fatalf("Open(%s): %v", path, err)
			}
			if got := contents(t, f); got != "the file" {
				t.Fatalf("read %q, want %q", got, "the file")
			}
		})
	}
}

// An allowlist with nothing usable in it must refuse everything. The
// opposite reading — no roots means no restriction — is the failure mode
// issue #48 is about, and it would arrive silently.
func TestSendRootsWithNoUsableRootRefusesEveryPath(t *testing.T) {
	_, file, _ := sendRoot(t)

	for _, roots := range [][]string{nil, {""}, {"", ""}} {
		f, err := openAllowed(file, roots...)

		wantRefused(t, f, err, file)
		if !strings.HasSuffix(err.Error(), "()") {
			t.Errorf("roots %q: error = %v, want it to name no root", roots, err)
		}
	}
}

// A missing file inside a root is missing, not outside: the caller named
// the right directory, and the error has to say what is actually wrong.
func TestSendRootsSaysAMissingFileIsMissing(t *testing.T) {
	root, _, _ := sendRoot(t)
	path := filepath.Join(root, "nope.bin")

	f, err := openAllowed(path, root)
	if err == nil {
		f.Close()
		t.Fatalf("opened %s, want it refused", path)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("error = %v, want one that says the file does not exist", err)
	}
	if strings.Contains(err.Error(), "outside") {
		t.Errorf("error = %v, want no talk of outside for a path that is inside", err)
	}
}

// uploadInvoker is a sendInvoker that also takes file uploads, keeping the
// bytes of every part, so a test can see what a send actually read.
// failUpload makes every part fail instead.
type uploadInvoker struct {
	*sendInvoker
	failUpload bool

	mu       sync.Mutex
	uploaded bytes.Buffer
}

func (f *uploadInvoker) Invoke(ctx context.Context, input bin.Encoder, output bin.Decoder) error {
	req, ok := input.(*tg.UploadSaveFilePartRequest)
	if !ok {
		return f.sendInvoker.Invoke(ctx, input, output)
	}
	if f.failUpload {
		return tgerr.New(400, "FILE_PART_INVALID")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	// The uploader reuses its part buffer, so what is kept is a copy.
	f.uploaded.Write(req.Bytes)
	output.(*tg.BoolBox).Bool = &tg.BoolTrue{}
	return nil
}

// bytes is everything uploaded so far.
func (f *uploadInvoker) bytes() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.uploaded.String()
}

// uploadClient is mentionClient talking to an uploadInvoker.
func uploadClient(t *testing.T) (*Client, *uploadInvoker) {
	t.Helper()
	c, send := mentionClient(t, false)
	inv := &uploadInvoker{sendInvoker: send}
	c.api = tg.NewClient(inv)
	c.peers = peers.Options{}.Build(c.api)
	return c, inv
}

// The race this whole path exists to close, run deterministically: the
// file is checked and opened, then swapped for a link to a secret outside
// the root, then sent. Sending by path would read the secret; sending the
// descriptor reads what was checked.
func TestSendOpenedFileMessageUploadsTheFileThatWasOpened(t *testing.T) {
	root, file, secret := sendRoot(t)
	c, inv := uploadClient(t)

	f, err := openAllowed(file, root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, file); err != nil {
		t.Fatal(err)
	}

	if _, err := c.SendOpenedFileMessage(basicGroupID, f, "", 0, 0); err != nil {
		t.Fatalf("SendOpenedFileMessage: %v", err)
	}

	if got := inv.bytes(); got != "the file" {
		t.Fatalf("uploaded %q, want %q, the file that was opened", got, "the file")
	}
	if len(inv.media) != 1 {
		t.Fatalf("sent %d media, want 1", len(inv.media))
	}
}

// The send owns the descriptor it is handed: it is closed once the send is
// over, and a failed upload is no exception.
func TestSendOpenedFileMessageClosesTheFile(t *testing.T) {
	for name, failUpload := range map[string]bool{"after a send": false, "after a failed upload": true} {
		t.Run(name, func(t *testing.T) {
			root, file, _ := sendRoot(t)
			c, inv := uploadClient(t)
			inv.failUpload = failUpload

			f, err := openAllowed(file, root)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			_, sendErr := c.SendOpenedFileMessage(basicGroupID, f, "", 0, 0)
			if failed := sendErr != nil; failed != failUpload {
				t.Fatalf("send error = %v, want failure %v", sendErr, failUpload)
			}

			if err := f.Close(); !errors.Is(err, os.ErrClosed) {
				t.Fatalf("closing again = %v, want %v: the send left the file open", err, os.ErrClosed)
			}
		})
	}
}

// The chat shows the file under the name the caller asked for — through a
// link, the link's name rather than its target's — and its extension picks
// the MIME type, as it does for a send by path.
func TestSendOpenedFileMessageNamesTheFileAsTheCallerDid(t *testing.T) {
	root, _, _ := sendRoot(t)
	link := filepath.Join(root, "report.md")
	if err := os.Symlink("file.txt", link); err != nil {
		t.Fatal(err)
	}
	c, inv := uploadClient(t)

	f, err := openAllowed(link, root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := c.SendOpenedFileMessage(basicGroupID, f, "", 0, 0); err != nil {
		t.Fatalf("SendOpenedFileMessage: %v", err)
	}

	if len(inv.media) != 1 {
		t.Fatalf("sent %d media, want 1", len(inv.media))
	}
	doc, ok := inv.media[0].Media.(*tg.InputMediaUploadedDocument)
	if !ok {
		t.Fatalf("media = %T, want an uploaded document", inv.media[0].Media)
	}
	want := documentMedia(doc.File, "report.md")
	if doc.MimeType != want.MimeType {
		t.Errorf("MIME type = %q, want %q", doc.MimeType, want.MimeType)
	}
	if !reflect.DeepEqual(doc.Attributes, want.Attributes) {
		t.Errorf("attributes = %#v, want %#v", doc.Attributes, want.Attributes)
	}
	if file, ok := doc.File.(*tg.InputFile); !ok || file.Name != "report.md" {
		t.Errorf("uploaded file = %#v, want one named %q", doc.File, "report.md")
	}
}

// A send from a descriptor carries what a send by path does: the caption,
// its mentions, and the message it replies to.
func TestSendOpenedFileMessageCarriesCaptionMentionsAndReply(t *testing.T) {
	root, file, _ := sendRoot(t)
	c, inv := uploadClient(t)

	f, err := openAllowed(file, root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	_, dropped, err := c.SendOpenedFileMessageWithMentions(basicGroupID, f, "hi Nadia",
		[]MentionSpan{{Offset: 3, Length: 5, UserID: nadia}}, 42, 0)
	if err != nil {
		t.Fatalf("SendOpenedFileMessageWithMentions: %v", err)
	}

	if dropped != 0 {
		t.Errorf("dropped %d mentions, want none", dropped)
	}
	if len(inv.media) != 1 {
		t.Fatalf("sent %d media, want 1", len(inv.media))
	}
	got := inv.media[0]
	if got.Message != "hi Nadia" {
		t.Errorf("caption = %q, want %q", got.Message, "hi Nadia")
	}
	if want := []tg.MessageEntityClass{mentionOf(3, 5)}; !reflect.DeepEqual(got.Entities, want) {
		t.Errorf("entities = %s, want %s", describe(got.Entities), describe(want))
	}
	if want := (&tg.InputReplyToMessage{ReplyToMsgID: 42}); !reflect.DeepEqual(got.ReplyTo, want) {
		t.Errorf("reply to = %#v, want %#v", got.ReplyTo, want)
	}
}

// unclean joins parts as a caller might type them: filepath.Join would
// clean the ".." away before the function under test ever saw it.
func unclean(parts ...string) string {
	return strings.Join(parts, string(filepath.Separator))
}

// ".." is refused whether it is spelled out in the path or hidden behind a
// directory link partway along it.
func TestSendRootsRefusesDotDotEscapes(t *testing.T) {
	root, _, secret := sendRoot(t)
	outside := filepath.Dir(secret)
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	up, err := filepath.Rel(root, outside)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(up, filepath.Join(root, "up")); err != nil {
		t.Fatal(err)
	}

	for name, path := range map[string]string{
		"spelled out":             unclean(sub, "..", "..", filepath.Base(outside), "secret.txt"),
		"behind a directory link": filepath.Join(root, "up", "secret.txt"),
	} {
		t.Run(name, func(t *testing.T) {
			f, err := openAllowed(path, root)

			wantRefused(t, f, err, path, root)
		})
	}

	// And one that climbs but never leaves is just a path.
	t.Run("staying inside", func(t *testing.T) {
		path := unclean(sub, "..", "file.txt")
		f, err := openAllowed(path, root)
		if err != nil {
			t.Fatalf("Open(%s): %v", path, err)
		}
		if got := contents(t, f); got != "the file" {
			t.Fatalf("read %q, want %q", got, "the file")
		}
	})
}
