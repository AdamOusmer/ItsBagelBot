// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package twitch

import (
	"context"
	"net/http"
	"net/url"

	"ItsBagelBot/pkg/codec"
)

func (c *Client) FollowersTotal(ctx context.Context, broadcasterID string) (int, bool, error) {
	q := url.Values{}
	q.Set("broadcaster_id", broadcasterID)
	return c.helixTotal(ctx, IdentityBot, broadcasterID, "/helix/channels/followers?"+q.Encode())
}

func (c *Client) SubscriptionsTotal(ctx context.Context, broadcasterID string) (int, bool, error) {
	q := url.Values{}
	q.Set("broadcaster_id", broadcasterID)
	return c.helixTotal(ctx, IdentityBroadcaster, broadcasterID, "/helix/subscriptions?"+q.Encode())
}

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
		return 0, false, statusError(res, "GET "+endpoint)
	}

	var payload struct {
		Total int `json:"total"`
	}
	if err := codec.NewDecoder(res.Body).Decode(&payload); err != nil {
		return 0, false, err
	}
	return payload.Total, true, nil
}
