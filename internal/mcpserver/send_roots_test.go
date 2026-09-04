package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/config"
	"github.com/Ceesaxp/telegram-cli/internal/telegram"
)

// sendRootsHandlers returns tool handlers bound to an unstarted Telegram
// client whose send roots are the returned filesDir and outbox. Unstarted
// is the point: path resolution happens before any RPC, so a rejection
// can be driven through the real handler, while an accepted path would go
// on to dial Telegram.
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

// As in the REST test, the accepting half stops at the resolution layer:
// the handler's next step is an upload and this client is unstarted.
func TestSendFileAcceptsConfiguredRoot(t *testing.T) {
	h, filesDir, outbox := sendRootsHandlers(t)

	for name, dir := range map[string]string{"outbox": outbox, "media cache": filesDir} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, "doc.pdf")
			if err := os.WriteFile(path, []byte("pdf"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := telegram.ResolveAllowedSendPath(path, h.tg.SendRoots()...); err != nil {
				t.Fatalf("path under %s rejected: %v", name, err)
			}
		})
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
