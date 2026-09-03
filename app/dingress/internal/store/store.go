// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package store

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

// Store is the Valkey-backed (or in-memory) reverse index dingress needs.
type Store interface {
	Broadcaster(ctx context.Context, guildID string) (string, bool)
	TrackClone(ctx context.Context, c Clone) error
	Clone(ctx context.Context, channelID string) (Clone, bool)
	CloneCount(ctx context.Context, guildID string) int
	ForgetClone(ctx context.Context, c Clone) error
	TrackTicket(ctx context.Context, t Ticket) error
	Ticket(ctx context.Context, channelID string) (Ticket, bool)
	ForgetTicket(ctx context.Context, channelID string) error
	AddXP(ctx context.Context, guildID, userID string) (xp int, leveled bool, level int)
	ClaimDaily(ctx context.Context, guildID, userID string) (ok bool, xp int)
	Rank(ctx context.Context, guildID, userID string) (xp, level int)
}

type valkeyStore struct {
	client valkey.Client
}

// New builds the production store. Guild reverse-index keys are the ones
// outgress writes on setup (discord:guild:{id}).
func New(client valkey.Client) Store {
	if client == nil {
		return NewMem()
	}
	return valkeyStore{client: client}
}

func guildKey(id string) string { return "discord:guild:" + id }

func cloneKey(id string) string { return "discord:voice:" + id }

func cloneSet(guildID string) string { return "discord:voices:" + guildID }

func ticketKey(id string) string { return "discord:ticket:" + id }

func xpKey(guildID, userID string) string { return "discord:xp:" + guildID + ":" + userID }

func xpCDKey(guildID, userID string) string { return "discord:xpcd:" + guildID + ":" + userID }

func dailyKey(guildID, userID string) string { return "discord:daily:" + guildID + ":" + userID }

func (s valkeyStore) Broadcaster(ctx context.Context, guildID string) (string, bool) {
	raw, err := s.client.Do(ctx, s.client.B().Get().Key(guildKey(guildID)).Build()).ToString()
	if err != nil || raw == "" {
		return "", false
	}
	return raw, true
}

func (s valkeyStore) TrackClone(ctx context.Context, c Clone) error {
	if err := s.client.Do(ctx, s.client.B().Set().Key(cloneKey(c.ChannelID)).Value(c.GuildID+"|"+c.OwnerID).Build()).Error(); err != nil {
		return err
	}
	return s.client.Do(ctx, s.client.B().Sadd().Key(cloneSet(c.GuildID)).Member(c.ChannelID).Build()).Error()
}

func (s valkeyStore) Clone(ctx context.Context, channelID string) (Clone, bool) {
	raw, err := s.client.Do(ctx, s.client.B().Get().Key(cloneKey(channelID)).Build()).ToString()
	if err != nil || raw == "" {
		return Clone{}, false
	}
	guildID, ownerID, ok := strings.Cut(raw, "|")
	if !ok {
		return Clone{}, false
	}
	return Clone{ChannelID: channelID, GuildID: guildID, OwnerID: ownerID}, true
}

func (s valkeyStore) CloneCount(ctx context.Context, guildID string) int {
	n, err := s.client.Do(ctx, s.client.B().Scard().Key(cloneSet(guildID)).Build()).AsInt64()
	if err != nil {
		return 0
	}
	return int(n)
}

func (s valkeyStore) ForgetClone(ctx context.Context, c Clone) error {
	_ = s.client.Do(ctx, s.client.B().Del().Key(cloneKey(c.ChannelID)).Build()).Error()
	return s.client.Do(ctx, s.client.B().Srem().Key(cloneSet(c.GuildID)).Member(c.ChannelID).Build()).Error()
}

func (s valkeyStore) TrackTicket(ctx context.Context, t Ticket) error {
	return s.client.Do(ctx, s.client.B().Set().Key(ticketKey(t.ChannelID)).Value(t.GuildID+"|"+t.OpenerID).Build()).Error()
}

func (s valkeyStore) Ticket(ctx context.Context, channelID string) (Ticket, bool) {
	raw, err := s.client.Do(ctx, s.client.B().Get().Key(ticketKey(channelID)).Build()).ToString()
	if err != nil || raw == "" {
		return Ticket{}, false
	}
	guildID, openerID, ok := strings.Cut(raw, "|")
	if !ok {
		return Ticket{}, false
	}
	return Ticket{ChannelID: channelID, GuildID: guildID, OpenerID: openerID}, true
}

