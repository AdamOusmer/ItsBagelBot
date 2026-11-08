// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package twitch

import (
	"ItsBagelBot/pkg/codec"
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

var ErrNoSnoozes = errors.New("no ad snoozes left")

type ChannelModerator struct{ BroadcasterID, ModeratorID string }
type WhisperRequest struct{ FromID, ToID, Message string }

func (c *Client) UpdateChatSettings(ctx context.Context, channel ChannelModerator, settings map[string]any) error {
	q := url.Values{"broadcaster_id": {channel.BroadcasterID}, "moderator_id": {channel.ModeratorID}}
	return c.sendJSON(ctx, botTarget(channel.BroadcasterID), HelixCall{Method: http.MethodPatch, Endpoint: "/helix/chat/settings?" + q.Encode()}, settings)
}

func (c *Client) ClearChat(ctx context.Context, channel ChannelModerator) error {
	q := url.Values{"broadcaster_id": {channel.BroadcasterID}, "moderator_id": {channel.ModeratorID}}
	return c.Send(ctx, IdentityBot, channel.BroadcasterID, HelixCall{Method: http.MethodDelete, Endpoint: "/helix/moderation/chat?" + q.Encode()})
}

func (c *Client) SendWhisper(ctx context.Context, broadcasterID string, request WhisperRequest) error {
	q := url.Values{"from_user_id": {request.FromID}, "to_user_id": {request.ToID}}
	return c.sendJSON(ctx, botTarget(broadcasterID), HelixCall{Method: http.MethodPost, Endpoint: "/helix/whispers?" + q.Encode()}, struct {
		Message string `json:"message"`
	}{request.Message})
}

func (c *Client) SnoozeNextAd(ctx context.Context, broadcasterID string) error {
	q := url.Values{"broadcaster_id": {broadcasterID}}
	err := c.Send(ctx, IdentityBroadcaster, broadcasterID, HelixCall{Method: http.MethodPost, Endpoint: "/helix/channels/ads/schedule/snooze?" + q.Encode()})
	var se *StatusError
	if errors.As(err, &se) && noSnoozes(se) {
		return errors.Join(ErrNoSnoozes, err)
	}
	return err
}

// A generic Helix 429 is retryable. Only the endpoint's explicit exhausted-snooze
// response is final; unknown provider messages stay retryable.
var exhaustedSnoozes = map[string]bool{
	"no snoozes":                   true,
	"channel has no snoozes left.": true,
	"channel has no snoozes left":  true,
}

func noSnoozes(err *StatusError) bool {
	if err.Status != http.StatusTooManyRequests {
		return false
	}
	var body struct {
		Message string `json:"message"`
	}
	if codec.Unmarshal([]byte(err.Body), &body) != nil {
		return false
	}
	message := strings.ToLower(strings.TrimSpace(body.Message))
	return exhaustedSnoozes[message]
}
