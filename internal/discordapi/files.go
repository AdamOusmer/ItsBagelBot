// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordapi

import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"

	domain "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/pkg/codec"
)

// FileUpload is one attachment posted into a channel, optionally alongside
// content and an embed (the ticket close summary posts both at once).
type FileUpload struct {
	ChannelID string
	Filename  string
	Data      []byte
	Content   string
	Embed     *domain.Embed
}

// SendFile posts a message carrying one file attachment.
//
// This is the only multipart call in the client. Discord's attachment upload
// is multipart/form-data with the JSON message body in a part literally named
// "payload_json" and each file in "files[N]"; there is no JSON-only form of
// it, and base64 in the JSON body works for a guild icon but not for a message
// attachment. The bot needs ATTACH_FILES in the target channel or Discord
// answers 403, which classifies as ErrForbidden like any other missing
// permission.
func (c *Client) SendFile(ctx context.Context, up FileUpload) (Message, error) {
	body, contentType, err := multipartBody(up)
	if err != nil {
		return Message{}, err
	}
	var ref struct {
		ID string `json:"id"`
	}
	if err := c.postMultipart(ctx, filePath(up.ChannelID), body, contentType, &ref); err != nil {
		return Message{}, err
	}
	if ref.ID == "" {
		return Message{}, ErrNoMessageID
	}
	return Message{ChannelID: up.ChannelID, ID: ref.ID}, nil
}

func filePath(channelID string) string {
	return "/channels/" + url.PathEscape(channelID) + "/messages"
}

// multipartBody renders the two parts Discord expects: payload_json (the
// ordinary message body) and files[0] (the attachment). The attachments array
// in payload_json is what binds the two -- its id must match the files[N]
// index, or Discord accepts the message and silently drops the file.
func multipartBody(up FileUpload) (*bytes.Buffer, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	payload, err := codec.Marshal(filePayload(up))
	if err != nil {
		return nil, "", fmt.Errorf("discord: encode payload_json: %w", err)
	}
	if err := w.WriteField("payload_json", string(payload)); err != nil {
		return nil, "", err
	}
	part, err := w.CreateFormFile("files[0]", up.Filename)
	if err != nil {
		return nil, "", err
	}
	if _, err := part.Write(up.Data); err != nil {
		return nil, "", err
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return &buf, w.FormDataContentType(), nil
}

func filePayload(up FileUpload) map[string]any {
	payload := map[string]any{
		"attachments": []map[string]any{{"id": 0, "filename": up.Filename}},
	}
	if up.Content != "" {
		payload["content"] = up.Content
	}
	if up.Embed != nil {
		payload["embeds"] = []domain.Embed{*up.Embed}
	}
	return payload
}

// postMultipart is doInto's multipart twin: same auth header, same error
// classification, a body this client encodes itself rather than through
// request.payload (which is JSON-only by construction).
func (c *Client) postMultipart(ctx context.Context, path string, body *bytes.Buffer, contentType string, out any) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, body)
	if err != nil {
		return err
	}
	httpReq.Header.Set("Authorization", "Bot "+c.token)
	httpReq.Header.Set("Content-Type", contentType)

	res, err := c.http.Do(httpReq)
	if err != nil {
		return err
	}
	defer drain(res)

	raw := readBody(res)
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return classify(res, raw)
	}
	if err := codec.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("discord: decode POST %s: %w", path, err)
	}
	return nil
}
