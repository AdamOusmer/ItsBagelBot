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
	// Color is the announce color (primary, blue, green, orange, purple);
	// empty for non-announce.
	Color string `json:"color,omitempty"`
	// To is the shoutout target (login or id); outgress resolves login to id.
	To string `json:"to,omitempty"`
	// MsgID is the Twitch chat message id a "delete" targets; outgress puts it
	// on the query string.
	MsgID string `json:"msg_id,omitempty"`
	// Channel-points redemption fields, set only for a "redemption_update" message
	// (see TypeRedemptionUpdate): RewardID is the custom reward, RedemptionID the
	// specific redemption to resolve, and Status the new state ("FULFILLED" or
	// "CANCELED"). All three ride the query string, no body.
	RewardID     string `json:"reward_id,omitempty"`
	RedemptionID string `json:"redemption_id,omitempty"`
	Status       string `json:"status,omitempty"`
}

// Batch is the shared producer/consumer contract for an ordered, at-most-once
// group. ID keys ownership and progress; Items execute in slice order.
type Batch struct {
	ID    string    `json:"id"`
	Items []Message `json:"items"`
}

func (b *Batch) Valid() bool {
	return b != nil && b.ID != "" && len(b.Items) > 0
}

// StreamStatusJob is the payload of a "stream_status" message: a request for
// outgress to resolve one broadcaster's current live state from Twitch (Helix
// Get Streams) and write it back into the shared live projection. It carries no
// reply subject; the result is published as a Valkey write + a cache
// invalidation, so producers (the worker's key-expiry watcher, the projector's
// cold-miss escalation) fire and forget.
type StreamStatusJob struct {
	BroadcasterID string `json:"broadcaster_id"`
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
	TypeChat         = "chat"
	TypeBatch        = "batch"
	TypeAPI          = "api"
	TypeEventSub     = "eventsub"
	TypeStreamStatus = "stream_status"
	TypeBan          = "ban"
	TypeTimeout      = "timeout"
	TypeUnban        = "unban"
	TypeAd           = "ad"
	TypeCommercial   = "commercial"
	TypeClip         = "clip"
	TypeAnnounce     = "announce"
	TypeShoutout     = "shoutout"
	// TypeShieldMode activates a channel's Shield Mode: one PUT that gates the
	// whole channel (blocks non-followers/new accounts) instead of banning a
	// mass-raid account by account, which would blow the shared Helix budget.
	TypeShieldMode = "shield_mode"
	// TypeDelete removes one chat message (Helix Delete Chat Messages); the
	// target message id rides Message.MsgID.
	TypeDelete = "delete"
	// TypeWarn issues a Twitch chat warning the target must acknowledge before
	// chatting again (Helix Warn Chat User); body {"data":{"user_id","reason"}}.
	TypeWarn = "warn"
	// TypeRedemptionUpdate resolves one channel-points redemption in the reward's
	// request queue (Helix Update Redemption Status): the bot marks it FULFILLED
	// or CANCELED (a refund) after running the reward's action. Runs under the
	// broadcaster token (channel:manage:redemptions); RewardID + RedemptionID +
	// Status ride the query string / body.
	TypeRedemptionUpdate = "redemption_update"
)

// Redemption status values for a TypeRedemptionUpdate Message.Status.
const (
	RedemptionFulfilled = "FULFILLED"
	RedemptionCanceled  = "CANCELED"
)

// EventSubJob mode values for the Mode field.
const (
	ModeEnable    = "enable"
	ModeDisable   = "disable"
	ModeReconnect = "reconnect"
	// ModeEnsureOptional (re)creates only the optional subscriptions (e.g. the
	// channel-points redemption sub) without touching the mandatory set. It is
	// how a broadcaster who re-consents with channel:read:redemptions after
	// already being enrolled picks up the redemption sub, without paying a full
	// drop-and-recreate. Creates are 409-idempotent, and a permanent rejection
	// (a non-affiliate with no channel points) is tolerated, not marked failing.
	ModeEnsureOptional = "ensure_optional"
)

// As field values recognized by ParseIdentity in the Twitch client.
// These are the exact strings the worker passes to twitch.ParseIdentity.
const (
	AsBot         = "bot"
	AsBroadcaster = "broadcaster"
	AsUser        = "user" // alias for AsBroadcaster
	AsApp         = "app"
)
