// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package twitch

import (
	"ItsBagelBot/pkg/codec"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const chattersPath = "/helix/chat/chatters"

const chattersPageSize = 1000

const chattersMaxPages = 30

type Chatter struct {
	ID    string
	Login string
}

type ChattersPage struct {
	Chatters   []Chatter
	NextCursor string
	Complete   bool
}

var ErrChattersIncomplete = errors.New("chatter listing incomplete; continue pagination")
var ErrRepeatedCursor = errors.New("chatter pagination cursor did not advance")

func (c *Client) GetChattersPage(ctx context.Context, broadcasterID, moderatorID, cursor string) (ChattersPage, error) {
	if c.user == nil {
		return ChattersPage{}, ErrNoUserToken
	}
	endpoint := chattersPath + "?broadcaster_id=" + url.QueryEscape(broadcasterID) +
		"&moderator_id=" + url.QueryEscape(moderatorID) + "&first=" + strconv.Itoa(chattersPageSize)
	if cursor != "" {
		endpoint += "&after=" + url.QueryEscape(cursor)
	}
	res, err := c.ExecuteAs(ctx, IdentityBot, "", getCall(endpoint))
	if err != nil {
		return ChattersPage{}, err
	}
	if res.StatusCode == http.StatusTooManyRequests {
		retryAt := time.Now().Add(RetryAfter(res))
		if !retryAt.After(time.Now()) {
			retryAt = time.Now().Add(time.Second)
		}
		defer drain(res)
		return ChattersPage{}, &AdmissionError{Code: "rate_limited", RetryAt: retryAt}
	}
	batch, next, err := decodeChattersPage(res)
	if err != nil {
		return ChattersPage{}, err
	}
	if next != "" && next == cursor {
		return ChattersPage{}, ErrRepeatedCursor
	}
	return ChattersPage{Chatters: batch, NextCursor: next, Complete: next == ""}, nil
}

func (c *Client) GetChatters(ctx context.Context, broadcasterID, moderatorID string) ([]Chatter, error) {
	var out []Chatter
	cursor := ""
	seen := make(map[string]struct{})
	for page := 0; page < chattersMaxPages; page++ {
		got, err := c.GetChattersPage(ctx, broadcasterID, moderatorID, cursor)
		if err != nil {
			return out, err
		}
		out = append(out, got.Chatters...)
		if got.Complete {
			return out, nil
		}
		if _, duplicate := seen[got.NextCursor]; duplicate {
			return out, ErrRepeatedCursor
		}
		seen[got.NextCursor] = struct{}{}
		cursor = got.NextCursor
	}
	return out, ErrChattersIncomplete
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
		Data *[]struct {
			UserID    string `json:"user_id"`
			UserLogin string `json:"user_login"`
		} `json:"data"`
		Pagination *struct {
			Cursor string `json:"cursor"`
		} `json:"pagination"`
	}
	if err := codec.NewDecoder(io.LimitReader(res.Body, 2<<20)).Decode(&payload); err != nil {
		return nil, "", err
	}
	if payload.Data == nil || payload.Pagination == nil {
		return nil, "", errors.New("invalid chatter response envelope")
	}
	if len(*payload.Data) > chattersPageSize || len(payload.Pagination.Cursor) > 4096 {
		return nil, "", errors.New("invalid oversized chatter page")
	}
	batch := make([]Chatter, 0, len(*payload.Data))
	for _, d := range *payload.Data {
		if d.UserID == "" {
			continue
		}
		batch = append(batch, Chatter{ID: d.UserID, Login: d.UserLogin})
	}
	return batch, payload.Pagination.Cursor, nil
}
