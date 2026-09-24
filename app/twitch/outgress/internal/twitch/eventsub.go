// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package twitch

import (
	"ItsBagelBot/pkg/codec"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

type SubSpec struct {
	Type      string            `json:"type"`
	Version   string            `json:"version"`
	Condition map[string]string `json:"condition"`
}

func ChannelSubscriptions(broadcasterID, botID string) []SubSpec {
	specs := make([]SubSpec, 0, 10)

	specs = append(specs,
		SubSpec{"stream.online", "1", map[string]string{"broadcaster_user_id": broadcasterID}},
		SubSpec{"stream.offline", "1", map[string]string{"broadcaster_user_id": broadcasterID}},
		SubSpec{"channel.update", "2", map[string]string{"broadcaster_user_id": broadcasterID}},
		SubSpec{"channel.raid", "1", map[string]string{"to_broadcaster_user_id": broadcasterID}},
		SubSpec{"channel.subscribe", "1", map[string]string{"broadcaster_user_id": broadcasterID}},
		SubSpec{"channel.subscription.gift", "1", map[string]string{"broadcaster_user_id": broadcasterID}},
		SubSpec{"channel.subscription.message", "1", map[string]string{"broadcaster_user_id": broadcasterID}},
		SubSpec{"channel.cheer", "1", map[string]string{"broadcaster_user_id": broadcasterID}},
		SubSpec{"channel.follow", "2", map[string]string{"broadcaster_user_id": broadcasterID, "moderator_user_id": broadcasterID}},
	)

	if botID != "" {
		specs = append(specs,
			SubSpec{ChatMessageType, "1", map[string]string{"broadcaster_user_id": broadcasterID, "user_id": botID}},
		)
	}

	return specs
}

const ChatMessageType = "channel.chat.message"

func ChannelOptionalSubscriptions(broadcasterID string) []SubSpec {
	return []SubSpec{
		{"channel.channel_points_custom_reward_redemption.add", "1", map[string]string{"broadcaster_user_id": broadcasterID}},
		{"channel.ad_break.begin", "1", map[string]string{"broadcaster_user_id": broadcasterID}},
	}
}

func ClientSubscriptions(clientID string) []SubSpec {
	return []SubSpec{
		{"user.authorization.grant", "1", map[string]string{"client_id": clientID}},
		{"user.authorization.revoke", "1", map[string]string{"client_id": clientID}},
	}
}

func (c *Client) CreateEventSub(ctx context.Context, spec SubSpec, conduitID string) error {

	body, _ := codec.Marshal(map[string]any{
		"type":      spec.Type,
		"version":   spec.Version,
		"condition": spec.Condition,
		"transport": map[string]string{
			"method":     "conduit",
			"conduit_id": conduitID,
		},
	})

	res, err := c.Do(ctx, http.MethodPost, "/helix/eventsub/subscriptions", body)
	if err != nil {
		return err
	}
	defer drain(res)

	if res.StatusCode == http.StatusAccepted || res.StatusCode == http.StatusConflict {
		return nil
	}
	return statusError(res, "eventsub create")
}

type EventSubEntry struct {
	ID        string `json:"id"`
	Transport struct {
		Method    string `json:"method"`
		ConduitID string `json:"conduit_id"`
	} `json:"transport"`
	Condition struct {
		BroadcasterUserID string `json:"broadcaster_user_id"`
	} `json:"condition"`
}

func (c *Client) ListEventSubs(ctx context.Context, userID, cursor string) ([]EventSubEntry, string, error) {

	endpoint := "/helix/eventsub/subscriptions?user_id=" + url.QueryEscape(userID)
	if cursor != "" {
		endpoint += "&after=" + url.QueryEscape(cursor)
	}

	res, err := c.Do(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, "", err
	}
	defer drain(res)

	if res.StatusCode != http.StatusOK {
		return nil, "", statusError(res, "eventsub list")
	}

	var page struct {
		Data       []EventSubEntry `json:"data"`
		Pagination struct {
			Cursor string `json:"cursor"`
		} `json:"pagination"`
	}
	if err := codec.NewDecoder(res.Body).Decode(&page); err != nil {
		return nil, "", err
	}
	return page.Data, page.Pagination.Cursor, nil
}

func (c *Client) DeleteEventSub(ctx context.Context, id string) error {

	res, err := c.Do(ctx, http.MethodDelete, "/helix/eventsub/subscriptions?id="+url.QueryEscape(id), nil)
	if err != nil {
		return err
	}
	defer drain(res)

	if res.StatusCode == http.StatusNoContent || res.StatusCode == http.StatusNotFound {
		return nil
	}
	return statusError(res, "eventsub delete")
}

type StatusError struct {
	Status int
	Op     string
	Body   string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("%s returned %d: %s", e.Op, e.Status, e.Body)
}

func statusError(res *http.Response, op string) error {
	body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
	return &StatusError{Status: res.StatusCode, Op: op, Body: string(body)}
}
