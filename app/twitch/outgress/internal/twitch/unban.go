// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package twitch

import (
	"context"
	"net/http"
	"net/url"
)

type UnbanResolution struct{ RequestID, Status, Text string }

func (c *Client) ResolveUnbanRequest(ctx context.Context, channel ChannelModerator, resolution UnbanResolution) error {
	q := url.Values{"broadcaster_id": {channel.BroadcasterID}, "moderator_id": {channel.ModeratorID}, "unban_request_id": {resolution.RequestID}, "status": {resolution.Status}}
	if resolution.Text != "" {
		q.Set("resolution_text", resolution.Text)
	}
	return c.Send(ctx, IdentityBot, channel.BroadcasterID, HelixCall{Method: http.MethodPatch, Endpoint: "/helix/moderation/unban_requests?" + q.Encode()})
}
