// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package discordstore is the Valkey-backed (or in-memory) state Discord
// community features need across process boundaries. It used to live
// app-local at app/dingress/internal/store, safe there because ROLE=gateway
// was the only reader and writer of every key in it. The three-way split
// (app/discord/ingress|engine|outgress) makes two of its keyspaces
// genuinely cross-process:
//
//   - The guild->broadcaster reverse index (discord:guild:*) is written by
//     outgress (the guild setup/unbind RPC, still dashboard-facing) and read
//     by engine (every gateway event needs it to resolve which broadcaster's
//     module config applies). It was ALSO duplicated, before this split, as
//     app/dingress/internal/egress's own liveStore.PutGuild/GetGuild/
//     DeleteGuild against the identical "discord:guild:"+id key -- two
//     interfaces, one keyspace, because ROLE=gateway and ROLE=egress were
//     one process and never noticed. Promoting this package is what removes
//     that duplication: outgress's guild-setup code now binds through this
//     package too (see app/discord/outgress's kv.go adapter) instead of
//     re-implementing the same key.
//   - Ticket/clone/XP/desk/voice-occupancy state is written by engine, which
//     owns every decision that touches it (open a ticket, spin up a voice
//     clone, award crumbs). Nothing outside engine reads or writes these
//     keys; they live here anyway because engine's own package already
//     imports this one for the guild index, and one Valkey-backed package is
//     simpler than two.
package discordstore

import (
	"context"
	"math"
	"strconv"
	"strings"
	"sync"

	"github.com/valkey-io/valkey-go"
)

const (
	xpPerMessage = 15
	xpCooldown   = 60
	dailyXP      = 50
	dailyTTL     = 24 * 60 * 60
)

// Guild is a Discord guild snowflake.
type Guild struct{ ID string }

// Channel is a Discord channel snowflake.
type Channel struct{ ID string }

// Member is one Discord user in one guild. Crumbs and dailies are keyed on this pair.
type Member struct {
	GuildID string
	UserID  string
}

func (m Member) key() string { return m.GuildID + ":" + m.UserID }

// Broadcaster is the Twitch user id the guild reverse-index points at.
type Broadcaster struct{ ID string }

// Clone is one join-to-create voice channel.
type Clone struct {
	ChannelID string
	GuildID   string
	OwnerID   string
}

// Ticket is one private support channel.
type Ticket struct {
	ChannelID string
	GuildID   string
	OpenerID  string
}

// VoiceSeat is one user's voice-channel membership at a point in time; the
// zero ChannelID means "not in a voice channel".
type VoiceSeat struct {
	GuildID   string
	UserID    string
	ChannelID string
}

// Store is the Valkey-backed (or in-memory) state engine and outgress share.
type Store interface {
	// Broadcaster resolves a Discord guild to the Twitch broadcaster it is
	// bound to. Written by BindGuild (outgress, on guild setup).
	Broadcaster(ctx context.Context, g Guild) (Broadcaster, bool)
	BindGuild(ctx context.Context, g Guild, b Broadcaster) error
	UnbindGuild(ctx context.Context, g Guild) error

	TrackClone(ctx context.Context, c Clone) error
	Clone(ctx context.Context, ch Channel) (Clone, bool)
	CloneCount(ctx context.Context, g Guild) int
	ForgetClone(ctx context.Context, c Clone) error

	TrackTicket(ctx context.Context, t Ticket) error
	Ticket(ctx context.Context, ch Channel) (Ticket, bool)
	ForgetTicket(ctx context.Context, ch Channel) error

	ClaimDesk(ctx context.Context, g Guild) bool
	RememberDesk(ctx context.Context, g Guild) error

	AddXP(ctx context.Context, m Member) (xp int, leveled bool, level int)
	ClaimDaily(ctx context.Context, m Member) (ok bool, xp int)
	Rank(ctx context.Context, m Member) (xp, level int)

	// UpdateVoiceOccupancy records seat and reports the channel the user
	// left (if any) and whether that channel is now empty. See the Valkey
	// implementation's doc for why this is not linearizable across
	// concurrent engine replicas, and why that is an acceptable trade here.
	UpdateVoiceOccupancy(ctx context.Context, seat VoiceSeat) (left string, leftEmpty bool)
}

type valkeyStore struct {
	client valkey.Client
}

// New builds the production store.
func New(client valkey.Client) Store {
	if client == nil {
		return NewMem()
	}
	return valkeyStore{client: client}
}

func guildKey(g Guild) string { return "discord:guild:" + g.ID }

func cloneKey(ch Channel) string { return "discord:voice:" + ch.ID }

func cloneSet(g Guild) string { return "discord:voices:" + g.ID }

func ticketKey(ch Channel) string { return "discord:ticket:" + ch.ID }

func deskKey(g Guild) string { return "discord:ticketdesk:" + g.ID }

func xpKey(m Member) string { return "discord:xp:" + m.key() }

