package telegram

import (
	"context"
	"fmt"
	"math/rand"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/gotd/td/tg"
)

// ResolveAllowedSendPath returns the absolute, symlink-resolved form of
// path if it exists and is the same as, or inside, one of roots.
// Empty roots are ignored. The file itself must exist (a dangling last
// component is rejected) so a symlink cannot later be swapped for a
// path outside the jail.
func ResolveAllowedSendPath(path string, roots ...string) (string, error) {
	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("send file: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("send file: %w", err)
	}
	for _, root := range roots {
		if root == "" {
			continue
		}
		absRoot, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		evalRoot, err := filepath.EvalSymlinks(absRoot)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(evalRoot, resolved)
		if err != nil {
			continue
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		return resolved, nil
	}
	// Name the roots. The caller here is an authenticated operator or the
	// agent they configured, the set is already logged at startup and
	// documented, and "outside the allowed directories" with no list is a
	// dead end — the reader cannot tell a typo from a policy.
	return "", fmt.Errorf("send file: path %q is outside the allowed directories (%s)",
		path, strings.Join(nonEmpty(roots), ", "))
}

// nonEmpty drops the blank roots ResolveAllowedSendPath skips, so the
// error names the set that was actually searched. A caller that passes no
// usable root gets "()" and rejects everything, which is the correct
// fail-closed reading of an empty allowlist.
func nonEmpty(roots []string) []string {
	out := make([]string, 0, len(roots))
	for _, r := range roots {
		if r != "" {
			out = append(out, r)
		}
	}
	return out
}

// SendFileMessage uploads a local file and sends it as a document,
// optionally with a caption and as a reply.
//
// placeholderID is the local echo the caller already drew for this send, so
// the success message can name the row to swap; 0 when there is none.
func (c *Client) SendFileMessage(chatID int64, path, caption string, replyToMessageID int64, placeholderID int64) (*Message, error) {
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

	msg, err := c.sendUploadedMedia(ctx, peer, media, caption, replyToMessageID, placeholderID)
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
//
// placeholderID names the local echo to swap, as in SendFileMessage.
func (c *Client) SendPhotoMessage(chatID int64, path, caption string, replyToMessageID int64, placeholderID int64) (*Message, error) {
	ctx, cancel := transferCtx()
	defer cancel()

	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("send photo: %w", err)
	}
	// Checked against the file on disk, and before the cached upload is
	// consumed: the limit is inputMediaUploadedPhoto's, not the upload's,
	// so an eagerly uploaded image is just as ineligible — and the entry
	// has to survive, because the way out of this error is to send the
	// same already-uploaded file as a document instead.
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

	msg, err := c.sendUploadedMedia(ctx, peer, media, caption, replyToMessageID, placeholderID)
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

	// The file may already be up: StartUpload puts it on Telegram's
	// servers when it is attached, which is normally seconds before this.
	// A cached failure is not one — the entry is dropped and the upload
	// retried here, synchronously, because the reason it failed (a dropped
	// link, most often) is usually gone by now.
	if file, cached, err := c.uploads.await(ctx, path); cached && err == nil {
		return peer, file, nil
	}

	inputFile, err := c.uploadFile(ctx, path, c.uploads.nextGeneration())
	if err != nil {
		return nil, nil, fmt.Errorf("upload %q: %w", path, err)
	}
	return peer, inputFile, nil
}

// sendUploadedMedia sends already-uploaded media to peer and publishes the
// resulting message to the update stream.
func (c *Client) sendUploadedMedia(ctx context.Context, peer tg.InputPeerClass, media tg.InputMediaClass, caption string, replyToMessageID int64, placeholderID int64) (*Message, error) {
	body, entities := c.formatOutgoing(caption)
	req := &tg.MessagesSendMediaRequest{
		Peer:     peer,
		Media:    media,
		Message:  body,
		Entities: entities,
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
	c.send(MessageSendSucceededMsg{Message: msg, OldMessageId: placeholderID})
	return msg, nil
}
