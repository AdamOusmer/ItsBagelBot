// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordstore

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

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
	ch := Channel{ID: c.ChannelID}
	g := Guild{ID: c.GuildID}
	b := s.client.B()
	for _, r := range s.client.DoMulti(ctx,
		b.Set().Key(cloneKey(ch)).Value(c.GuildID+"|"+c.OwnerID).ExSeconds(int64(voiceTTL.Seconds())).Build(),
		b.Sadd().Key(cloneSet(g)).Member(c.ChannelID).Build(),
		b.Expire().Key(cloneSet(g)).Seconds(int64(voiceTTL.Seconds())).Build(),
	) {
		if err := r.Error(); err != nil {
			return err
		}
	}
	return nil
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

var voiceMoveScript = valkey.NewLuaScript(`
local user, to, occupants, clones, ttl = ARGV[1], ARGV[2], ARGV[3], ARGV[4], ARGV[5]
local from = redis.call('GET', KEYS[1]) or ''
local empty = 0
if from ~= '' then
    redis.call('SREM', occupants .. from, user)
    if from ~= to and redis.call('SCARD', occupants .. from) == 0 then
        empty = 1
    end
end
if to == '' then
    redis.call('DEL', KEYS[1])
else
    redis.call('SADD', occupants .. to, user)
    redis.call('EXPIRE', occupants .. to, ttl)
    redis.call('EXPIRE', clones .. to, ttl)
    redis.call('SET', KEYS[1], to, 'EX', ttl)
end
return {from, tostring(empty)}
`)

func (s valkeyStore) UpdateVoiceOccupancy(ctx context.Context, seat VoiceSeat) VoiceMove {
	m := Member{GuildID: seat.GuildID, UserID: seat.UserID}
	args := []string{seat.UserID, seat.ChannelID, occupantsKey(Channel{}), cloneKey(Channel{}), voiceTTLArg}
	got, err := voiceMoveScript.Exec(ctx, s.client, []string{seatKey(m)}, args).AsStrSlice()
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