func (s valkeyStore) ForgetTicket(ctx context.Context, channelID string) error {
	return s.client.Do(ctx, s.client.B().Del().Key(ticketKey(channelID)).Build()).Error()
}

func (s valkeyStore) AddXP(ctx context.Context, guildID, userID string) (int, bool, int) {
	err := s.client.Do(ctx, s.client.B().Set().Key(xpCDKey(guildID, userID)).Value("1").Nx().ExSeconds(xpCooldown).Build()).Error()
	if err != nil {
		// Nil is SET NX declined (cooldown). Any other error fails closed.
		xp, level := s.Rank(ctx, guildID, userID)
		return xp, false, level
	}
	before, _ := s.Rank(ctx, guildID, userID)
	n, err := s.client.Do(ctx, s.client.B().Incrby().Key(xpKey(guildID, userID)).Increment(xpPerMessage).Build()).AsInt64()
	if err != nil {
		return before, false, levelOf(before)
	}
	xp := int(n)
	oldLevel := levelOf(before)
	newLevel := levelOf(xp)
	return xp, newLevel > oldLevel, newLevel
}

func (s valkeyStore) ClaimDaily(ctx context.Context, guildID, userID string) (bool, int) {
	err := s.client.Do(ctx, s.client.B().Set().Key(dailyKey(guildID, userID)).Value("1").Nx().ExSeconds(dailyTTL).Build()).Error()
	if valkey.IsValkeyNil(err) {
		xp, _ := s.Rank(ctx, guildID, userID)
		return false, xp
	}
	if err != nil {
		return false, 0
	}
	n, err := s.client.Do(ctx, s.client.B().Incrby().Key(xpKey(guildID, userID)).Increment(dailyXP).Build()).AsInt64()
	if err != nil {
		return true, dailyXP
	}
	return true, int(n)
}

func (s valkeyStore) Rank(ctx context.Context, guildID, userID string) (int, int) {
	raw, err := s.client.Do(ctx, s.client.B().Get().Key(xpKey(guildID, userID)).Build()).ToString()
	if err != nil || raw == "" {
		return 0, 0
	}
	xp, _ := strconv.Atoi(raw)
	return xp, levelOf(xp)
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
	xp         map[string]int
	xpCD       map[string]bool
	daily      map[string]bool
}

// NewMem builds an empty memory store.
func NewMem() *Mem {
	return &Mem{
		guild:      map[string]string{},
		clones:     map[string]Clone{},
		cloneCount: map[string]int{},
		tickets:    map[string]Ticket{},
		xp:         map[string]int{},
		xpCD:       map[string]bool{},
		daily:      map[string]bool{},
	}
}

// PutGuild is the test helper that stands in for outgress's reverse index.
func (m *Mem) PutGuild(guildID, broadcasterID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.guild[guildID] = broadcasterID
}

func (m *Mem) Broadcaster(_ context.Context, guildID string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.guild[guildID]
	return v, ok
}

func (m *Mem) TrackClone(_ context.Context, c Clone) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clones[c.ChannelID] = c
	m.cloneCount[c.GuildID]++
	return nil
}

func (m *Mem) Clone(_ context.Context, channelID string) (Clone, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.clones[channelID]
	return c, ok
}

func (m *Mem) CloneCount(_ context.Context, guildID string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cloneCount[guildID]
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

func (m *Mem) Ticket(_ context.Context, channelID string) (Ticket, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tickets[channelID]
	return t, ok
}

func (m *Mem) ForgetTicket(_ context.Context, channelID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.tickets, channelID)
	return nil
}

func (m *Mem) AddXP(_ context.Context, guildID, userID string) (int, bool, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := guildID + ":" + userID
	if m.xpCD[k] {
		xp := m.xp[k]
		return xp, false, levelOf(xp)
	}
	m.xpCD[k] = true
	before := m.xp[k]
	m.xp[k] = before + xpPerMessage
	oldL, newL := levelOf(before), levelOf(m.xp[k])
	return m.xp[k], newL > oldL, newL
}

func (m *Mem) ClaimDaily(_ context.Context, guildID, userID string) (bool, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := guildID + ":" + userID
	if m.daily[k] {
		return false, m.xp[k]
	}
	m.daily[k] = true
	m.xp[k] += dailyXP
	return true, m.xp[k]
}

func (m *Mem) Rank(_ context.Context, guildID, userID string) (int, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	xp := m.xp[guildID+":"+userID]
	return xp, levelOf(xp)
}

// SeedXP is a test helper that sets crumbs without touching the cooldown.
func (m *Mem) SeedXP(guildID, userID string, xp int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.xp[guildID+":"+userID] = xp
}
