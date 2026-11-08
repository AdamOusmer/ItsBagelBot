// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package twitch

import (
	"context"
	"net/http"
	"net/url"
)

func (c *Client) StartRaid(ctx context.Context, broadcasterID, targetID string) error {
	q := url.Values{"from_broadcaster_id": {broadcasterID}, "to_broadcaster_id": {targetID}}
	return c.Send(ctx, IdentityBroadcaster, broadcasterID, HelixCall{Method: http.MethodPost, Endpoint: "/helix/raids?" + q.Encode()})
}

func (c *Client) CancelRaid(ctx context.Context, broadcasterID string) error {
	q := url.Values{"broadcaster_id": {broadcasterID}}
	return c.Send(ctx, IdentityBroadcaster, broadcasterID, HelixCall{Method: http.MethodDelete, Endpoint: "/helix/raids?" + q.Encode()})
}

type ChannelRoleChange struct{ Path, Method, UserID string }

func (c *Client) SetChannelRole(ctx context.Context, broadcasterID string, change ChannelRoleChange) error {
	q := url.Values{"broadcaster_id": {broadcasterID}, "user_id": {change.UserID}}
	return c.Send(ctx, IdentityBroadcaster, broadcasterID, HelixCall{Method: change.Method, Endpoint: change.Path + "?" + q.Encode()})
}