func xpCDKey(m Member) string { return "discord:xpcd:" + m.key() }

func dailyKey(m Member) string { return "discord:daily:" + m.key() }

// occupantsKey is the set of user ids currently seated in one channel.
func occupantsKey(ch Channel) string { return "discord:voiceoccupants:" + ch.ID }

// seatKey is the channel one user is currently seated in, keyed by
// guild+user so a user in no guild's voice channel simply has no key.
func seatKey(m Member) string { return "discord:voiceseat:" + m.key() }

func (s valkeyStore) Broadcaster(ctx context.Context, g Guild) (Broadcaster, bool) {
	raw, err := s.client.Do(ctx, s.client.B().Get().Key(guildKey(g)).Build()).ToString()
	if err != nil {
		return Broadcaster{}, false
	}
	if raw == "" {
		return Broadcaster{}, false
	}
	return Broadcaster{ID: raw}, true
}

func (s valkeyStore) BindGuild(ctx context.Context, g Guild, b Broadcaster) error {
	if g.ID == "" {
		return nil
	}
	if b.ID == "" {
		return nil
	}
	return s.client.Do(ctx, s.client.B().Set().Key(guildKey(g)).Value(b.ID).Build()).Error()
}

func (s valkeyStore) UnbindGuild(ctx context.Context, g Guild) error {
	return s.client.Do(ctx, s.client.B().Del().Key(guildKey(g)).Build()).Error()
}

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

func (s valkeyStore) TrackTicket(ctx context.Context, t Ticket) error {
	ch := Channel{ID: t.ChannelID}
	return s.client.Do(ctx, s.client.B().Set().Key(ticketKey(ch)).Value(t.GuildID+"|"+t.OpenerID).Build()).Error()
}

func (s valkeyStore) Ticket(ctx context.Context, ch Channel) (Ticket, bool) {
	raw, err := s.client.Do(ctx, s.client.B().Get().Key(ticketKey(ch)).Build()).ToString()
	if err != nil {
		return Ticket{}, false
	}
	if raw == "" {
		return Ticket{}, false
	}
	guildID, openerID, ok := strings.Cut(raw, "|")
	if !ok {
		return Ticket{}, false
	}
	return Ticket{ChannelID: ch.ID, GuildID: guildID, OpenerID: openerID}, true
}

func (s valkeyStore) ForgetTicket(ctx context.Context, ch Channel) error {
	return s.client.Do(ctx, s.client.B().Del().Key(ticketKey(ch)).Build()).Error()
}

func (s valkeyStore) ClaimDesk(ctx context.Context, g Guild) bool {
	err := s.client.Do(ctx, s.client.B().Set().Key(deskKey(g)).Value("1").Nx().Build()).Error()
	return err == nil
}

func (s valkeyStore) RememberDesk(ctx context.Context, g Guild) error {
	return s.client.Do(ctx, s.client.B().Set().Key(deskKey(g)).Value("1").Build()).Error()
}

func (s valkeyStore) AddXP(ctx context.Context, m Member) (int, bool, int) {
	err := s.client.Do(ctx, s.client.B().Set().Key(xpCDKey(m)).Value("1").Nx().ExSeconds(xpCooldown).Build()).Error()
	if err != nil {
		xp, level := s.Rank(ctx, m)
		return xp, false, level
	}
	before, _ := s.Rank(ctx, m)
	n, err := s.client.Do(ctx, s.client.B().Incrby().Key(xpKey(m)).Increment(xpPerMessage).Build()).AsInt64()
	if err != nil {
		return before, false, levelOf(before)
	}
	xp := int(n)
	return xp, levelOf(xp) > levelOf(before), levelOf(xp)
}

func (s valkeyStore) ClaimDaily(ctx context.Context, m Member) (bool, int) {
	err := s.client.Do(ctx, s.client.B().Set().Key(dailyKey(m)).Value("1").Nx().ExSeconds(dailyTTL).Build()).Error()
	if valkey.IsValkeyNil(err) {
		xp, _ := s.Rank(ctx, m)
		return false, xp
	}
	if err != nil {
		return false, 0
	}
	n, err := s.client.Do(ctx, s.client.B().Incrby().Key(xpKey(m)).Increment(dailyXP).Build()).AsInt64()
	if err != nil {
		return true, dailyXP
	}
	return true, int(n)
}

func (s valkeyStore) Rank(ctx context.Context, m Member) (int, int) {
	raw, err := s.client.Do(ctx, s.client.B().Get().Key(xpKey(m)).Build()).ToString()
	if err != nil {
		return 0, 0
	}
	if raw == "" {
		return 0, 0
	}
	xp, _ := strconv.Atoi(raw)
	return xp, levelOf(xp)
}

