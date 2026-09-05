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
	// Flapping is true while ingress is holding back its reconnects because
	// consecutive sessions kept dying young. It is on the key rather than
	// only in the logs because "offline" and "offline, and the process has
	// deliberately backed off to one attempt every five minutes" need
	// different answers from whoever is looking, and on 2026-09-05 the
	// difference was a token reset nobody saw coming.
	Flapping bool `json:"flapping,omitempty"`
	// ConnectsInWindow is how many gateway connects this pod has opened in
	// the last 24h. Discord's own limit is 1000/day and they enforce it by
	// resetting the token, so this is the number an operator wants before
	// restarting a pod "just to see".
	ConnectsInWindow int `json:"connects_in_window,omitempty"`
	// AtCeiling is true once ConnectsInWindow has spent the whole rolling
	// allowance and ingress has stopped dialling until the window frees.
	// Unlike Flapping this is not a slow-down, it is a stop: the bot will
	// not come back on its own before ParkUntilUnixMS, so readiness fails
	// on it rather than reporting a pod that is quietly doing nothing.
	AtCeiling bool `json:"at_ceiling,omitempty"`
	// ParkUntilUnixMS is when the connect budget will next allow a socket,
	// zero when nothing beyond the ordinary 5s identify spacing is holding
	// one back. "Offline, back at 14:07" and "offline" need different
	// answers from whoever is reading, and only this pod knows the deadline.
	ParkUntilUnixMS int64 `json:"park_until_unix_ms,omitempty"`
}

const (
	// BotHeartbeatInterval paces ingress's rewrite of the key. It is
	// deliberately shorter than Discord's own ~41s gateway heartbeat: the
	// key must go stale because *ingress* stopped, not because one gateway
	// beat happened to land late.
	BotHeartbeatInterval = 30 * time.Second
	// BotHeartbeatMaxAge is three missed writes (3 x 30s). Two would trip on
	// a single Valkey failover (sentinel promotion is seconds, but the
	// client's reconnect plus one retry can eat a whole interval), which is
	// a false alarm that restarts a perfectly healthy gateway pod. It
	// answers one question only -- "is the ingress process still running?"
	// -- because that is all a republish ticker can prove.
	BotHeartbeatMaxAge = 90 * time.Second
	// BotEventMaxAge is how long a *connected* socket may produce nothing
	// before it counts as wedged. Also 90s, but for a different reason:
	// Discord's gateway hands out a 41.25s heartbeat interval, and every
	// heartbeat ACK it sends back counts as an event here, so even a bot in
	// a silent guild produces one roughly every 41s. 90s is two of those
	// windows plus slack for a late ACK; one window (45s) would fire on
	// every ACK that arrives a beat late, which is a restart of a healthy
	// pod.
	//
	// This, not BotHeartbeatMaxAge, is the liveness gate. The republish
	// ticker keeps ticking inside a process whose socket has silently
	// stopped delivering -- that is the exact failure liveness exists to
	// catch, and gating on the ticker's own beat cannot see it.
	BotEventMaxAge = 90 * time.Second
	// BotFatalGrace is how long a fatal close is tolerated before liveness
	// fails and the kubelet restarts the pod. 60s, not zero, because the
	// fix for every fatal code is a new token or new intents, which arrives
	// as a Doppler secret change that restarts the pod anyway -- restarting
	// instantly would just crash-loop ahead of that rotation and bury the
	// one ERROR line that explains what happened. 60s is long enough for an
	// operator to read that line out of a pod that is still standing and
	// short enough that a pod nobody ever rotates does not sit dead for an
	// hour.
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

// EventStale reports whether a socket that still claims to be connected has
// stopped producing anything -- dispatches or gateway heartbeat ACKs alike
// (see BotEventMaxAge). Only meaningful while Connected, for the same reason
// HeartbeatStale is: a disconnected bot is already reported down by
// Connected, and a quiet one on top of that adds nothing.
//
// LastEventUnixMS of 0 reads as not-stale rather than as stale: a status
// written before the first event ever landed (an Up that has not yet seen
// traffic) has no evidence either way, and guessing "wedged" there would
// fail readiness on every healthy cold start.
func (s BotStatus) EventStale(now time.Time) bool {
	if !s.Connected || s.LastEventUnixMS == 0 {
		return false
	}
	return now.Sub(time.UnixMilli(s.LastEventUnixMS)) > BotEventMaxAge
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
	case CloseInvalidShard:
		// 4010 and 4011 are different faults and had the same explanation
		// here, which sent an operator hunting for a shard count that was
		// never the problem: 4010 is a shard id/count this Identify sent
		// that does not add up, 4011 is Discord saying the bot is now too
		// large for one connection. Only 4011 means "reshard".
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
