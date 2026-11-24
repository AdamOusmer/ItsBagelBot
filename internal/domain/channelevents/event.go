// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package channelevents

import (
	"errors"
	"time"

	"ItsBagelBot/pkg/codec"
)

type Kind string

const (
	Follow        Kind = "follow"
	Sub           Kind = "sub"
	Resub         Kind = "resub"
	CommunityGift Kind = "community_gift"
	GiftUpgrade   Kind = "gift_upgrade"
	Cheer         Kind = "cheer"
	Raid          Kind = "raid"
	Redemption    Kind = "redemption"
	Session       Kind = "session"
	Chat          Kind = "chat"

	HypeTrain  Kind = "hype_train"
	Goal       Kind = "goal"
	Charity    Kind = "charity"
	Poll       Kind = "poll"
	Prediction Kind = "prediction"
	Shoutout   Kind = "shoutout"
	AdBreak    Kind = "ad_break"
)

func Kinds() []Kind {
	return []Kind{Follow, Sub, Resub, CommunityGift, GiftUpgrade, Cheer, Raid, Redemption, Session, Chat,
		HypeTrain, Goal, Charity, Poll, Prediction, Shoutout, AdBreak}
}

func (k Kind) Valid() bool {
	for _, kind := range Kinds() {
		if k == kind {
			return true
		}
	}
	return false
}

type SessionState string

const (
	SessionStarted SessionState = "started"
	SessionEnded   SessionState = "ended"
)

// User is keyed by Twitch user id; login and name are display data only.
type User struct {
	ID    string `json:"id,omitempty"`
	Login string `json:"login,omitempty"`
	Name  string `json:"name,omitempty"`
}

type Fragment struct {
	Type string `json:"t"`
	ID   string `json:"id,omitempty"`
	Text string `json:"text,omitempty"`
}

type Message struct {
	Text      string     `json:"text,omitempty"`
	Fragments []Fragment `json:"parts,omitempty"`
}

type Reward struct {
	ID    string `json:"id,omitempty"`
	Title string `json:"title,omitempty"`
	Cost  int64  `json:"cost,omitempty"`
	Input string `json:"input,omitempty"`
}

type SessionChange struct {
	State         SessionState `json:"state"`
	FollowerCount int64        `json:"follower_count,omitempty"`
	SubCount      int64        `json:"sub_count,omitempty"`
}

// Event is one normalized channel event. ID is the Twitch event identity and doubles as the Nats-Msg-Id.
// Amount is bits for cheer, gifted subs for community_gift and viewers for raid.
type Event struct {
	ID         string    `json:"id"`
	Kind       Kind      `json:"kind"`
	ChannelID  string    `json:"channel_id"`
	OccurredAt time.Time `json:"occurred_at"`
	Trial      bool      `json:"trial,omitempty"`

	User            *User          `json:"user,omitempty"`
	Gifter          *User          `json:"gifter,omitempty"`
	Anonymous       bool           `json:"anonymous,omitempty"`
	Tier            string         `json:"tier,omitempty"`
	Prime           bool           `json:"prime,omitempty"`
	Months          int            `json:"months,omitempty"`
	Streak          int            `json:"streak,omitempty"`
	Amount          int64          `json:"amount,omitempty"`
	CumulativeTotal int64          `json:"cumulative_total,omitempty"`
	Recipients      []User         `json:"recipients,omitempty"`
	Message         *Message       `json:"message,omitempty"`
	Badges          []string       `json:"badges,omitempty"`
	Color           string         `json:"color,omitempty"`
	Reward          *Reward        `json:"reward,omitempty"`
	Change          *SessionChange `json:"session,omitempty"`

	Detail codec.RawMessage `json:"detail,omitempty"`
}

var errEvent = errors.New("invalid channel event")

func Decode(raw []byte) (Event, error) {
	var event Event
	if err := codec.UnmarshalStrict(raw, &event); err != nil {
		return Event{}, err
	}
	return event, event.Validate()
}

func (e Event) hasHeader() bool {
	return e.ID != "" && e.ChannelID != "" && !e.OccurredAt.IsZero() && e.Kind.Valid()
}

func (e Event) Validate() error {
	if !e.hasHeader() {
		return errEvent
	}
	if e.Kind == Session && e.Change == nil {
		return errEvent
	}
	return nil
}

// Version orders events of one channel for version-guarded writes.
func (e Event) Version() int64 { return e.OccurredAt.UnixMilli() }
