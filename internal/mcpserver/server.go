// Package mcpserver exposes the Telegram account as MCP tools over stdio.
package mcpserver

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/Ceesaxp/telegram-cli/internal/tgjson"
)

// Server wraps an MCP server bound to a Telegram client.
type Server struct {
	mcp *mcp.Server
}

// Tool input/output types. Schemas are inferred by the SDK from these
// structs (json/jsonschema tags), so keep them flat and documented.

type getMeIn struct{}

type getMeOut struct {
	User tgjson.ContactInfo `json:"user"`
}

type listChatsIn struct {
	Limit int `json:"limit,omitempty" jsonschema:"max chats to return (default 50, max 100)"`
}

type listChatsOut struct {
	Chats []tgjson.ChatInfo `json:"chats"`
}

type getChatHistoryIn struct {
	ChatID        int64 `json:"chat_id" jsonschema:"canonical chat ID from list_chats"`
	Limit         int32 `json:"limit,omitempty" jsonschema:"max messages to return (default 50, max 100)"`
	FromMessageID int64 `json:"from_message_id,omitempty" jsonschema:"paginate backwards starting before this message ID"`
	Offset        int32 `json:"offset,omitempty" jsonschema:"skip this many messages"`
}

type messagesOut struct {
	Messages []tgjson.MessageInfo `json:"messages"`
}

type searchChatsIn struct {
	Query string `json:"query" jsonschema:"search text"`
	Limit int32  `json:"limit,omitempty" jsonschema:"max results (default 20)"`
}

type searchMessagesIn struct {
	Query string `json:"query" jsonschema:"search text"`
	Limit int32  `json:"limit,omitempty" jsonschema:"max results (default 20, max 100)"`
}

type getContactsIn struct{}

type getContactsOut struct {
	Contacts []tgjson.ContactInfo `json:"contacts"`
}

type sendMessageIn struct {
	ChatID           int64  `json:"chat_id" jsonschema:"canonical chat ID from list_chats"`
	Text             string `json:"text" jsonschema:"message text"`
	ReplyToMessageID int64  `json:"reply_to_message_id,omitempty" jsonschema:"reply to this message ID"`
}

type messageOut struct {
	Message tgjson.MessageInfo `json:"message"`
}

type sendFileIn struct {
	ChatID           int64  `json:"chat_id" jsonschema:"canonical chat ID from list_chats"`
	Path             string `json:"path" jsonschema:"local filesystem path of the file to send; must already exist and be inside one of the server's configured send roots (the media cache plus storage.send_dirs, logged at startup) — paths elsewhere on the filesystem are rejected"`
	Caption          string `json:"caption,omitempty" jsonschema:"optional caption text sent with the file"`
	ReplyToMessageID int64  `json:"reply_to_message_id,omitempty" jsonschema:"reply to this message ID"`
}

type forwardMessagesIn struct {
	FromChatID int64   `json:"from_chat_id" jsonschema:"canonical chat ID the messages are in now"`
	ToChatID   int64   `json:"to_chat_id" jsonschema:"canonical chat ID to forward them to"`
	MessageIDs []int64 `json:"message_ids" jsonschema:"IDs of the messages to forward"`
}

type editMessageIn struct {
	ChatID    int64  `json:"chat_id" jsonschema:"canonical chat ID"`
	MessageID int64  `json:"message_id" jsonschema:"ID of the message to edit"`
	Text      string `json:"text" jsonschema:"new message text"`
}

type markReadIn struct {
	ChatID     int64   `json:"chat_id" jsonschema:"canonical chat ID"`
	MessageIDs []int64 `json:"message_ids" jsonschema:"message IDs to mark as read"`
}

type statusOut struct {
	Status string `json:"status"`
}

type downloadMediaIn struct {
	ChatID    int64 `json:"chat_id" jsonschema:"canonical chat ID"`
	MessageID int64 `json:"message_id" jsonschema:"ID of the message carrying media"`
}

type downloadMediaOut struct {
	Path string `json:"path" jsonschema:"local path of the downloaded file"`
	Size int64  `json:"size"`
	Name string `json:"name,omitempty"`
}

