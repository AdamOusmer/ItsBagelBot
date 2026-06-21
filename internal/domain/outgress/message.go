// Package outgress holds the canonical pub-sub wire types for the outgress lanes.
// Every producer that enqueues a Twitch Helix call or EventSub job publishes a
// Message onto the appropriate watermill subject; workers in each lane decode it.
package outgress

import "encoding/json"

// Message is the wire contract every producer publishes on the outgress subjects.
// Workers decode this struct from the watermill message payload.
type Message struct {
	Type          string          `json:"type"`           // "chat", "api" or "eventsub"
	BroadcasterID string          `json:"broadcaster_id"` // target channel
	SenderID      string          `json:"sender_id"`      // the bot's user ID
	Endpoint      string          `json:"endpoint"`       // Helix path, e.g. "/helix/chat/messages"
	Method        string          `json:"method"`         // HTTP method
	Payload       json.RawMessage `json:"payload"`        // raw JSON body
	// As selects whose token the call runs under: "bot", "broadcaster" (alias
	// "user"), or "app". Empty routes by endpoint (the default). Use it to send
	// chat as the streamer ("broadcaster") instead of the bot.
	As string `json:"as,omitempty"`
}

// EventSubJob is the payload of an "eventsub" message: the receive toggle's
// intent for one channel. Enabled creates the channel's subscriptions on the
// Conduit, disabled deletes them.
//
// Mode, when set, overrides the legacy Enabled field:
//
//	"enable"    - create all eventsub subscriptions (same as Enabled:true)
//	"disable"   - delete all eventsub subscriptions (same as Enabled:false)
//	"reconnect" - atomic drop-then-recreate with single-flight and persisted state
type EventSubJob struct {
	Enabled bool   `json:"enabled"`
	Mode    string `json:"mode,omitempty"` // "enable" | "disable" | "reconnect"; empty => legacy Enabled
}

// Message type values. Producers set Message.Type to one of these.
const (
	TypeChat       = "chat"
	TypeAPI        = "api"
	TypeEventSub   = "eventsub"
	TypeBan        = "ban"
	TypeTimeout    = "timeout"
	TypeUnban      = "unban"
	TypeAd         = "ad"
	TypeCommercial = "commercial"
	TypeClip       = "clip"
)

// EventSubJob mode values for the Mode field.
const (
	ModeEnable    = "enable"
	ModeDisable   = "disable"
	ModeReconnect = "reconnect"
)

// As field values recognized by ParseIdentity in the Twitch client.
// These are the exact strings the worker passes to twitch.ParseIdentity.
const (
	AsBot         = "bot"
	AsBroadcaster = "broadcaster"
	AsUser        = "user" // alias for AsBroadcaster
	AsApp         = "app"
)
