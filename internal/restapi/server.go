// Package restapi exposes the Telegram account as a JSON REST API.
// It uses only the standard library (net/http, Go 1.22+ method patterns).
package restapi

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/gotd/td/telegram/peers"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/Ceesaxp/telegram-cli/internal/tgjson"
)

// Server is the REST API handler set bound to a Telegram client.
type Server struct {
	tg  *telegram.Client
	mux *http.ServeMux

	// token is the bearer token required on every route except
	// GET /api/health. An empty token disables authentication; callers
	// must only pass "" when they explicitly opt in (e.g.
	// --insecure-no-auth). New() has no way to enforce that itself, so
	// the guarantee lives in the caller: cmd/telegram-api's resolveToken
	// calls log.Fatal rather than ever returning an empty, non-opted-in
	// token to New().
	token string

	// extraHosts holds additional lowercase hostnames accepted by the
	// Host/Origin checks, beyond the always-allowed localhost/127.0.0.1/
	// ::1: the configured listen host (via SetListenHost, when it names a
	// specific, non-wildcard address) and any operator-supplied
	// AddAllowedHost values. A wildcard bind address (0.0.0.0, ::, or no
	// host at all) never lands here automatically — explicit allowlisting
	// only, so exposing the server beyond loopback requires an operator
	// to opt in per-hostname.
	extraHosts map[string]struct{}
}

// New creates the REST API server and registers all routes. token is the
// bearer token required on every route except GET /api/health; pass ""
// only when the caller explicitly wants authentication disabled.
func New(client *telegram.Client, token string) *Server {
	s := &Server{tg: client, token: token}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/me", s.me)
	mux.HandleFunc("GET /api/chats", s.chats)
	mux.HandleFunc("GET /api/chats/{id}/history", s.chatHistory)
	mux.HandleFunc("GET /api/search/chats", s.searchChats)
	mux.HandleFunc("GET /api/search/messages", s.searchMessages)
	mux.HandleFunc("GET /api/contacts", s.contacts)
	mux.HandleFunc("POST /api/send", s.send)
	mux.HandleFunc("POST /api/send-file", s.sendFile)
	mux.HandleFunc("POST /api/edit", s.edit)
	mux.HandleFunc("POST /api/mark-read", s.markRead)
	mux.HandleFunc("GET /api/media", s.media)
	// JSON 404 for anything else.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found: "+r.Method+" "+r.URL.Path)
	})
	s.mux = mux
	return s
}

// SetListenHost records the host portion of the address the server will
// listen on (e.g. "192.168.1.5" from "192.168.1.5:8080") as an additional
// allowed Host/Origin, alongside localhost/127.0.0.1/[::1]. Call this
// before serving requests; it is safe to call at any point before that.
//
// Wildcard bind addresses (0.0.0.0, ::, or no host at all, e.g. "-addr
// :8080") are deliberately NOT auto-allowed: binding wide open must not
// silently accept every Host header, or the Host check stops defending
// against DNS rebinding. Exposing the server beyond loopback requires an
// explicit AddAllowedHost call (wired to --allowed-host in
// cmd/telegram-api).
func (s *Server) SetListenHost(addr string) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	s.addAllowedHost(host)
}

// AddAllowedHost adds host (accepted with any port) to the set of
// Host/Origin values allowed in addition to localhost/127.0.0.1/[::1].
// Call once per operator-supplied --allowed-host value. Wildcard
// addresses (0.0.0.0, ::) are rejected even when passed explicitly: a
// browser can be tricked into treating 0.0.0.0 as a synonym for
// localhost (the "0.0.0.0 day" class of bug), so it must never become an
// accepted Host value.
func (s *Server) AddAllowedHost(host string) {
	s.addAllowedHost(strings.TrimSpace(host))
}

func (s *Server) addAllowedHost(host string) {
	host = stripBrackets(host)
	if host == "" || isWildcardHost(host) {
		return
	}
	if s.extraHosts == nil {
		s.extraHosts = make(map[string]struct{})
	}
	s.extraHosts[strings.ToLower(host)] = struct{}{}
}

