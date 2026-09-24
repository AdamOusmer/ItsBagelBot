// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordapi

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

const MessagePageMax = 100

type MessageAuthor struct {
	ID         string `json:"id"`
	Username   string `json:"username"`
	GlobalName string `json:"global_name"`
}

func (a MessageAuthor) DisplayName() string {
	if a.GlobalName != "" {
		return a.GlobalName
	}
	if a.Username != "" {
		return a.Username
	}
	return a.ID
}

type MessageAttachment struct {
	URL      string `json:"url"`
	Filename string `json:"filename"`
}

type FullMessage struct {
	ID          string              `json:"id"`
	Content     string              `json:"content"`
	Author      MessageAuthor       `json:"author"`
	Attachments []MessageAttachment `json:"attachments"`
	Embeds      []MessageEmbed      `json:"embeds"`
	Timestamp   string              `json:"timestamp"`
}

type MessageEmbed struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

func (m FullMessage) At() time.Time {
	t, err := time.Parse(time.RFC3339, m.Timestamp)
	if err != nil {
		return time.Time{}
	}
	return t
}

type MessagePage struct {
	ChannelID string
	Before    string
	Limit     int
}

func (p MessagePage) path() string {
	limit := p.Limit
	if limit <= 0 || limit > MessagePageMax {
		limit = MessagePageMax
	}
	q := url.Values{}
	q.Set("limit", itoa(limit))
	if p.Before != "" {
		q.Set("before", p.Before)
	}
	return "/channels/" + url.PathEscape(p.ChannelID) + "/messages?" + q.Encode()
}

func (c *Client) ListMessagesFull(ctx context.Context, page MessagePage) ([]FullMessage, error) {
	var out []FullMessage
	err := c.doInto(ctx, request{method: http.MethodGet, path: page.path()}, &out)
	return out, err
}
