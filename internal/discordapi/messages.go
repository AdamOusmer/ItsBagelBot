// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordapi

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// MessagePageMax is Discord's own per-call maximum for GET
// /channels/{id}/messages. A larger limit is rejected outright, not clamped.
const MessagePageMax = 100

// MessageAuthor is the author object on a message, trimmed to what a
// transcript line renders.
type MessageAuthor struct {
	ID         string `json:"id"`
	Username   string `json:"username"`
	GlobalName string `json:"global_name"`
}

// DisplayName is the name a transcript line carries: the account's chosen
// display name, then the username, then the bare id. The per-guild nickname is
// deliberately NOT consulted -- a message object carries no member, so
// resolving nicknames would mean one extra REST call per distinct author on a
// path that already pages a whole channel.
func (a MessageAuthor) DisplayName() string {
	if a.GlobalName != "" {
		return a.GlobalName
	}
	if a.Username != "" {
		return a.Username
	}
	return a.ID
}

// MessageAttachment is one file on a message. Only the fields a transcript
// records: Discord's CDN URL expires, so the transcript stores the link as it
// was, not the bytes.
type MessageAttachment struct {
	URL      string `json:"url"`
	Filename string `json:"filename"`
}

// FullMessage is one message as the transcript needs it, in contrast to the
// {id,name,type} Snowflake that ListMessages decodes for bulk delete.
type FullMessage struct {
	ID          string              `json:"id"`
	Content     string              `json:"content"`
	Author      MessageAuthor       `json:"author"`
	Attachments []MessageAttachment `json:"attachments"`
	// Timestamp is Discord's ISO 8601 string. Kept raw on the wire type and
	// parsed by At below, so a message with a timestamp Discord changed the
	// format of degrades to the zero time instead of failing the whole page.
	Timestamp string `json:"timestamp"`
}

// At parses Timestamp, or returns the zero time.
func (m FullMessage) At() time.Time {
	t, err := time.Parse(time.RFC3339, m.Timestamp)
	if err != nil {
		return time.Time{}
	}
	return t
}

// MessagePage is one page of GET /channels/{id}/messages. Before is the
// pagination cursor: Discord returns the messages OLDER than that id, newest
// first, which is why the transcript walks backwards and reverses at the end.
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

// ListMessagesFull returns one page of a channel's history with the author,
// content, timestamp and attachments kept. Separate from ListMessages rather
// than widening it: the purge path lists 100 ids per call and has no use for
// any of those fields, and decoding them would make every purge pay for the
// transcript's needs.
func (c *Client) ListMessagesFull(ctx context.Context, page MessagePage) ([]FullMessage, error) {
	var out []FullMessage
	err := c.doInto(ctx, request{method: http.MethodGet, path: page.path()}, &out)
	return out, err
}