// AllowedHosts returns the full effective Host/Origin allowlist —
// localhost/127.0.0.1/::1 plus every host added via SetListenHost or
// AddAllowedHost — sorted, for startup logging and for the diagnostic
// body returned on a 403.
func (s *Server) AllowedHosts() []string {
	set := map[string]struct{}{"localhost": {}, "127.0.0.1": {}, "::1": {}}
	for h := range s.extraHosts {
		set[h] = struct{}{}
	}
	list := make([]string, 0, len(set))
	for h := range set {
		list = append(list, h)
	}
	sort.Strings(list)
	return list
}

// isWildcardHost reports whether host is an "any address" bind host that
// must never be treated as an allowed Host/Origin value.
func isWildcardHost(host string) bool {
	switch host {
	case "0.0.0.0", "::", "0:0:0:0:0:0:0:0":
		return true
	}
	return false
}

// Handler returns the root HTTP handler, wrapped with the security
// middleware (Host/Origin validation, Content-Type enforcement, and
// bearer-token authentication).
func (s *Server) Handler() http.Handler {
	return s.secure(s.mux)
}

// secure wraps next with cross-origin hardening and authentication.
// Order: Host validation, then Origin validation, then Content-Type
// enforcement for state-changing requests, then bearer-token auth (every
// route except GET /api/health). Rejecting on Host/Origin before auth
// means a browser-driven cross-origin or DNS-rebinding request never
// even gets to try a token.
func (s *Server) secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.hostAllowed(r.Host) {
			s.writeForbiddenHost(w, "forbidden host")
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && !s.originAllowed(origin) {
			s.writeForbiddenHost(w, "forbidden origin")
			return
		}
		if r.Method == http.MethodPost && !isJSONContentType(r.Header.Get("Content-Type")) {
			writeError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
			return
		}
		if !(r.Method == http.MethodGet && r.URL.Path == "/api/health") && !s.authorized(r) {
			writeError(w, http.StatusUnauthorized, "missing or invalid bearer token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// authorized reports whether r carries the correct bearer token. When no
// token is configured, authentication is disabled and every request is
// authorized. The comparison is done on fixed-length SHA-256 digests via
// crypto/subtle.ConstantTimeCompare so it leaks neither the token value
// nor, via early-exit on length, its length.
func (s *Server) authorized(r *http.Request) bool {
	if s.token == "" {
		return true
	}
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, prefix) {
		return false
	}
	supplied := strings.TrimPrefix(h, prefix)
	want := sha256.Sum256([]byte(s.token))
	got := sha256.Sum256([]byte(supplied))
	return subtle.ConstantTimeCompare(want[:], got[:]) == 1
}

// hostAllowed reports whether hostHeader (the request's Host, e.g. from
// http.Request.Host) names localhost, 127.0.0.1, ::1 (any of these with
// any port), or the exact configured listen host.
func (s *Server) hostAllowed(hostHeader string) bool {
	return s.hostnameAllowed(hostOnly(hostHeader))
}

// originAllowed reports whether origin (an Origin header value, e.g.
// "https://evil.example:443") names a localhost origin or the exact
// configured listen host.
func (s *Server) originAllowed(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	return s.hostnameAllowed(u.Hostname())
}

func (s *Server) hostnameAllowed(host string) bool {
	host = strings.ToLower(host)
	switch host {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	_, ok := s.extraHosts[host]
	return ok
}

// writeForbiddenHost writes a 403 whose body names the effective
// Host/Origin allowlist, so a rejected client (or its operator) can see
// exactly what's accepted instead of guessing.
func (s *Server) writeForbiddenHost(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusForbidden, errorOut{
		Error:        msg,
		AllowedHosts: s.AllowedHosts(),
	})
}

// hostOnly extracts the hostname from a Host header value: it strips a
// trailing ":port" if present, then strips IPv6 brackets if present (a
// bracketed literal with no port, e.g. "[::1]", has no port for
// net.SplitHostPort to remove, so it needs a second pass).
func hostOnly(hostHeader string) string {
	if h, _, err := net.SplitHostPort(hostHeader); err == nil {
		return h
	}
	return stripBrackets(hostHeader)
}

// stripBrackets removes a matching "[...]" pair around a bracketed IPv6
// literal, e.g. "[::1]" -> "::1". Left unchanged if not bracketed.
func stripBrackets(host string) string {
	if len(host) >= 2 && host[0] == '[' && host[len(host)-1] == ']' {
		return host[1 : len(host)-1]
	}
	return host
}

// isJSONContentType reports whether ct names the application/json media
// type, ignoring parameters such as charset.
func isJSONContentType(ct string) bool {
	mt, _, err := mime.ParseMediaType(ct)
	return err == nil && mt == "application/json"
}

// --- Response types ---

type errorOut struct {
	Error string `json:"error"`
	// AllowedHosts is populated only on a Host/Origin 403, listing the
	// effective allowlist so the rejection is self-diagnosing.
	AllowedHosts []string `json:"allowed_hosts,omitempty"`
}

type userOut struct {
	User tgjson.ContactInfo `json:"user"`
}

type chatsOut struct {
	Chats []tgjson.ChatInfo `json:"chats"`
}

type messagesOut struct {
	Messages []tgjson.MessageInfo `json:"messages"`
}

type contactsOut struct {
	Contacts []tgjson.ContactInfo `json:"contacts"`
}

type messageOut struct {
	Message tgjson.MessageInfo `json:"message"`
}

type statusOut struct {
	Status string `json:"status"`
}

type mediaOut struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
	Name string `json:"name,omitempty"`
}

// --- Request types ---

type sendIn struct {
	ChatID           int64  `json:"chat_id"`
	Text             string `json:"text"`
	ReplyToMessageID int64  `json:"reply_to_message_id,omitempty"`
}

type sendFileIn struct {
	ChatID           int64  `json:"chat_id"`
	Path             string `json:"path"`
	Caption          string `json:"caption,omitempty"`
	ReplyToMessageID int64  `json:"reply_to_message_id,omitempty"`
}

type editIn struct {
	ChatID    int64  `json:"chat_id"`
	MessageID int64  `json:"message_id"`
	Text      string `json:"text"`
}

type markReadIn struct {
	ChatID     int64   `json:"chat_id"`
	MessageIDs []int64 `json:"message_ids"`
}

// --- Handlers ---

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, statusOut{Status: "ok"})
}

