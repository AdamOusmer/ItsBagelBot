// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package twitch

import (
	"ItsBagelBot/pkg/codec"
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

const chattersPath = "/helix/chat/chatters"

const chattersPageSize = 1000

const chattersMaxPages = 30

type Chatter struct {
	ID    string
	Login string
}

func (c *Client) GetChatters(ctx context.Context, broadcasterID, moderatorID string) ([]Chatter, error) {
	if c.user == nil {
		return nil, ErrNoUserToken
	}
	base := chattersPath + "?broadcaster_id=" + url.QueryEscape(broadcasterID) +
		"&moderator_id=" + url.QueryEscape(moderatorID) +
		"&first=" + strconv.Itoa(chattersPageSize)

	var out []Chatter
	cursor := ""
	for page := 0; page < chattersMaxPages; page++ {
		endpoint := base
		if cursor != "" {
			endpoint += "&after=" + url.QueryEscape(cursor)
		}
		res, err := c.ExecuteAs(ctx, IdentityBot, "", getCall(endpoint))
		if err != nil {
			return nil, err
		}
		batch, next, err := decodeChattersPage(res)
		if err != nil {
			return nil, err
		}
		out = append(out, batch...)
		if next == "" {
			return out, nil
		}
		cursor = next
	}
	return out, nil
}

func decodeChattersPage(res *http.Response) ([]Chatter, string, error) {
	defer drain(res)
	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return nil, "", ErrMissingScope
	}
	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return nil, "", &StatusError{Status: res.StatusCode, Body: string(body)}
	}

	var payload struct {
		Data []struct {
			UserID    string `json:"user_id"`
			UserLogin string `json:"user_login"`
		} `json:"data"`
		Pagination struct {
			Cursor string `json:"cursor"`
		} `json:"pagination"`
	}
	if err := codec.NewDecoder(res.Body).Decode(&payload); err != nil {
		return nil, "", err
	}
	batch := make([]Chatter, 0, len(payload.Data))
	for _, d := range payload.Data {
		if d.UserID == "" {
			continue
		}
		batch = append(batch, Chatter{ID: d.UserID, Login: d.UserLogin})
	}
	return batch, payload.Pagination.Cursor, nil
}
