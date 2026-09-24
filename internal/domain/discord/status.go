// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

import (
	"time"

	"ItsBagelBot/pkg/codec"
)

const BotStatusKey = "discord:bot:status"

// Must stay in Valkey: Discord caps identifies per token per day, across restarts.
const BotConnectsKey = "discord:bot:connects"

const BotConnectsTTL = 25 * time.Hour

type BotStatus struct {
	Connected        bool   `json:"connected"`
	SinceUnixMS      int64  `json:"since_unix_ms,omitempty"`
	SessionID        string `json:"session_id,omitempty"`
	Resumes          int    `json:"resumes,omitempty"`
	GuildCount       int    `json:"guild_count,omitempty"`
	LastEventUnixMS  int64  `json:"last_event_unix_ms,omitempty"`
	LastCloseCode    int    `json:"last_close_code,omitempty"`
	LastCloseReason  string `json:"last_close_reason,omitempty"`
	HeartbeatUnixMS  int64  `json:"heartbeat_unix_ms,omitempty"`
	Pod              string `json:"pod,omitempty"`
	Flapping         bool   `json:"flapping,omitempty"`
	ConnectsInWindow int    `json:"connects_in_window,omitempty"`
	AtCeiling        bool   `json:"at_ceiling,omitempty"`
	ParkUntilUnixMS  int64  `json:"park_until_unix_ms,omitempty"`
}

const (
	BotHeartbeatInterval = 30 * time.Second
	BotHeartbeatMaxAge   = 90 * time.Second
	BotEventMaxAge       = 90 * time.Second
	BotFatalGrace        = 60 * time.Second
)

func EncodeBotStatus(s BotStatus) ([]byte, error) { return codec.Marshal(s) }

func DecodeBotStatus(raw []byte) (BotStatus, error) {
	var s BotStatus
	err := codec.Unmarshal(raw, &s)
	return s, err
}

func (s BotStatus) HeartbeatStale(now time.Time) bool {
	if !s.Connected || s.HeartbeatUnixMS == 0 {
		return false
	}
	return now.Sub(time.UnixMilli(s.HeartbeatUnixMS)) > BotHeartbeatMaxAge
}

func (s BotStatus) EventStale(now time.Time) bool {
	if !s.Connected || s.LastEventUnixMS == 0 {
		return false
	}
	return now.Sub(time.UnixMilli(s.LastEventUnixMS)) > BotEventMaxAge
}

const (
	CloseAuthenticationFailed = 4004
	CloseInvalidShard         = 4010
	CloseShardingRequired     = 4011
	CloseInvalidAPIVersion    = 4012
	CloseInvalidIntents       = 4013
	CloseDisallowedIntents    = 4014
)

func FatalCloseCode(code int) bool {
	switch code {
	case CloseAuthenticationFailed, CloseInvalidShard, CloseShardingRequired,
		CloseInvalidAPIVersion, CloseInvalidIntents, CloseDisallowedIntents:
		return true
	default:
		return false
	}
}

func CloseCodeMessage(code int) string {
	switch code {
	case CloseAuthenticationFailed:
		return "the bot token was rejected"
	case CloseInvalidShard:
		return "the bot sent an invalid shard"
	case CloseShardingRequired:
		return "the bot needs to be resharded"
	case CloseInvalidAPIVersion:
		return "the bot used an unsupported Discord API version"
	case CloseInvalidIntents, CloseDisallowedIntents:
		return "the bot's privileged intents are not enabled"
	default:
		return ""
	}
}