// New creates an MCP server exposing the Telegram client as tools.
func New(client *telegram.Client) *Server {
	s := mcp.NewServer(&mcp.Implementation{
		Name:    "telegram-mcp",
		Title:   "Telegram MCP",
		Version: "0.1.0",
	}, nil)

	h := &handlers{tg: client}

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_me",
		Description: "Get the authorized Telegram user (id, name, username, phone)",
	}, h.getMe)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_chats",
		Description: "List chats/dialogs (pinned first, then most recent)",
	}, h.listChats)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_chat_history",
		Description: "Get messages of a chat, newest first",
	}, h.getChatHistory)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "search_chats",
		Description: "Search chats by title/username",
	}, h.searchChats)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "search_messages",
		Description: "Search messages globally by text",
	}, h.searchMessages)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_contacts",
		Description: "List Telegram contacts",
	}, h.getContacts)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "send_message",
		Description: "Send a text message to a chat",
	}, h.sendMessage)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "send_file",
		Description: "Upload a local file and send it as a document to a chat, with an optional caption. Only files inside the server's configured send roots can be sent",
	}, h.sendFile)
	mcp.AddTool(s, &mcp.Tool{
		Name: "forward_messages",
		Description: "Forward messages from one chat to another using Telegram's own forwarding, " +
			"which keeps the original sender's attribution and any captions",
	}, h.forwardMessages)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "edit_message",
		Description: "Edit the text of a message",
	}, h.editMessage)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "mark_read",
		Description: "Mark messages as read",
	}, h.markRead)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "download_media",
		Description: "Download the media (photo/document/video/audio/voice/sticker/animation) of a message and return the local path",
	}, h.downloadMedia)

	return &Server{mcp: s}
}

// Run serves the MCP protocol over stdio until the client disconnects
// or the context is canceled.
func (s *Server) Run(ctx context.Context) error {
	return s.mcp.Run(ctx, &mcp.StdioTransport{})
}

// handlers holds the tool implementations.
type handlers struct {
	tg *telegram.Client
}

func (h *handlers) getMe(ctx context.Context, _ *mcp.CallToolRequest, _ getMeIn) (*mcp.CallToolResult, getMeOut, error) {
	me, err := h.tg.GetMe()
	if err != nil {
		return nil, getMeOut{}, err
	}
	return nil, getMeOut{User: tgjson.ToContactInfo(me)}, nil
}

func (h *handlers) listChats(ctx context.Context, _ *mcp.CallToolRequest, in listChatsIn) (*mcp.CallToolResult, listChatsOut, error) {
	limit := in.Limit
	if limit <= 0 {
		limit = 50
	}
	chats, err := h.tg.ListChats(limit)
	if err != nil {
		return nil, listChatsOut{}, err
	}
	out := listChatsOut{Chats: make([]tgjson.ChatInfo, 0, len(chats))}
	for _, chat := range chats {
		out.Chats = append(out.Chats, tgjson.ToChatInfo(chat))
	}
	return nil, out, nil
}

func (h *handlers) getChatHistory(ctx context.Context, _ *mcp.CallToolRequest, in getChatHistoryIn) (*mcp.CallToolResult, messagesOut, error) {
	limit := in.Limit
	if limit <= 0 {
		limit = 50
	}
	msgs, err := h.tg.GetChatHistory(in.ChatID, in.FromMessageID, in.Offset, limit)
	if err != nil {
		return nil, messagesOut{}, err
	}
	return nil, toMessagesOut(msgs), nil
}

func (h *handlers) searchChats(ctx context.Context, _ *mcp.CallToolRequest, in searchChatsIn) (*mcp.CallToolResult, listChatsOut, error) {
	chats, err := h.tg.SearchChats(in.Query, in.Limit)
	if err != nil {
		return nil, listChatsOut{}, err
	}
	out := listChatsOut{Chats: make([]tgjson.ChatInfo, 0, len(chats))}
	for _, chat := range chats {
		out.Chats = append(out.Chats, tgjson.ToChatInfo(chat))
	}
	return nil, out, nil
}

func (h *handlers) searchMessages(ctx context.Context, _ *mcp.CallToolRequest, in searchMessagesIn) (*mcp.CallToolResult, messagesOut, error) {
	msgs, err := h.tg.SearchMessages(in.Query, in.Limit)
	if err != nil {
		return nil, messagesOut{}, err
	}
	return nil, toMessagesOut(msgs), nil
}

