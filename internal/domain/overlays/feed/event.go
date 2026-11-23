// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package feed

import (
	"crypto/rand"
	"errors"
	"reflect"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

type Kind string

const (
	Follow        Kind = "follow"
	Sub           Kind = "sub"
	Resub         Kind = "resub"
	GiftSub       Kind = "gift_sub"
	CommunityGift Kind = "community_gift"
	Cheer         Kind = "cheer"
	Raid          Kind = "raid"
	Chat          Kind = "chat"
	Redemption    Kind = "redemption"
	UserCleared   Kind = "user_cleared"
)

func (k Kind) Valid() bool {
	switch k {
	case Follow, Sub, Resub, GiftSub, CommunityGift, Cheer, Raid, Chat, Redemption, UserCleared:
		return true
	}
	return false
}

type User struct {
	ID    string `json:"id,omitempty"`
	Login string `json:"login,omitempty"`
	Name  string `json:"name,omitempty"`
	Color string `json:"color,omitempty"`
}

type Recipient struct {
	ID    string `json:"id,omitempty"`
	Login string `json:"login,omitempty"`
	Name  string `json:"name,omitempty"`
}

type Part struct {
	Type string `json:"t"`
	ID   string `json:"id,omitempty"`
	Text string `json:"text,omitempty"`
}

type Message struct {
	Text  string `json:"text,omitempty"`
	Parts []Part `json:"parts,omitempty"`
}

type Reward struct {
	ID    string `json:"id,omitempty"`
	Title string `json:"title,omitempty"`
}

type Fields struct {
	User      *User      `json:"user,omitempty"`
	Anonymous bool       `json:"anonymous,omitempty"`
	Upgrade   bool       `json:"upgrade,omitempty"`
	Amount    int64      `json:"amount,omitempty"`
	Tier      string     `json:"tier,omitempty"`
	Months    int        `json:"months,omitempty"`
	Streak    int        `json:"streak,omitempty"`
	Recipient *Recipient `json:"recipient,omitempty"`
	Message   *Message   `json:"message,omitempty"`
	Badges    []string   `json:"badges,omitempty"`
	Reward    *Reward    `json:"reward,omitempty"`
}

// Event is the overlay-facing alert or chat payload; At is when the channel event occurred and orders latest-of-each.
type Event struct {
	ID   string    `json:"id"`
	Kind Kind      `json:"kind"`
	At   time.Time `json:"at"`
	Fields
}

// Delivery is an Event as sent to a viewer connection.
type Delivery struct {
	Event
	Test   bool `json:"test,omitempty"`
	Replay bool `json:"replay,omitempty"`
}

func (e Event) Version() int64 { return e.At.UnixMilli() }

var (
	errEvent  = errors.New("invalid overlay event")
	color     = regexp.MustCompile("^#[0-9a-fA-F]{6}$")
	badgeUUID = regexp.MustCompile("^[0-9a-fA-F]{8}(-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}$")
)

func (e Event) Validate() error {
	if !e.Kind.Valid() || e.At.IsZero() || e.ID == "" || len(e.ID) > 96 {
		return errEvent
	}
	if !e.Fields.valid() || !e.validKindFields() {
		return errEvent
	}
	return nil
}

func (e Event) validKindFields() bool {
	switch {
	case (e.Reward != nil) != (e.Kind == Redemption):
		return false
	case (e.Recipient != nil) != (e.Kind == GiftSub):
		return false
	case e.Kind == UserCleared:
		return e.User != nil && e.User.ID != "" && e.Fields.withoutUser()
	}
	return true
}

func (f Fields) withoutUser() bool {
	f.User = nil
	return reflect.ValueOf(f).IsZero()
}

func (f Fields) valid() bool {
	return f.User.valid() && f.validPayload()
}

func (f Fields) validPayload() bool {
	return min(f.Amount, int64(f.Months), int64(f.Streak)) >= 0 && ValidTier(f.Tier) && f.Message.valid() && validBadges(f.Badges)
}

func (u *User) valid() bool {
	return u == nil || u.Color == "" || color.MatchString(u.Color)
}

func ValidTier(tier string) bool {
	switch tier {
	case "", "1000", "2000", "3000", "prime":
		return true
	}
	return false
}

func (m *Message) valid() bool {
	if m == nil {
		return true
	}
	if len(m.Text) > 500 || len(m.Parts) > 60 {
		return false
	}
	remaining := 500
	for _, part := range m.Parts {
		remaining -= len(part.Text)
		if !part.valid() || remaining < 0 {
			return false
		}
	}
	return true
}

func (p Part) valid() bool {
	switch p.Type {
	case "text":
		return p.ID == ""
	case "emote":
		return p.ID != ""
	}
	return false
}

func validBadges(badges []string) bool {
	for _, badge := range badges {
		if !validBadge(badge) {
			return false
		}
	}
	return true
}

func validBadge(badge string) bool {
	switch badge {
	case "broadcaster", "moderator", "vip", "subscriber", "founder":
		return true
	}
	return badgeUUID.MatchString(badge)
}

// Emulated builds a test event with a fresh id and the current time.
func Emulated(kind Kind, fields Fields) Event {
	fields.Message = fields.Message.bounded()
	switch {
	case kind == GiftSub && fields.Recipient == nil:
		fields.Recipient = &Recipient{}
	case kind == Redemption && fields.Reward == nil:
		fields.Reward = &Reward{}
	case kind == UserCleared && fields.User == nil:
		fields.User = &User{ID: rand.Text()}
	}
	return Event{ID: rand.Text(), Kind: kind, At: time.Now().UTC(), Fields: fields}
}

func (m *Message) bounded() *Message {
	if m == nil {
		return nil
	}
	out := &Message{Text: truncate(m.Text, 500)}
	remaining := 500
	for _, p := range m.Parts[:min(len(m.Parts), 60)] {
		p.Text = truncate(p.Text, remaining)
		out.Parts = append(out.Parts, p)
		remaining -= len(p.Text)
	}
	return out
}

func truncate(s string, limit int) string {
	s = strings.ToValidUTF8(s, string(utf8.RuneError))
	if len(s) <= limit {
		return s
	}
	s = s[:limit]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}
