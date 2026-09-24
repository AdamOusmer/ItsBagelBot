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

type FileUpload struct {
	ChannelID string
	Filename  string
	Data      []byte
	Content   string
	Embed     *domain.Embed
}

func (c *Client) SendFile(ctx context.Context, up FileUpload) (Message, error) {
	form, err := multipartBody(up)
	if err != nil {
		return Message{}, err
	}
	var ref struct {
		ID string `json:"id"`
	}
	if err := c.postMultipart(ctx, filePath(up.ChannelID), form, &ref); err != nil {
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

func multipartBody(up FileUpload) (multipartForm, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	payload, err := codec.Marshal(filePayload(up))
	if err != nil {
		return multipartForm{}, fmt.Errorf("discord: encode payload_json: %w", err)
	}
	if err := w.WriteField("payload_json", string(payload)); err != nil {
		return multipartForm{}, err
	}
	part, err := w.CreateFormFile("files[0]", up.Filename)
	if err != nil {
		return multipartForm{}, err
	}
	if _, err := part.Write(up.Data); err != nil {
		return multipartForm{}, err
	}
	if err := w.Close(); err != nil {
		return multipartForm{}, err
	}
	return multipartForm{body: &buf, contentType: w.FormDataContentType()}, nil
}

type multipartForm struct {
	body        *bytes.Buffer
	contentType string
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

func (c *Client) postMultipart(ctx context.Context, path string, form multipartForm, out any) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, form.body)
	if err != nil {
		return err
	}
	httpReq.Header.Set("Authorization", "Bot "+c.token)
	httpReq.Header.Set("Content-Type", form.contentType)

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
