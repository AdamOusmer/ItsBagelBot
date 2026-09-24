// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordstore

import (
	"context"
	"strings"
)

type Clone struct {
	ChannelID string
	GuildID   string
	OwnerID   string
}

type VoiceSeat struct {
	GuildID   string
	UserID    string
	ChannelID string
}

func cloneKey(ch Channel) string { return "discord:voice:" + ch.ID }

func cloneSet(g Guild) string { return "discord:voices:" + g.ID }

func occupantsKey(ch Channel) string { return "discord:voiceoccupants:" + ch.ID }

func seatKey(m Member) string { return "discord:voiceseat:" + m.key() }

func (s valkeyStore) TrackClone(ctx context.Context, c Clone) error {
	ch := Channel{ID: c.ChannelID}
	g := Guild{ID: c.GuildID}
	if err := s.client.Do(ctx, s.client.B().Set().Key(cloneKey(ch)).Value(c.GuildID+"|"+c.OwnerID).Build()).Error(); err != nil {
		return err
	}
	return s.client.Do(ctx, s.client.B().Sadd().Key(cloneSet(g)).Member(c.ChannelID).Build()).Error()
}

func (s valkeyStore) Clone(ctx context.Context, ch Channel) (Clone, bool) {
	raw, err := s.client.Do(ctx, s.client.B().Get().Key(cloneKey(ch)).Build()).ToString()
	if err != nil {
		return Clone{}, false
	}
	if raw == "" {
		return Clone{}, false
	}
	guildID, ownerID, ok := strings.Cut(raw, "|")
	if !ok {
		return Clone{}, false
	}
	return Clone{ChannelID: ch.ID, GuildID: guildID, OwnerID: ownerID}, true
}

func (s valkeyStore) CloneCount(ctx context.Context, g Guild) int {
	n, err := s.client.Do(ctx, s.client.B().Scard().Key(cloneSet(g)).Build()).AsInt64()
	if err != nil {
		return 0
	}
	return int(n)
}

func (s valkeyStore) ForgetClone(ctx context.Context, c Clone) error {
	ch := Channel{ID: c.ChannelID}
	g := Guild{ID: c.GuildID}
	_ = s.client.Do(ctx, s.client.B().Del().Key(cloneKey(ch)).Build()).Error()
	return s.client.Do(ctx, s.client.B().Srem().Key(cloneSet(g)).Member(c.ChannelID).Build()).Error()
}

func (s valkeyStore) UpdateVoiceOccupancy(ctx context.Context, seat VoiceSeat) (string, bool) {
	m := Member{GuildID: seat.GuildID, UserID: seat.UserID}
	prev, _ := s.client.Do(ctx, s.client.B().Get().Key(seatKey(m)).Build()).ToString()

	var left string
	var leftEmpty bool
	if prev != "" {
		left = prev
		leftEmpty = s.leaveVoice(ctx, Channel{ID: prev}, seat.UserID)
	}

	if seat.ChannelID == "" {
		_ = s.client.Do(ctx, s.client.B().Del().Key(seatKey(m)).Build()).Error()
		return left, leftEmpty
	}

	_ = s.client.Do(ctx, s.client.B().Sadd().Key(occupantsKey(Channel{ID: seat.ChannelID})).Member(seat.UserID).Build()).Error()
	_ = s.client.Do(ctx, s.client.B().Set().Key(seatKey(m)).Value(seat.ChannelID).Build()).Error()
	return left, leftEmpty && prev != seat.ChannelID
}

func (s valkeyStore) leaveVoice(ctx context.Context, ch Channel, userID string) bool {
	_ = s.client.Do(ctx, s.client.B().Srem().Key(occupantsKey(ch)).Member(userID).Build()).Error()
	n, err := s.client.Do(ctx, s.client.B().Scard().Key(occupantsKey(ch)).Build()).AsInt64()
	if err != nil {
		return false
	}
	return n == 0
}

func (m *Mem) TrackClone(_ context.Context, c Clone) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clones[c.ChannelID] = c
	m.cloneCount[c.GuildID]++
	return nil
}

func (m *Mem) Clone(_ context.Context, ch Channel) (Clone, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.clones[ch.ID]
	return c, ok
}

func (m *Mem) CloneCount(_ context.Context, g Guild) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cloneCount[g.ID]
}

func (m *Mem) ForgetClone(_ context.Context, c Clone) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.clones, c.ChannelID)
	if m.cloneCount[c.GuildID] > 0 {
		m.cloneCount[c.GuildID]--
	}
	return nil
}

func (m *Mem) UpdateVoiceOccupancy(_ context.Context, seat VoiceSeat) (left string, leftEmpty bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	mem := Member{GuildID: seat.GuildID, UserID: seat.UserID}
	key := mem.key()
	prev := m.seats[key]
	if prev != "" {
		left = prev
		leftEmpty = m.leaveVoiceLocked(prev, seat.UserID)
	}
	if seat.ChannelID == "" {
		delete(m.seats, key)
		return left, leftEmpty
	}
	if m.occupants[seat.ChannelID] == nil {
		m.occupants[seat.ChannelID] = map[string]struct{}{}
	}
	m.occupants[seat.ChannelID][seat.UserID] = struct{}{}
	m.seats[key] = seat.ChannelID
	return left, leftEmpty && prev != seat.ChannelID
}

func (m *Mem) leaveVoiceLocked(channelID, userID string) bool {
	delete(m.occupants[channelID], userID)
	if len(m.occupants[channelID]) != 0 {
		return false
	}
	delete(m.occupants, channelID)
	return true
}
