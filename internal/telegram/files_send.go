package telegram

import (
	"context"
	"fmt"
	"math/rand"
	"mime"
	"os"
	"path/filepath"

	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
)

// SendFileMessage uploads a local file and sends it as a document,
// optionally with a caption and as a reply.
func (c *Client) SendFileMessage(chatID int64, path, caption string, replyToMessageID int64) (*Message, error) {
	ctx := context.Background()

	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("send file: %w", err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("send file: %q is a directory", path)
	}

	peer, err := c.inputPeer(ctx, chatID)
	if err != nil {
		return nil, fmt.Errorf("send file: %w", err)
	}

	inputFile, err := uploader.NewUploader(c.api).FromPath(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("send file: upload %q: %w", path, err)
	}

	mimeType := mime.TypeByExtension(filepath.Ext(path))
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	media := &tg.InputMediaUploadedDocument{
		File:     inputFile,
		MimeType: mimeType,
		Attributes: []tg.DocumentAttributeClass{
			&tg.DocumentAttributeFilename{FileName: filepath.Base(path)},
		},
	}

	req := &tg.MessagesSendMediaRequest{
		Peer:     peer,
		Media:    media,
		Message:  caption,
		RandomID: rand.Int63(),
	}
	if replyToMessageID != 0 {
		req.ReplyTo = &tg.InputReplyToMessage{ReplyToMsgID: int(replyToMessageID)}
	}

	updates, err := c.api.MessagesSendMedia(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("send file: %w", err)
	}

	msg := messageFromUpdates(c, updates)
	if msg == nil {
		return nil, fmt.Errorf("send file: no message in response")
	}
	c.send(MessageSendSucceededMsg{Message: msg, OldMessageId: 0})
	return msg, nil
}