func (s *Server) me(w http.ResponseWriter, _ *http.Request) {
	me, err := s.tg.GetMe()
	if err != nil {
		writeTelegramError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, userOut{User: tgjson.ToContactInfo(me)})
}

func (s *Server) chats(w http.ResponseWriter, r *http.Request) {
	limit := intQuery(r, "limit", 50)
	chats, err := s.tg.ListChats(limit)
	if err != nil {
		writeTelegramError(w, err)
		return
	}
	out := chatsOut{Chats: make([]tgjson.ChatInfo, 0, len(chats))}
	for _, chat := range chats {
		out.Chats = append(out.Chats, tgjson.ToChatInfo(chat))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) chatHistory(w http.ResponseWriter, r *http.Request) {
	chatID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid chat id")
		return
	}
	limit := int32(intQuery(r, "limit", 50))
	fromID := int64(intQuery(r, "from_message_id", 0))
	offset := int32(intQuery(r, "offset", 0))

	msgs, err := s.tg.GetChatHistory(chatID, fromID, offset, limit)
	if err != nil {
		writeTelegramError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toMessagesOut(msgs))
}

func (s *Server) searchChats(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, http.StatusBadRequest, "missing required parameter: q")
		return
	}
	limit := int32(intQuery(r, "limit", 20))

	chats, err := s.tg.SearchChats(q, limit)
	if err != nil {
		writeTelegramError(w, err)
		return
	}
	out := chatsOut{Chats: make([]tgjson.ChatInfo, 0, len(chats))}
	for _, chat := range chats {
		out.Chats = append(out.Chats, tgjson.ToChatInfo(chat))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) searchMessages(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, http.StatusBadRequest, "missing required parameter: q")
		return
	}
	limit := int32(intQuery(r, "limit", 20))

	msgs, err := s.tg.SearchMessages(q, limit)
	if err != nil {
		writeTelegramError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toMessagesOut(msgs))
}

