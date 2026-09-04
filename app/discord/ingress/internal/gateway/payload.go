// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package gateway

import "ItsBagelBot/pkg/codec"

// Intents the gateway identifies with: guilds, members, voice states,
// messages, and message content. Members and message content are privileged
// and must be enabled on the Discord application.
const Intents = 1 | 2 | 128 | 512 | 32768

const (
	opDispatch       = 0
	opHeartbeat      = 1
	opIdentify       = 2
	opPresenceUpdate = 3
	opReconnect      = 7
	opInvalidSession = 9
	opHello          = 10
	opHeartbeatAck   = 11
	gatewayURL       = "wss://gateway.discord.gg/?v=10&encoding=json"
	// activityTypeWatching is Discord's Activity Type 3, "Watching {name}" --
	// the client prepends "Watching" itself, so the activity name we send
	// must not repeat it (see presenceUpdateBody). Verified against
	// https://docs.discord.com/developers/events/gateway-events, the current
	// home of what used to be discord.com/developers/docs/topics/gateway-events
	// (that path now 301s here).
	activityTypeWatching   = 3
	eventReady             = "READY"
	eventGuildCreate       = "GUILD_CREATE"
	eventMemberAdd         = "GUILD_MEMBER_ADD"
	eventMemberRemove      = "GUILD_MEMBER_REMOVE"
	eventVoiceState        = "VOICE_STATE_UPDATE"
	eventMessageCreate     = "MESSAGE_CREATE"
	eventMessageDelete     = "MESSAGE_DELETE"
	eventMessageUpdate     = "MESSAGE_UPDATE"
	eventInteractionCreate = "INTERACTION_CREATE"
)

type packet struct {
	Op int              `json:"op"`
	S  *int             `json:"s,omitempty"`
	T  string           `json:"t,omitempty"`
	D  codec.RawMessage `json:"d,omitempty"`
}

type helloData struct {
	HeartbeatInterval int `json:"heartbeat_interval"`
}

type readyData struct {
	SessionID   string `json:"session_id"`
	Application struct {
		ID string `json:"id"`
	} `json:"application"`
	User struct {
		ID string `json:"id"`
	} `json:"user"`
}

func identifyBody(token string) map[string]any {
	return map[string]any{
		"op": opIdentify,
		"d": map[string]any{
			"token":   token,
			"intents": Intents,
			"properties": map[string]string{
				"os":      "linux",
				"browser": "itsbagelbot",
				"device":  "itsbagelbot",
			},
		},
	}
}

func heartbeatBody(seq *int) map[string]any {
	return map[string]any{"op": opHeartbeat, "d": seq}
}

// presenceUpdateBody builds a Gateway Update Presence (op 3) frame showing a
// single "Watching <name>" activity, per the Gateway Presence Update
// Structure (since/activities/status/afk) and Activity Object documented at
// https://docs.discord.com/developers/events/gateway-events. since/afk are
// meaningless for a bot account (they describe a human user going idle), so
// they are sent at their "not idle" zero values rather than omitted --
// Discord's docs don't mark either optional on this op.
func presenceUpdateBody(name string) map[string]any {
	return map[string]any{
		"op": opPresenceUpdate,
		"d": map[string]any{
			"since": nil,
			"activities": []map[string]any{
				{"name": name, "type": activityTypeWatching},
			},
			"status": "online",
			"afk":    false,
		},
	}
}
