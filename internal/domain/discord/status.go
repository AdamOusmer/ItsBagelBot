// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

import (
	"time"

	"ItsBagelBot/pkg/codec"
)

// BotStatusKey is the one Valkey key describing the fleet's single gateway
// Identify session. Written only by discord-ingress (the process that holds
// that session), read by discord-outgress's dashboard-facing status RPC.
//
// No TTL. An expiring key would read as "no bot" during the exact window an
// operator most wants the last close code -- the socket is down and nothing
// is refreshing anything. Staleness is carried in-band instead, by
// HeartbeatUnixMS: a reader that finds a heartbeat older than
// BotHeartbeatMaxAge knows ingress itself stopped, which is a different
// (and louder) fact than "the key expired".
const BotStatusKey = "discord:bot:status"

// BotStatus is the gateway session's state as ingress last observed it.
type BotStatus struct {
	Connected bool `json:"connected"`
	// SinceUnixMS is when the current connection came up (READY or RESUMED),
	// not when the process started: the dashboard shows it as "online for".
	SinceUnixMS     int64  `json:"since_unix_ms,omitempty"`
	SessionID       string `json:"session_id,omitempty"`
	Resumes         int    `json:"resumes,omitempty"`
	GuildCount      int    `json:"guild_count,omitempty"`
	LastEventUnixMS int64  `json:"last_event_unix_ms,omitempty"`
	// LastCloseCode is the WebSocket close code of the most recent socket
	// death, 0 when the socket died without one (network drop, read
	// timeout). It survives across reconnects deliberately: a 4014 that a
	// later reconnect papered over is still the thing an operator needs to
	// see.
	LastCloseCode   int    `json:"last_close_code,omitempty"`
	LastCloseReason string `json:"last_close_reason,omitempty"`
	HeartbeatUnixMS int64  `json:"heartbeat_unix_ms,omitempty"`
	Pod             string `json:"pod,omitempty"`
}

const (
	// BotHeartbeatInterval paces ingress's rewrite of the key. It is
	// deliberately shorter than Discord's own ~41s gateway heartbeat: the
	// key must go stale because *ingress* stopped, not because one gateway
	// beat happened to land late.
	BotHeartbeatInterval = 30 * time.Second
	// BotHeartbeatMaxAge is three missed writes. Two would trip on a single
	// Valkey failover (sentinel promotion is seconds, but the client's
	// reconnect plus one retry can eat a whole interval), which is a false
	// alarm that restarts a perfectly healthy gateway pod.
	BotHeartbeatMaxAge = 90 * time.Second
	// BotFatalGrace is how long a fatal close is tolerated before liveness
	// fails and the kubelet restarts the pod. It is not zero because the
	// fix for every fatal code is a new token or new intents, which arrives
	// as a Doppler secret change that restarts the pod anyway -- restarting
	// instantly would just crash-loop ahead of that rotation and bury the
	// one ERROR line that explains what happened.
	BotFatalGrace = 60 * time.Second
)

// EncodeBotStatus renders the key's value.
func EncodeBotStatus(s BotStatus) ([]byte, error) { return codec.Marshal(s) }

// DecodeBotStatus parses the key's value.
func DecodeBotStatus(raw []byte) (BotStatus, error) {
	var s BotStatus
	err := codec.Unmarshal(raw, &s)
	return s, err
}

// HeartbeatStale reports whether ingress stopped refreshing the key. Only
// meaningful while Connected: a disconnected bot is already reported as
// down by Connected itself, and a stale heartbeat on top of that adds
// nothing.
func (s BotStatus) HeartbeatStale(now time.Time) bool {
	if !s.Connected || s.HeartbeatUnixMS == 0 {
		return false
	}
	return now.Sub(time.UnixMilli(s.HeartbeatUnixMS)) > BotHeartbeatMaxAge
}

// Fatal close codes. Discord closes with these when reconnecting cannot
// possibly help: the token, the shard, the API version or the intents are
// wrong, and every one of them needs a human or a secret rotation.
// Verified against https://docs.discord.com/developers/topics/opcodes-and-status-codes.
const (
	CloseAuthenticationFailed = 4004
	CloseInvalidShard         = 4010
	CloseShardingRequired     = 4011
	CloseInvalidAPIVersion    = 4012
	CloseInvalidIntents       = 4013
	CloseDisallowedIntents    = 4014
)

// FatalCloseCode reports whether a close code means "do not reconnect".
// Everything else -- 4000 unknown error, 4007 invalid seq, 4009 session
// timeout, or no code at all -- is a socket Discord expects us to come
// back on.
func FatalCloseCode(code int) bool {
	switch code {
	case CloseAuthenticationFailed, CloseInvalidShard, CloseShardingRequired,
		CloseInvalidAPIVersion, CloseInvalidIntents, CloseDisallowedIntents:
		return true
	default:
		return false
	}
}

// CloseCodeMessage explains a close code in the words the dashboard shows a
// streamer. An unknown code returns "", so callers fall back to the raw
// number rather than printing a wrong explanation.
func CloseCodeMessage(code int) string {
	switch code {
	case CloseAuthenticationFailed:
		return "the bot token was rejected"
	case CloseInvalidShard, CloseShardingRequired:
		return "the bot needs to be resharded"
	case CloseInvalidAPIVersion:
		return "the bot used an unsupported Discord API version"
	case CloseInvalidIntents, CloseDisallowedIntents:
		return "the bot's privileged intents are not enabled"
	default:
		return ""
	}
}
