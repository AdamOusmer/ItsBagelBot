// Package manage holds the shared wire types for the outgress management RPC surface.
package manage

import "time"

// GrantState is the health of a broadcaster's own stored OAuth grant, which is
// separate from SubState. Twitch announces a revocation over EventSub, so
// SubState learns about it; a refresh token that simply stops working announces
// nothing, so it is only ever observed when a call under the broadcaster's
// identity fails.
type GrantState string

const (
	// GrantUnknown is the healthy default. An absent Valkey field decodes to
	// this, so every pre-existing channel reads correct without a backfill.
	GrantUnknown GrantState = ""
	// GrantDead means Twitch rejected the stored grant and only re-consent
	// will fix it.
	GrantDead GrantState = "dead"
)

// Channel is the canonical per-broadcaster registry entry: it is both the
// outgress channel registry's stored model and the wire shape for the
// channel.get / channel.set / channel.list verbs.
type Channel struct {
	BroadcasterID string     `json:"broadcaster_id"`
	Enabled       bool       `json:"enabled"`
	IsMod         bool       `json:"is_mod"`
	ModCheckedAt  time.Time  `json:"mod_checked_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	SubState      string     `json:"sub_state"` // "ok" | "pending" | "failing" | "revoked" | "" (unknown)
	SubError      string     `json:"sub_error"` // last failure detail; empty when ok
	SubCheckedAt  time.Time  `json:"sub_checked_at"`
	GrantState    GrantState `json:"grant_state"` // "" (healthy/unknown) | "dead"
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

// Reward is the canonical wire shape for one Twitch custom channel-points reward
// the dashboard manages, for the channelpoints.{list,create,update,delete} verbs.
// It mirrors the subset of the Helix Custom Reward object outgress reads/writes.
// The bot ACTION a redemption triggers (chat line, announce, shoutout, auto
// fulfill) is NOT here: that mapping is stored by the dashboard in the modules
// service (the hidden "channelpoints" module blob) and read by sesame. This type
// is only the Twitch-side reward outgress owns via the broadcaster token.
type Reward struct {
	ID              string `json:"id,omitempty"`
	Title           string `json:"title"`
	Cost            int    `json:"cost"`
	Prompt          string `json:"prompt,omitempty"`
	BackgroundColor string `json:"background_color,omitempty"`
	IsEnabled       bool   `json:"is_enabled"`
	IsPaused        bool   `json:"is_paused"`
	// IsUserInputRequired makes the viewer type text with the redeem, exposed to
	// the reward's chat action as {input}.
	IsUserInputRequired bool `json:"is_user_input_required"`
	// ShouldSkipQueue auto-marks redemptions FULFILLED on Twitch's side. The
	// dashboard sets it false whenever the reward has a bot action that must
	// resolve the redemption (auto fulfill/cancel), since a skipped redemption
	// cannot be updated.
	ShouldSkipQueue bool `json:"should_skip_queue"`
	// Limit controls ("claimable once and so on"). Each *Enabled gates its value;
	// a disabled limit ignores the value.
	MaxPerStreamEnabled        bool `json:"max_per_stream_enabled"`
	MaxPerStream               int  `json:"max_per_stream"`
	MaxPerUserPerStreamEnabled bool `json:"max_per_user_per_stream_enabled"`
	MaxPerUserPerStream        int  `json:"max_per_user_per_stream"`
	GlobalCooldownEnabled      bool `json:"global_cooldown_enabled"`
	GlobalCooldownSeconds      int  `json:"global_cooldown_seconds"`
}

// RewardRequest is the input for the channelpoints verbs. RewardID targets an
// update/delete; Reward carries the desired state for create/update. list needs
// only the broadcaster id.
type RewardRequest struct {
	BroadcasterID string  `json:"broadcaster_id"`
	RewardID      string  `json:"reward_id,omitempty"`
	Reward        *Reward `json:"reward,omitempty"`
}

// RewardReply is the reply for the channelpoints verbs. create/update echo the
// resulting Reward (with its Twitch-assigned id); list returns Rewards.
type RewardReply struct {
	Reward  *Reward  `json:"reward,omitempty"`
	Rewards []Reward `json:"rewards,omitempty"`
	// MissingScope is set when Twitch rejected the call for a missing OAuth scope:
	// the broadcaster's stored grant predates channel:manage:redemptions, so they
	// must re-consent. The dashboard shows a reconnect CTA on this flag rather than
	// treating it as a generic failure.
	MissingScope bool   `json:"missing_scope,omitempty"`
	Error        string `json:"error,omitempty"`
}

// Chatter is one connected chat user, as Helix Get Chatters reports them.
// Only the stable id and login travel: the loyalty watch tick needs an
// identity to accrue against, not a display name, and dropping the name keeps
// a big channel's reply well under the broker payload ceiling.
type Chatter struct {
	ID    string `json:"id"`
	Login string `json:"login"`
}

// ChattersRequest is the input for the chatters.get verb.
type ChattersRequest struct {
	BroadcasterID string `json:"broadcaster_id"`
}

// ChattersReply is the reply for the chatters.get verb. MissingScope mirrors
// the reward verbs: the bot's token lacks moderator:read:chatters or the bot
// is not a moderator of the channel, so the caller should skip rather than
// retry.
type ChattersReply struct {
	Chatters     []Chatter `json:"chatters,omitempty"`
	MissingScope bool      `json:"missing_scope,omitempty"`
	Error        string    `json:"error,omitempty"`
}
