// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package gateway

import "ItsBagelBot/pkg/codec"

const Intents = 1 | 2 | 128 | 512 | 32768

const (
	opDispatch             = 0
	opHeartbeat            = 1
	opIdentify             = 2
	opResume               = 6
	opPresenceUpdate       = 3
	opReconnect            = 7
	opInvalidSession       = 9
	opHello                = 10
	opHeartbeatAck         = 11
	gatewayURL             = "wss://gateway.discord.gg/?v=10&encoding=json"
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
	eventResumed           = "RESUMED"
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
	SessionID string `json:"session_id"`
	Guilds    []struct {
		ID string `json:"id"`
	} `json:"guilds,omitempty"`
	ResumeGatewayURL string `json:"resume_gateway_url"`
	Application      struct {
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

func resumeBody(token, sessionID string, seq *int) map[string]any {
	return map[string]any{
		"op": opResume,
		"d": map[string]any{
			"token":      token,
			"session_id": sessionID,
			"seq":        seq,
		},
	}
}

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
