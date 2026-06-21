// Package manage holds the shared wire types for the outgress management RPC surface.
package manage

import "time"

// Channel is the canonical per-broadcaster registry entry: it is both the
// outgress channel registry's stored model and the wire shape for the
// channel.get / channel.set / channel.list verbs.
type Channel struct {
	BroadcasterID string    `json:"broadcaster_id"`
	Enabled       bool      `json:"enabled"`
	IsMod         bool      `json:"is_mod"`
	ModCheckedAt  time.Time `json:"mod_checked_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	SubState      string    `json:"sub_state"` // "ok" | "pending" | "failing" | "" (unknown)
	SubError      string    `json:"sub_error"` // last failure detail; empty when ok
	SubCheckedAt  time.Time `json:"sub_checked_at"`
}

// ChannelRequest is the input shape for channel.get and channel.set verbs.
type ChannelRequest struct {
	BroadcasterID string `json:"broadcaster_id"`
	Enabled       *bool  `json:"enabled,omitempty"`
	IsMod         *bool  `json:"is_mod,omitempty"`
}

// ChannelReply is the reply shape for channel.get and channel.set verbs.
type ChannelReply struct {
	Channel *Channel `json:"channel,omitempty"`
	Found   bool     `json:"found"`
	Error   string   `json:"error,omitempty"`
}

// ChannelListReply is the reply shape for the channel.list verb.
type ChannelListReply struct {
	Channels []Channel `json:"channels"`
	Error    string    `json:"error,omitempty"`
}

// SystemStatusReply is the reply shape for the system.status verb.
type SystemStatusReply struct {
	Paused                   bool   `json:"paused"`
	AppTokenExpiresInSeconds int64  `json:"app_token_expires_in_seconds"`
	HasUserToken             bool   `json:"has_user_token"`
	Error                    string `json:"error,omitempty"`
}

// SystemPauseRequest is the input shape for the system.pause verb.
type SystemPauseRequest struct {
	Paused bool `json:"paused"`
}

// SystemPauseReply is the reply shape for the system.pause verb.
type SystemPauseReply struct {
	Paused bool   `json:"paused"`
	Error  string `json:"error,omitempty"`
}
