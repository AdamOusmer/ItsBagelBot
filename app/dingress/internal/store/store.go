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

// Store is the Valkey-backed (or in-memory) reverse index dingress needs.
type Store interface {
	Broadcaster(ctx context.Context, g Guild) (Broadcaster, bool)
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

func guildKey(g Guild) string { return "discord:guild:" + g.ID }

func cloneKey(ch Channel) string { return "discord:voice:" + ch.ID }

func cloneSet(g Guild) string { return "discord:voices:" + g.ID }

func ticketKey(ch Channel) string { return "discord:ticket:" + ch.ID }

func deskKey(g Guild) string { return "discord:ticketdesk:" + g.ID }

func xpKey(m Member) string { return "discord:xp:" + m.key() }

func xpCDKey(m Member) string { return "discord:xpcd:" + m.key() }

func dailyKey(m Member) string { return "discord:daily:" + m.key() }

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
	}
}

// PutGuild is the test helper that stands in for outgress's reverse index.
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
