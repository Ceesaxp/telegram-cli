package restapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/config"
	"github.com/Ceesaxp/telegram-cli/internal/telegram"
)

// sendRootsClient returns an unstarted Telegram client bound to a config
// whose send roots are the returned filesDir and outbox. Unstarted is the
// point: path resolution happens before any RPC, so a rejection can be
// driven end-to-end through the real handler, while an accepted path
// would go on to dial Telegram. See TestSendFileAcceptsConfiguredRoot for
// how the accepting half is covered instead.
func sendRootsClient(t *testing.T) (client *telegram.Client, filesDir, outbox string) {
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
	return telegram.NewRPCClient(cfg, telegram.NewTUIAuthorizer(cfg)), filesDir, outbox
}

// The working directory used to be an implicit send root, so whatever
// directory the operator started the server from was readable by every
// caller holding the token (issue #48).
func TestSendFileRejectsPathUnderWorkingDirectory(t *testing.T) {
	client, _, _ := sendRootsClient(t)

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// A file that certainly exists under the process's cwd, so the
	// rejection is about the root and not about the file being missing.
	inCwd := filepath.Join(cwd, "server.go")
	if _, err := os.Stat(inCwd); err != nil {
		t.Fatalf("expected %s to exist for this test: %v", inCwd, err)
	}

	body, _ := json.Marshal(map[string]any{"chat_id": 1, "path": inCwd})
	w := httptest.NewRecorder()
	New(client, testToken).Handler().ServeHTTP(w, authedRequest(http.MethodPost, "/api/send-file", string(body)))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body: %s)", w.Code, http.StatusBadRequest, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "outside the allowed directories") {
		t.Fatalf("body = %s, want an out-of-root rejection", w.Body.String())
	}
}

// The accepting half stops at the resolution layer on purpose: the
// handler's next step is an upload, and the client here is deliberately
// unstarted. This asserts the same function on the same roots the handler
// passes it, which is where the policy lives.
func TestSendFileAcceptsConfiguredRoot(t *testing.T) {
	client, filesDir, outbox := sendRootsClient(t)

	for name, dir := range map[string]string{"outbox": outbox, "media cache": filesDir} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, "doc.pdf")
			if err := os.WriteFile(path, []byte("pdf"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := telegram.ResolveAllowedSendPath(path, client.SendRoots()...); err != nil {
				t.Fatalf("path under %s rejected: %v", name, err)
			}
		})
	}
}

// POST /api/forward names both chats and the messages explicitly. There is
// no "current chat" here: the TUI has a cursor to mean that and an API
// caller does not, so an implicit source would be a different message
// depending on who asked.
func TestForwardRequiresBothChatsAndMessages(t *testing.T) {
	cases := map[string]struct{ body, want string }{
		"no source":      {`{"to_chat_id":2,"message_ids":[7]}`, "from_chat_id"},
		"no destination": {`{"from_chat_id":1,"message_ids":[7]}`, "to_chat_id"},
		"no messages":    {`{"from_chat_id":1,"to_chat_id":2}`, "message_ids"},
		"empty messages": {`{"from_chat_id":1,"to_chat_id":2,"message_ids":[]}`, "message_ids"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			testServer().ServeHTTP(w, authedRequest(http.MethodPost, "/api/forward", tc.body))

			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d (body: %s)", w.Code, http.StatusBadRequest, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), tc.want) {
				t.Fatalf("body = %s, want it to name %s", w.Body.String(), tc.want)
			}
		})
	}
}