func (h *handlers) getContacts(ctx context.Context, _ *mcp.CallToolRequest, _ getContactsIn) (*mcp.CallToolResult, getContactsOut, error) {
	users, err := h.tg.GetContacts()
	if err != nil {
		return nil, getContactsOut{}, err
	}
	out := getContactsOut{Contacts: make([]tgjson.ContactInfo, 0, len(users))}
	for _, u := range users {
		out.Contacts = append(out.Contacts, tgjson.ToContactInfo(u))
	}
	return nil, out, nil
}

func (h *handlers) sendMessage(ctx context.Context, _ *mcp.CallToolRequest, in sendMessageIn) (*mcp.CallToolResult, messageOut, error) {
	msg, err := h.tg.SendTextMessage(in.ChatID, in.Text, in.ReplyToMessageID)
	if err != nil {
		return nil, messageOut{}, err
	}
	return nil, messageOut{Message: tgjson.ToMessageInfo(msg)}, nil
}

func (h *handlers) sendFile(ctx context.Context, _ *mcp.CallToolRequest, in sendFileIn) (*mcp.CallToolResult, messageOut, error) {
	path, err := telegram.ResolveAllowedSendPath(in.Path, h.tg.SendRoots()...)
	if err != nil {
		return nil, messageOut{}, err
	}
	msg, err := h.tg.SendFileMessage(in.ChatID, path, in.Caption, in.ReplyToMessageID)
	if err != nil {
		return nil, messageOut{}, err
	}
	return nil, messageOut{Message: tgjson.ToMessageInfo(msg)}, nil
}

func (h *handlers) forwardMessages(ctx context.Context, _ *mcp.CallToolRequest, in forwardMessagesIn) (*mcp.CallToolResult, messagesOut, error) {
	if in.FromChatID == 0 || in.ToChatID == 0 {
		return nil, messagesOut{}, errors.New("from_chat_id and to_chat_id are required")
	}
	if len(in.MessageIDs) == 0 {
		return nil, messagesOut{}, errors.New("message_ids must not be empty")
	}
	msgs, err := h.tg.ForwardMessages(in.FromChatID, in.ToChatID, in.MessageIDs)
	if err != nil {
		return nil, messagesOut{}, err
	}
	return nil, toMessagesOut(msgs), nil
}

func (h *handlers) editMessage(ctx context.Context, _ *mcp.CallToolRequest, in editMessageIn) (*mcp.CallToolResult, messageOut, error) {
	msg, err := h.tg.EditTextMessage(in.ChatID, in.MessageID, in.Text)
	if err != nil {
		return nil, messageOut{}, err
	}
	return nil, messageOut{Message: tgjson.ToMessageInfo(msg)}, nil
}

func (h *handlers) markRead(ctx context.Context, _ *mcp.CallToolRequest, in markReadIn) (*mcp.CallToolResult, statusOut, error) {
	if err := h.tg.ViewMessages(in.ChatID, in.MessageIDs); err != nil {
		return nil, statusOut{}, err
	}
	return nil, statusOut{Status: "ok"}, nil
}

func (h *handlers) downloadMedia(ctx context.Context, _ *mcp.CallToolRequest, in downloadMediaIn) (*mcp.CallToolResult, downloadMediaOut, error) {
	msg, err := h.tg.GetMessage(in.ChatID, in.MessageID)
	if err != nil {
		return nil, downloadMediaOut{}, err
	}

	key, name, _ := tgjson.MediaFile(msg)
	if key == "" {
		return nil, downloadMediaOut{}, errors.New("message has no downloadable media")
	}

	file, err := h.tg.DownloadFileSync(key)
	if err != nil {
		return nil, downloadMediaOut{}, err
	}
	return nil, downloadMediaOut{Path: file.Path, Size: file.Size, Name: name}, nil
}

func toMessagesOut(msgs []*telegram.Message) messagesOut {
	out := messagesOut{Messages: make([]tgjson.MessageInfo, 0, len(msgs))}
	for _, m := range msgs {
		out.Messages = append(out.Messages, tgjson.ToMessageInfo(m))
	}
	return out
}
