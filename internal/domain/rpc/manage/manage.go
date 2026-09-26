// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package manage

import (
	"time"

	"ItsBagelBot/internal/domain/rpc"
)

type GrantState string

const (
	GrantUnknown GrantState = ""
	GrantDead    GrantState = "dead"
)

type Channel struct {
	BroadcasterID string     `json:"broadcaster_id"`
	Enabled       bool       `json:"enabled"`
	IsMod         bool       `json:"is_mod"`
	ModCheckedAt  time.Time  `json:"mod_checked_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	SubState      string     `json:"sub_state"`
	SubError      string     `json:"sub_error"`
	SubCheckedAt  time.Time  `json:"sub_checked_at"`
	GrantState    GrantState `json:"grant_state"`
}

type ChannelRequest struct {
	BroadcasterID string `json:"broadcaster_id"`
	Enabled       *bool  `json:"enabled,omitempty"`
	IsMod         *bool  `json:"is_mod,omitempty"`
}

type ChannelReply struct {
	Channel *Channel `json:"channel,omitempty"`
	Found   bool     `json:"found"`
	Error   string   `json:"error,omitempty"`
}

type ChannelListReply struct {
	Channels []Channel `json:"channels"`
	Error    string    `json:"error,omitempty"`
}

type SystemStatusReply struct {
	Paused                   bool   `json:"paused"`
	AppTokenExpiresInSeconds int64  `json:"app_token_expires_in_seconds"`
	HasUserToken             bool   `json:"has_user_token"`
	Error                    string `json:"error,omitempty"`
}

type SystemPauseRequest struct {
	Paused bool `json:"paused"`
}

type SystemPauseReply struct {
	Paused bool   `json:"paused"`
	Error  string `json:"error,omitempty"`
}

type Reward struct {
	ID                         string `json:"id,omitempty"`
	Title                      string `json:"title"`
	Cost                       int    `json:"cost"`
	Prompt                     string `json:"prompt,omitempty"`
	BackgroundColor            string `json:"background_color,omitempty"`
	IsEnabled                  bool   `json:"is_enabled"`
	IsPaused                   bool   `json:"is_paused"`
	IsUserInputRequired        bool   `json:"is_user_input_required"`
	ShouldSkipQueue            bool   `json:"should_skip_queue"`
	MaxPerStreamEnabled        bool   `json:"max_per_stream_enabled"`
	MaxPerStream               int    `json:"max_per_stream"`
	MaxPerUserPerStreamEnabled bool   `json:"max_per_user_per_stream_enabled"`
	MaxPerUserPerStream        int    `json:"max_per_user_per_stream"`
	GlobalCooldownEnabled      bool   `json:"global_cooldown_enabled"`
	GlobalCooldownSeconds      int    `json:"global_cooldown_seconds"`
}

type RewardRequest struct {
	BroadcasterID string  `json:"broadcaster_id"`
	RewardID      string  `json:"reward_id,omitempty"`
	Reward        *Reward `json:"reward,omitempty"`
}

type RewardReply struct {
	Reward       *Reward  `json:"reward,omitempty"`
	Rewards      []Reward `json:"rewards,omitempty"`
	MissingScope bool     `json:"missing_scope,omitempty"`
	rpc.Refusal
}

type Chatter struct {
	ID    string `json:"id"`
	Login string `json:"login"`
}

// ChattersRequest asks for one bounded attendance page. Identity and deadline
// survive queueing and retries; a cursor belongs to this broadcaster/window.
type ChattersRequest struct {
	BroadcasterID     string `json:"broadcaster_id"`
	RequestID         string `json:"request_id"`
	WindowID          string `json:"window_id"`
	SessionGeneration string `json:"session_generation,omitempty"`
	LiveSession       string `json:"live_session,omitempty"`
	DeadlineUnixMilli int64  `json:"deadline_unix_milli"`
	Cursor            string `json:"cursor,omitempty"`
	CheckLive         bool   `json:"check_live,omitempty"`
}

// ChattersReply preserves request identity and explicitly distinguishes a page
// from a completed listing. A successful live check remains usable even if the
// attendance page subsequently fails; callers must fence it by generation.
type ChattersReply struct {
	BroadcasterID            string    `json:"broadcaster_id"`
	RequestID                string    `json:"request_id"`
	WindowID                 string    `json:"window_id"`
	SessionGeneration        string    `json:"session_generation,omitempty"`
	LiveSession              string    `json:"live_session,omitempty"`
	Chatters                 []Chatter `json:"chatters,omitempty"`
	Complete                 bool      `json:"complete"`
	NextCursor               string    `json:"next_cursor,omitempty"`
	CheckedAtUnixMilli       int64     `json:"checked_at_unix_milli,omitempty"`
	StreamID                 string    `json:"stream_id,omitempty"`
	StreamStartedAtUnixMilli int64     `json:"stream_started_at_unix_milli,omitempty"`
	Live                     bool      `json:"live"`
	MissingScope             bool      `json:"missing_scope,omitempty"`
	Error                    string    `json:"error,omitempty"`
	ErrorCode                string    `json:"error_code,omitempty"`
	RetryAtUnixMilli         int64     `json:"retry_at_unix_milli,omitempty"`
}
