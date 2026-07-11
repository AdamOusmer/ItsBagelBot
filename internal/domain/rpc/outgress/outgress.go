// Package outgress defines request/reply contracts for authenticated Twitch
// reads exposed by the outgress service to Sesame.
package outgress

import "time"

type FollowageRequest struct {
	BroadcasterID string `json:"broadcaster_id"`
	TargetID      string `json:"target_id,omitempty"`
	TargetLogin   string `json:"target_login,omitempty"`
}

type FollowageReply struct {
	TargetID   string    `json:"target_id,omitempty"`
	UserFound  bool      `json:"user_found"`
	Following  bool      `json:"following"`
	FollowedAt time.Time `json:"followed_at,omitempty"`
	Error      string    `json:"error,omitempty"`
}
