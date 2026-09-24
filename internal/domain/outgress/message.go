// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package outgress

import "ItsBagelBot/pkg/codec"

type Message struct {
	Origin          string           `json:"origin,omitempty"`
	TrialGeneration uint64           `json:"trial_generation,omitempty"`
	Type            string           `json:"type"`
	BroadcasterID   string           `json:"broadcaster_id"`
	Locale          string           `json:"locale,omitempty"`
	SenderID        string           `json:"sender_id"`
	Endpoint        string           `json:"endpoint"`
	Method          string           `json:"method"`
	Payload         codec.RawMessage `json:"payload"`
	As              string           `json:"as,omitempty"`
	Color           string           `json:"color,omitempty"`
	To              string           `json:"to,omitempty"`
	MsgID           string           `json:"msg_id,omitempty"`
	RewardID        string           `json:"reward_id,omitempty"`
	RedemptionID    string           `json:"redemption_id,omitempty"`
	Status          string           `json:"status,omitempty"`
}

type Batch struct {
	ID    string    `json:"id"`
	Items []Message `json:"items"`
}

func (b *Batch) Valid() bool {
	return b != nil && b.ID != "" && len(b.Items) > 0
}

type StreamStatusJob struct {
	BroadcasterID string `json:"broadcaster_id"`
}

const TokenWarmScope = "token-warm"

type EventSubJob struct {
	Enabled bool   `json:"enabled"`
	Mode    string `json:"mode,omitempty"`
}

const (
	TypeChat             = "chat"
	TypeBatch            = "batch"
	TypeAPI              = "api"
	TypeEventSub         = "eventsub"
	TypeStreamStatus     = "stream_status"
	TypeBan              = "ban"
	TypeTimeout          = "timeout"
	TypeUnban            = "unban"
	TypeAd               = "ad"
	TypeCommercial       = "commercial"
	TypeClip             = "clip"
	TypeChannelUpdate    = "channel_update"
	TypeStreamMarker     = "stream_marker"
	TypeAnnounce         = "announce"
	TypeShoutout         = "shoutout"
	TypePin              = "pin"
	TypeShieldMode       = "shield_mode"
	TypeDelete           = "delete"
	TypeWarn             = "warn"
	TypeRedemptionUpdate = "redemption_update"
)

const (
	RedemptionFulfilled = "FULFILLED"
	RedemptionCanceled  = "CANCELED"
)

const (
	ModeEnable         = "enable"
	ModeDisable        = "disable"
	ModeReconnect      = "reconnect"
	ModeEnsureOptional = "ensure_optional"
)

const (
	AsBot         = "bot"
	AsBroadcaster = "broadcaster"
	AsUser        = "user"
	AsApp         = "app"
)
