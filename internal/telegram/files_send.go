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
	ctx, cancel := transferCtx()
	defer cancel()

	peer, inputFile, err := c.uploadForSend(ctx, chatID, path)
	if err != nil {
		return nil, fmt.Errorf("send file: %w", err)
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

	msg, err := c.sendUploadedMedia(ctx, peer, media, caption, replyToMessageID)
	if err != nil {
		return nil, fmt.Errorf("send file: %w", err)
	}
	return msg, nil
}

// photoSizeLimit is the largest file Telegram accepts as an uploaded
// photo. Past it the uploader switches to inputFileBig, which
// inputMediaUploadedPhoto rejects — so check up front and fail with an
// actionable message instead of an opaque upstream error.
const photoSizeLimit = 10 << 20 // 10 MiB

// SendPhotoMessage uploads a local image and sends it as a photo, so it
// renders inline in the chat rather than as a file attachment.
// Images above photoSizeLimit must be sent with SendFileMessage instead.
func (c *Client) SendPhotoMessage(chatID int64, path, caption string, replyToMessageID int64) (*Message, error) {
	ctx, cancel := transferCtx()
	defer cancel()

	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("send photo: %w", err)
	}
	if info.Size() > photoSizeLimit {
		return nil, fmt.Errorf(
			"send photo: image too large to send as photo (%.1f MB, limit 10 MB) — send it as a file instead",
			float64(info.Size())/(1<<20))
	}

	peer, inputFile, err := c.uploadForSend(ctx, chatID, path)
	if err != nil {
		return nil, fmt.Errorf("send photo: %w", err)
	}

	media := &tg.InputMediaUploadedPhoto{File: inputFile}

	msg, err := c.sendUploadedMedia(ctx, peer, media, caption, replyToMessageID)
	if err != nil {
		return nil, fmt.Errorf("send photo: %w", err)
	}
	return msg, nil
}

// uploadForSend resolves the target peer and uploads path to Telegram.
func (c *Client) uploadForSend(ctx context.Context, chatID int64, path string) (tg.InputPeerClass, tg.InputFileClass, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, nil, err
	}
	if info.IsDir() {
		return nil, nil, fmt.Errorf("%q is a directory", path)
	}

	peer, err := c.inputPeer(ctx, chatID)
	if err != nil {
		return nil, nil, err
	}

	inputFile, err := uploader.NewUploader(c.api).FromPath(ctx, path)
	if err != nil {
		return nil, nil, fmt.Errorf("upload %q: %w", path, err)
	}
	return peer, inputFile, nil
}

// sendUploadedMedia sends already-uploaded media to peer and publishes the
// resulting message to the update stream.
func (c *Client) sendUploadedMedia(ctx context.Context, peer tg.InputPeerClass, media tg.InputMediaClass, caption string, replyToMessageID int64) (*Message, error) {
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
		return nil, err
	}

	msg := messageFromUpdates(c, updates)
	if msg == nil {
		return nil, fmt.Errorf("no message in response")
	}
	c.send(MessageSendSucceededMsg{Message: msg, OldMessageId: 0})
	return msg, nil
}
