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
	endpoint := chatterPageEndpoint(broadcasterID, moderatorID, cursor)
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
	if !cursorAdvanced(cursor, next) {
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
	if err := chatterResponseStatus(res); err != nil {
		return nil, "", err
	}

	var payload chatterPayload
	if err := codec.NewDecoder(io.LimitReader(res.Body, 2<<20)).Decode(&payload); err != nil {
		return nil, "", err
	}
	if err := payload.validate(); err != nil {
		return nil, "", err
	}
	return payload.chatters(), payload.Pagination.Cursor, nil
}

func chatterPageEndpoint(broadcasterID, moderatorID, cursor string) string {
	endpoint := chattersPath + "?broadcaster_id=" + url.QueryEscape(broadcasterID) +
		"&moderator_id=" + url.QueryEscape(moderatorID) + "&first=" + strconv.Itoa(chattersPageSize)
	if cursor != "" {
		endpoint += "&after=" + url.QueryEscape(cursor)
	}
	return endpoint
}
func cursorAdvanced(current, next string) bool {
	if next == "" {
		return true
	}
	return next != current
}

type chatterPayload struct {
	Data *[]struct {
		UserID    string `json:"user_id"`
		UserLogin string `json:"user_login"`
	} `json:"data"`
	Pagination *struct {
		Cursor string `json:"cursor"`
	} `json:"pagination"`
}

func (p chatterPayload) validate() error {
	if p.Data == nil {
		return errors.New("invalid chatter response envelope")
	}
	if p.Pagination == nil {
		return errors.New("invalid chatter response envelope")
	}
	if len(*p.Data) > chattersPageSize {
		return errors.New("invalid oversized chatter page")
	}
	if len(p.Pagination.Cursor) > 4096 {
		return errors.New("invalid oversized chatter page")
	}
	return nil
}
func (p chatterPayload) chatters() []Chatter {
	batch := make([]Chatter, 0, len(*p.Data))
	for _, d := range *p.Data {
		if d.UserID == "" {
			continue
		}
		batch = append(batch, Chatter{ID: d.UserID, Login: d.UserLogin})
	}
	return batch
}
func chatterResponseStatus(res *http.Response) error {
	switch res.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return ErrMissingScope
	}
	if res.StatusCode >= http.StatusOK && res.StatusCode < http.StatusMultipleChoices {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
	return &StatusError{Status: res.StatusCode, Body: string(body)}
}
