// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package feed

import "ItsBagelBot/internal/domain/channelevents"

var frameKinds = map[channelevents.Kind]Kind{
	channelevents.Follow:        Follow,
	channelevents.Sub:           Sub,
	channelevents.GiftUpgrade:   Sub,
	channelevents.Resub:         Resub,
	channelevents.CommunityGift: CommunityGift,
	channelevents.Cheer:         Cheer,
	channelevents.Raid:          Raid,
	channelevents.Redemption:    Redemption,
	channelevents.Chat:          Chat,
}

// FromChannel maps a normalized channel event to its overlay event; kinds with no overlay form report false.
func FromChannel(e channelevents.Event) (Event, bool) {
	kind, ok := frameKinds[e.Kind]
	if !ok {
		return Event{}, false
	}
	fields := Fields{
		Anonymous: e.Anonymous,
		Upgrade:   e.Kind == channelevents.GiftUpgrade,
		Amount:    e.Amount,
		Tier:      tierOf(e),
		Months:    e.Months,
		Streak:    e.Streak,
		Message:   messageOf(e.Message),
		Badges:    e.Badges,
	}
	if !e.Anonymous {
		fields.User = userOf(e.User, e.Color)
	}
	if e.Reward != nil {
		fields.Reward = &Reward{ID: e.Reward.ID, Title: e.Reward.Title}
		fields.Amount = e.Reward.Cost
	}
	return Event{ID: e.ID, Kind: kind, At: e.OccurredAt.UTC(), Fields: fields}, true
}

func tierOf(e channelevents.Event) string {
	if e.Prime {
		return "prime"
	}
	return e.Tier
}

func userOf(u *channelevents.User, color string) *User {
	if u == nil {
		return nil
	}
	return &User{ID: u.ID, Login: u.Login, Name: u.Name, Color: color}
}

func messageOf(m *channelevents.Message) *Message {
	if m == nil {
		return nil
	}
	out := &Message{Text: m.Text}
	for _, fragment := range m.Fragments {
		out.Parts = append(out.Parts, Part{Type: fragment.Type, ID: fragment.ID, Text: fragment.Text})
	}
	return out
}