// UpdateVoiceOccupancy ports app/dingress/internal/community's in-memory
// occupancy.update onto Valkey, key for key: two round trips (leave the old
// seat, take the new one) rather than one atomic script. That is a
// deliberate simplification, not an oversight -- the two operations it
// races against itself are "the same user's own next VOICE_STATE_UPDATE"
// (Discord delivers one user's voice events to one guild in order, so there
// is nothing to race) and "another replica handling a DIFFERENT user's
// event for the same channel" (which only contends on the occupants set's
// membership count, and a stale read there costs at worst one clone deleted
// a beat late or kept a beat past truly empty -- never a wrong owner, never
// a double-delete). A Lua script would make that count linearizable for no
// behavior the cleanup this feeds actually needs.
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

// leaveVoice removes userID from ch's occupant set and reports whether that
// leaves it empty.
func (s valkeyStore) leaveVoice(ctx context.Context, ch Channel, userID string) bool {
	_ = s.client.Do(ctx, s.client.B().Srem().Key(occupantsKey(ch)).Member(userID).Build()).Error()
	n, err := s.client.Do(ctx, s.client.B().Scard().Key(occupantsKey(ch)).Build()).AsInt64()
	if err != nil {
		return false
	}
	return n == 0
}

func levelOf(xp int) int {
	if xp <= 0 {
		return 0
	}
	return int(math.Sqrt(float64(xp) / 100))
}

// Mem is an in-process Store for tests.
type Mem struct {
	mu         sync.Mutex
	guild      map[string]string
	clones     map[string]Clone
	cloneCount map[string]int
	tickets    map[string]Ticket
	desk       map[string]bool
	xp         map[string]int
	xpCD       map[string]bool
	daily      map[string]bool
	occupants  map[string]map[string]struct{}
	seats      map[string]string
}

// NewMem builds an empty memory store.
func NewMem() *Mem {
	return &Mem{
		guild:      map[string]string{},
		clones:     map[string]Clone{},
		cloneCount: map[string]int{},
		tickets:    map[string]Ticket{},
		desk:       map[string]bool{},
		xp:         map[string]int{},
		xpCD:       map[string]bool{},
		daily:      map[string]bool{},
		occupants:  map[string]map[string]struct{}{},
		seats:      map[string]string{},
	}
}

// PutGuild is the test helper that stands in for BindGuild.
func (m *Mem) PutGuild(g Guild, b Broadcaster) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.guild[g.ID] = b.ID
}

func (m *Mem) Broadcaster(_ context.Context, g Guild) (Broadcaster, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.guild[g.ID]
	return Broadcaster{ID: v}, ok
}

func (m *Mem) BindGuild(_ context.Context, g Guild, b Broadcaster) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if g.ID == "" || b.ID == "" {
		return nil
	}
	m.guild[g.ID] = b.ID
	return nil
}

func (m *Mem) UnbindGuild(_ context.Context, g Guild) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.guild, g.ID)
	return nil
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

func (m *Mem) TrackTicket(_ context.Context, t Ticket) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tickets[t.ChannelID] = t
	return nil
}

func (m *Mem) Ticket(_ context.Context, ch Channel) (Ticket, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tickets[ch.ID]
	return t, ok
}

func (m *Mem) ForgetTicket(_ context.Context, ch Channel) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.tickets, ch.ID)
	return nil
}

func (m *Mem) ClaimDesk(_ context.Context, g Guild) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.desk[g.ID] {
		return false
	}
	m.desk[g.ID] = true
	return true
}

func (m *Mem) RememberDesk(_ context.Context, g Guild) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.desk[g.ID] = true
	return nil
}

func (m *Mem) AddXP(_ context.Context, mem Member) (int, bool, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := mem.key()
	if m.xpCD[k] {
		xp := m.xp[k]
		return xp, false, levelOf(xp)
	}
	m.xpCD[k] = true
	before := m.xp[k]
	m.xp[k] = before + xpPerMessage
	return m.xp[k], levelOf(m.xp[k]) > levelOf(before), levelOf(m.xp[k])
}

func (m *Mem) ClaimDaily(_ context.Context, mem Member) (bool, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := mem.key()
	if m.daily[k] {
		return false, m.xp[k]
	}
	m.daily[k] = true
	m.xp[k] += dailyXP
	return true, m.xp[k]
}

func (m *Mem) Rank(_ context.Context, mem Member) (int, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	xp := m.xp[mem.key()]
	return xp, levelOf(xp)
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

// leaveVoiceLocked assumes m.mu is already held.
func (m *Mem) leaveVoiceLocked(channelID, userID string) bool {
	delete(m.occupants[channelID], userID)
	if len(m.occupants[channelID]) != 0 {
		return false
	}
	delete(m.occupants, channelID)
	return true
}

type XPSeed struct {
	Member Member
	Amount int
}

// SeedXP is a test helper that sets crumbs without touching the cooldown.
func (m *Mem) SeedXP(s XPSeed) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.xp[s.Member.key()] = s.Amount
}
