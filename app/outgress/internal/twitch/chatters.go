package twitch

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

// chattersPath is Helix Get Chatters. It rides the bot's user token
// (moderator:read:chatters, see userScopedPrefixes): the bot must be a
// moderator of the channel, which channel enrollment already requires.
const chattersPath = "/helix/chat/chatters"

// chattersPageSize is Helix's maximum page size for Get Chatters; fewer pages
// for big channels means fewer rate-bucket points per watch tick.
const chattersPageSize = 1000

// chattersMaxPages caps one listing at 30k chatters. Not a product cap: it
// bounds a single RPC's Helix spend and reply size so one enormous channel's
// tick can never monopolize the bot token's rate bucket. Anyone beyond the cap
// misses one tick's accrual, which the loss-tolerant counters absorb.
const chattersMaxPages = 30

// Chatter is one connected chat user (lurkers included).
type Chatter struct {
	ID    string
	Login string
}

// GetChatters lists everyone connected to the broadcaster's chat, paging
// through Helix under the bot's user token. moderatorID is the bot account's
// own user id (Helix requires the token owner's id on the query string). A
// 401/403 surfaces as ErrMissingScope (grant predates moderator:read:chatters,
// or the bot lost its moderator seat) so callers can skip rather than retry.
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

// decodeChattersPage consumes one response: status mapping, then the data +
// pagination cursor.
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
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
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
