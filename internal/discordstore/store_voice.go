// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordstore

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	ddiscord "ItsBagelBot/internal/domain/discord"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"go.uber.org/zap"
)

var ErrCloneCapReached = errors.New("discordstore: the guild is at its temporary voice room cap")

const voiceTTL = 24 * time.Hour

var voiceTTLArg = strconv.Itoa(int(voiceTTL.Seconds()))

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

type VoiceMove struct {
	From      string
	To        string
	LeftEmpty bool
}

func cloneKey(ch Channel) string { return "discord:voice:" + ch.ID }

func cloneSet(g Guild) string { return "discord:voices:" + g.ID }

func occupantsKey(ch Channel) string { return "discord:voiceoccupants:" + ch.ID }

func seatKey(m Member) string { return "discord:voiceseat:" + m.key() }

func (s valkeyStore) TrackClone(ctx context.Context, c Clone) error {
	keys := []string{cloneKey(Channel{ID: c.ChannelID}), cloneSet(Guild{ID: c.GuildID})}
	args := []string{c.ChannelID, c.GuildID + "|" + c.OwnerID, voiceTTLArg, strconv.Itoa(ddiscord.VoiceCloneCap)}
	tracked, err := pkg_valkey.RunCapacitySetAdd(ctx, s.client, keys, args).AsInt64()
	if err != nil {
		return err
	}
	if tracked != 1 {
		return ErrCloneCapReached
	}
	return nil
}

func (s valkeyStore) Clone(ctx context.Context, ch Channel) (Clone, bool) {
	raw, err := pkg_valkey.Exec1(ctx, s.client, pkg_valkey.GetCmd(s.client, cloneKey(ch))).ToString()
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
	n, err := pkg_valkey.Exec1(ctx, s.client, pkg_valkey.ScardCmd(s.client, cloneSet(g))).AsInt64()
	if err != nil {
		return 0
	}
	return int(n)
}

func (s valkeyStore) ForgetClone(ctx context.Context, c Clone) error {
	ch := Channel{ID: c.ChannelID}
	g := Guild{ID: c.GuildID}
	_ = pkg_valkey.Exec1(ctx, s.client, pkg_valkey.DelCmd(s.client, cloneKey(ch))).Error()
	return pkg_valkey.Exec1(ctx, s.client, pkg_valkey.SremCmd(s.client, cloneSet(g), c.ChannelID)).Error()
}

func (s valkeyStore) UpdateVoiceOccupancy(ctx context.Context, seat VoiceSeat) VoiceMove {
	m := Member{GuildID: seat.GuildID, UserID: seat.UserID}
	args := []string{seat.UserID, seat.ChannelID, occupantsKey(Channel{}), cloneKey(Channel{}), voiceTTLArg}
	got, err := pkg_valkey.RunSeatTransfer(ctx, s.client, []string{seatKey(m)}, args).AsStrSlice()
	if err != nil || len(got) != 2 {
		zap.L().Warn("discord voice occupancy update failed; ignoring the event",
			zap.String("guild_id", seat.GuildID), zap.String("user_id", seat.UserID), zap.Error(err))
		return VoiceMove{}
	}
	return VoiceMove{From: got[0], To: seat.ChannelID, LeftEmpty: got[1] == "1"}
}

func (m *Mem) TrackClone(_ context.Context, c Clone) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, tracked := m.clones[c.ChannelID]; !tracked {
		if m.cloneCount[c.GuildID] >= ddiscord.VoiceCloneCap {
			return ErrCloneCapReached
		}
		m.cloneCount[c.GuildID]++
	}
	m.clones[c.ChannelID] = c
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
	if _, tracked := m.clones[c.ChannelID]; tracked {
		delete(m.clones, c.ChannelID)
		m.cloneCount[c.GuildID]--
	}
	return nil
}

func (m *Mem) UpdateVoiceOccupancy(_ context.Context, seat VoiceSeat) VoiceMove {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := Member{GuildID: seat.GuildID, UserID: seat.UserID}.key()
	move := VoiceMove{From: m.seats[key], To: seat.ChannelID}
	if move.From != "" {
		emptied := m.leaveVoiceLocked(move.From, seat.UserID)
		move.LeftEmpty = emptied && move.From != seat.ChannelID
	}
	if seat.ChannelID == "" {
		delete(m.seats, key)
		return move
	}
	if m.occupants[seat.ChannelID] == nil {
		m.occupants[seat.ChannelID] = map[string]struct{}{}
	}
	m.occupants[seat.ChannelID][seat.UserID] = struct{}{}
	m.seats[key] = seat.ChannelID
	return move
}

func (m *Mem) leaveVoiceLocked(channelID, userID string) bool {
	delete(m.occupants[channelID], userID)
	if len(m.occupants[channelID]) != 0 {
		return false
	}
	delete(m.occupants, channelID)
	return true
}
