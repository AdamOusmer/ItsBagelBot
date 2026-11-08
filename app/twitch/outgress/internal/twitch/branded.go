// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package twitch

import (
	"context"
	"net/http"
	"net/url"
)

func (c *Client) SetBrandedContent(ctx context.Context, broadcasterID string, enabled bool) error {
	return c.sendJSON(ctx, broadcasterTarget(broadcasterID), HelixCall{Method: http.MethodPatch, Endpoint: "/helix/channels?broadcaster_id=" + url.QueryEscape(broadcasterID)}, map[string]any{"is_branded_content": enabled})
}
