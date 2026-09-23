// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package twitch

import (
	"ItsBagelBot/pkg/codec"
	"context"
	"io"
	"net/http"
	"net/url"
)

// FollowersTotal reads the channel's current follower count under the bot's
// moderator-scoped user token: the sibling to FollowedAt, which decodes one
// viewer's followed_at from the same endpoint. This decodes Helix's total
// instead and carries no user_id, so it costs one page regardless of the
// channel's size.
func (c *Client) FollowersTotal(ctx context.Context, broadcasterID string) (int, bool, error) {
	q := url.Values{}
	q.Set("broadcaster_id", broadcasterID)
	return c.helixTotal(ctx, IdentityBot, broadcasterID, "/helix/channels/followers?"+q.Encode())
}

// SubscriptionsTotal reads the channel's current subscriber count under the
// BROADCASTER's own token (channel:read:subscriptions): Twitch exposes this
// to the channel itself, not to a moderator or the app.
func (c *Client) SubscriptionsTotal(ctx context.Context, broadcasterID string) (int, bool, error) {
	q := url.Values{}
	q.Set("broadcaster_id", broadcasterID)
	return c.helixTotal(ctx, IdentityBroadcaster, broadcasterID, "/helix/subscriptions?"+q.Encode())
}

// helixTotal reads one Helix pagination envelope's "total" field under the
// given identity. found is false on a 401/403 (the grant is missing the
// scope this identity needs) rather than an error, so a caller reading two
// independent totals (followers, subs) can let one degrade without failing
// the other — the same convention decodeChattersPage follows for
// moderator:read:chatters. Any other non-2xx, or a decode failure, is a real
// error.
func (c *Client) helixTotal(ctx context.Context, id Identity, broadcasterID, endpoint string) (total int, found bool, err error) {
	res, err := c.ExecuteAs(ctx, id, broadcasterID, getCall(endpoint))
	if err != nil {
		return 0, false, err
	}
	defer drain(res)

	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return 0, false, nil
	}
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return 0, false, &StatusError{Status: res.StatusCode, Body: string(body)}
	}

	var payload struct {
		Total int `json:"total"`
	}
	if err := codec.NewDecoder(res.Body).Decode(&payload); err != nil {
		return 0, false, err
	}
	return payload.Total, true, nil
}