func (s *Server) contacts(w http.ResponseWriter, _ *http.Request) {
	users, err := s.tg.GetContacts()
	if err != nil {
		writeTelegramError(w, err)
		return
	}
	out := contactsOut{Contacts: make([]tgjson.ContactInfo, 0, len(users))}
	for _, u := range users {
		out.Contacts = append(out.Contacts, tgjson.ToContactInfo(u))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) send(w http.ResponseWriter, r *http.Request) {
	var in sendIn
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.ChatID == 0 {
		writeError(w, http.StatusBadRequest, "missing required field: chat_id")
		return
	}
	if in.Text == "" {
		writeError(w, http.StatusBadRequest, "missing required field: text")
		return
	}

	msg, err := s.tg.SendTextMessage(in.ChatID, in.Text, in.ReplyToMessageID)
	if err != nil {
		writeTelegramError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, messageOut{Message: tgjson.ToMessageInfo(msg)})
}

func (s *Server) sendFile(w http.ResponseWriter, r *http.Request) {
	var in sendFileIn
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.ChatID == 0 {
		writeError(w, http.StatusBadRequest, "missing required field: chat_id")
		return
	}
	if in.Path == "" {
		writeError(w, http.StatusBadRequest, "missing required field: path")
		return
	}

	cwd, err := os.Getwd()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot determine working directory")
		return
	}
	path, err := telegram.ResolveAllowedSendPath(in.Path, s.tg.FilesDir(), cwd)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	msg, err := s.tg.SendFileMessage(in.ChatID, path, in.Caption, in.ReplyToMessageID)
	if err != nil {
		writeTelegramError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, messageOut{Message: tgjson.ToMessageInfo(msg)})
}

func (s *Server) edit(w http.ResponseWriter, r *http.Request) {
	var in editIn
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.ChatID == 0 {
		writeError(w, http.StatusBadRequest, "missing required field: chat_id")
		return
	}
	if in.MessageID == 0 {
		writeError(w, http.StatusBadRequest, "missing required field: message_id")
		return
	}
	if in.Text == "" {
		writeError(w, http.StatusBadRequest, "missing required field: text")
		return
	}

	msg, err := s.tg.EditTextMessage(in.ChatID, in.MessageID, in.Text)
	if err != nil {
		writeTelegramError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, messageOut{Message: tgjson.ToMessageInfo(msg)})
}

func (s *Server) markRead(w http.ResponseWriter, r *http.Request) {
	var in markReadIn
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.ChatID == 0 {
		writeError(w, http.StatusBadRequest, "missing required field: chat_id")
		return
	}
	if len(in.MessageIDs) == 0 {
		writeError(w, http.StatusBadRequest, "missing required field: message_ids")
		return
	}

	if err := s.tg.ViewMessages(in.ChatID, in.MessageIDs); err != nil {
		writeTelegramError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, statusOut{Status: "ok"})
}

func (s *Server) media(w http.ResponseWriter, r *http.Request) {
	chatID, err := strconv.ParseInt(r.URL.Query().Get("chat_id"), 10, 64)
	if err != nil || chatID == 0 {
		writeError(w, http.StatusBadRequest, "missing or invalid parameter: chat_id")
		return
	}
	messageID, err := strconv.ParseInt(r.URL.Query().Get("message_id"), 10, 64)
	if err != nil || messageID == 0 {
		writeError(w, http.StatusBadRequest, "missing or invalid parameter: message_id")
		return
	}

	msg, err := s.tg.GetMessage(chatID, messageID)
	if err != nil {
		writeTelegramError(w, err)
		return
	}

	key, name, _ := tgjson.MediaFile(msg)
	if key == "" {
		writeError(w, http.StatusBadRequest, "message has no downloadable media")
		return
	}

	file, err := s.tg.DownloadFileSync(key)
	if err != nil {
		writeTelegramError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, mediaOut{Path: file.Path, Size: file.Size, Name: name})
}

// --- Helpers ---

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, errorOut{Error: msg})
}

// writeTelegramError maps an upstream error to a status code:
// unknown chat/peer → 404, everything else → 502.
func writeTelegramError(w http.ResponseWriter, err error) {
	var notFound *peers.PeerNotFoundError
	if errors.As(err, &notFound) {
		writeError(w, http.StatusNotFound, "chat not found")
		return
	}
	writeError(w, http.StatusBadGateway, "telegram error: "+err.Error())
}

func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	limited := http.MaxBytesReader(w, r.Body, 1<<20) // 1 MiB
	if err := json.NewDecoder(limited).Decode(v); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusBadRequest, "request body too large")
			return false
		}
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

func intQuery(r *http.Request, name string, def int) int {
	v := r.URL.Query().Get(name)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func toMessagesOut(msgs []*telegram.Message) messagesOut {
	out := messagesOut{Messages: make([]tgjson.MessageInfo, 0, len(msgs))}
	for _, m := range msgs {
		out.Messages = append(out.Messages, tgjson.ToMessageInfo(m))
	}
	return out
}
