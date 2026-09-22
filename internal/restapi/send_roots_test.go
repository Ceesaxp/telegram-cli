package restapi

import (
	"encoding/json"
	"io"
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
// point: the file is opened and checked before any RPC, so a rejection can
// be driven end-to-end through the real handler. An accepted file would go
// on to dial Telegram, so those tests put a fakeFileSender in its place.
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
	sendRootsServer(t, client).Handler().ServeHTTP(w, authedRequest(http.MethodPost, "/api/send-file", string(body)))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body: %s)", w.Code, http.StatusBadRequest, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "outside the allowed directories") {
		t.Fatalf("body = %s, want an out-of-root rejection", w.Body.String())
	}
}

// fakeFileSender stands in for Telegram behind the send-file handler. It
// runs swap first — the moment between the handler's check and the upload
// — then reads the file it was handed, the way the upload would.
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

// sendRootsServer is the REST server as telegram-api starts it: bound to
// client, with the send roots client's config names.
func sendRootsServer(t *testing.T, client *telegram.Client) *Server {
	t.Helper()
	srv := New(client, testToken)
	roots, _ := telegram.OpenSendRoots(client.SendRoots()...)
	t.Cleanup(func() { roots.Close() })
	srv.SetSendRoots(roots)
	return srv
}

// postSendFile drives POST /api/send-file through the real handler, with
// sender standing in for Telegram.
func postSendFile(t *testing.T, client *telegram.Client, sender fileSender, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	srv := sendRootsServer(t, client)
	srv.files = sender
	raw, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, authedRequest(http.MethodPost, "/api/send-file", string(raw)))
	return w
}

// A path under either root goes through the handler to the send, with the
// request's chat, caption and reply.
func TestSendFileAcceptsConfiguredRoot(t *testing.T) {
	client, filesDir, outbox := sendRootsClient(t)

	for name, dir := range map[string]string{"outbox": outbox, "media cache": filesDir} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, "doc.pdf")
			if err := os.WriteFile(path, []byte("pdf"), 0o600); err != nil {
				t.Fatal(err)
			}
			sender := &fakeFileSender{}

			w := postSendFile(t, client, sender, map[string]any{
				"chat_id": 7, "path": path, "caption": "hi", "reply_to_message_id": 3,
			})

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d (body: %s)", w.Code, http.StatusOK, w.Body.String())
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
	client, _, outbox := sendRootsClient(t)
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

	w := postSendFile(t, client, sender, map[string]any{"chat_id": 7, "path": path})

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body: %s)", w.Code, http.StatusOK, w.Body.String())
	}
	if sender.read != "the file" {
		t.Fatalf("sent %q, want %q, the file that was checked", sender.read, "the file")
	}
}

// A directory is refused where it is opened, as a bad request, and never
// reaches the send.
func TestSendFileRefusesADirectory(t *testing.T) {
	client, _, outbox := sendRootsClient(t)
	sender := &fakeFileSender{}

	w := postSendFile(t, client, sender, map[string]any{"chat_id": 7, "path": outbox})

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body: %s)", w.Code, http.StatusBadRequest, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "not a regular file") {
		t.Fatalf("body = %s, want a not-a-regular-file refusal", w.Body.String())
	}
	if sender.calls != 0 {
		t.Fatalf("the send was reached %d times, want never", sender.calls)
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
