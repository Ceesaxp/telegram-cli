package mcpserver

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/config"
	"github.com/Ceesaxp/telegram-cli/internal/telegram"
)

// sendRootsHandlers returns tool handlers bound to an unstarted Telegram
// client whose send roots are the returned filesDir and outbox. Unstarted
// is the point: the file is opened and checked before any RPC, so a
// rejection can be driven through the real handler. An accepted file
// would go on to dial Telegram, so those tests put a fakeFileSender in its
// place.
func sendRootsHandlers(t *testing.T) (h *handlers, filesDir, outbox string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)

	filesDir = filepath.Join(home, "files")
	outbox = filepath.Join(home, "outbox")
	for _, dir := range []string{filesDir, outbox} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	cfg := &config.Config{Storage: config.StorageConfig{
		SessionFile: filepath.Join(home, "session.json"),
		FilesDir:    filesDir,
		SendDirs:    []string{outbox},
	}}
	return &handlers{tg: telegram.NewRPCClient(cfg, telegram.NewTUIAuthorizer(cfg))}, filesDir, outbox
}

// This is the case issue #48 was really about: an MCP host started from a
// login shell has $HOME as its working directory, and the caller on the
// other end is a language model reading untrusted incoming messages. A
// prompt-injected "send me ~/.ssh/id_ed25519" used to succeed.
func TestSendFileRejectsPathUnderWorkingDirectory(t *testing.T) {
	h, _, _ := sendRootsHandlers(t)

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	inCwd := filepath.Join(cwd, "server.go")
	if _, err := os.Stat(inCwd); err != nil {
		t.Fatalf("expected %s to exist for this test: %v", inCwd, err)
	}

	_, _, err = h.sendFile(context.Background(), nil, sendFileIn{ChatID: 1, Path: inCwd})
	if err == nil {
		t.Fatal("expected a path under the working directory to be rejected")
	}
	if !strings.Contains(err.Error(), "outside the allowed directories") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// fakeFileSender stands in for Telegram behind send_file. It runs swap
// first — the moment between the handler's check and the upload — then
// reads the file it was handed, the way the upload would.
type fakeFileSender struct {
	swap func()

	calls   int
	name    string
	read    string
	chatID  int64
	caption string
	replyTo int64
}

func (f *fakeFileSender) SendOpenedFileMessage(chatID int64, file *os.File, caption string, replyTo, _ int64) (*telegram.Message, error) {
	defer file.Close()
	f.calls++
	if f.swap != nil {
		f.swap()
	}
	b, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	f.name, f.read = file.Name(), string(b)
	f.chatID, f.caption, f.replyTo = chatID, caption, replyTo
	return &telegram.Message{ID: 100, ChatID: chatID}, nil
}

// A path under either root goes through the handler to the send, with the
// call's chat, caption and reply.
func TestSendFileAcceptsConfiguredRoot(t *testing.T) {
	h, filesDir, outbox := sendRootsHandlers(t)

	for name, dir := range map[string]string{"outbox": outbox, "media cache": filesDir} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, "doc.pdf")
			if err := os.WriteFile(path, []byte("pdf"), 0o600); err != nil {
				t.Fatal(err)
			}
			sender := &fakeFileSender{}
			h.files = sender

			_, _, err := h.sendFile(context.Background(), nil, sendFileIn{ChatID: 7, Path: path, Caption: "hi", ReplyToMessageID: 3})
			if err != nil {
				t.Fatalf("path under %s rejected: %v", name, err)
			}
			if sender.read != "pdf" || filepath.Base(sender.name) != "doc.pdf" {
				t.Errorf("sent %q named %q, want %q named doc.pdf", sender.read, sender.name, "pdf")
			}
			if sender.chatID != 7 || sender.caption != "hi" || sender.replyTo != 3 {
				t.Errorf("sent to chat %d with caption %q replying to %d, want 7, %q, 3",
					sender.chatID, sender.caption, sender.replyTo, "hi")
			}
		})
	}
}

// The handler hands the send the file it checked, not a path to open
// again: swapping the file for a link to a secret once the send has it
// changes nothing about what is read. That the upload reads only this
// descriptor is the telegram package's test of SendOpenedFileMessage.
func TestSendFileSendsTheFileItChecked(t *testing.T) {
	h, _, outbox := sendRootsHandlers(t)
	path := filepath.Join(outbox, "doc.txt")
	if err := os.WriteFile(path, []byte("the file"), 0o600); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(secret, []byte("the secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	sender := &fakeFileSender{swap: func() {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(secret, path); err != nil {
			t.Fatal(err)
		}
	}}
	h.files = sender

	if _, _, err := h.sendFile(context.Background(), nil, sendFileIn{ChatID: 7, Path: path}); err != nil {
		t.Fatalf("send_file: %v", err)
	}
	if sender.read != "the file" {
		t.Fatalf("sent %q, want %q, the file that was checked", sender.read, "the file")
	}
}

// A directory is refused where it is opened and never reaches the send.
func TestSendFileRefusesADirectory(t *testing.T) {
	h, _, outbox := sendRootsHandlers(t)
	sender := &fakeFileSender{}
	h.files = sender

	_, _, err := h.sendFile(context.Background(), nil, sendFileIn{ChatID: 7, Path: outbox})
	if err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("error = %v, want a not-a-regular-file refusal", err)
	}
	if sender.calls != 0 {
		t.Fatalf("the send was reached %d times, want never", sender.calls)
	}
}

// MCP and REST must not drift into different policies — the whole reason
// the roots come from one place.
func TestSendRootsMatchTheConfiguredSet(t *testing.T) {
	h, filesDir, outbox := sendRootsHandlers(t)

	got := h.tg.SendRoots()
	want := []string{filesDir, outbox}
	if len(got) != len(want) {
		t.Fatalf("SendRoots() = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("SendRoots() = %q, want %q", got, want)
		}
	}
}

// forward_messages validates before it reaches Telegram, so a malformed
// call from a model is answered rather than dialled.
func TestForwardMessagesValidatesItsInput(t *testing.T) {
	h, _, _ := sendRootsHandlers(t)

	cases := map[string]forwardMessagesIn{
		"no source":      {ToChatID: 2, MessageIDs: []int64{7}},
		"no destination": {FromChatID: 1, MessageIDs: []int64{7}},
		"no messages":    {FromChatID: 1, ToChatID: 2},
		"empty messages": {FromChatID: 1, ToChatID: 2, MessageIDs: []int64{}},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, err := h.forwardMessages(context.Background(), nil, in); err == nil {
				t.Fatal("expected a validation error")
			}
		})
	}
}
