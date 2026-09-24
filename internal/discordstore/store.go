// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordstore

import (
	"context"
	"errors"
	"sort"
	"sync"

	ddiscord "ItsBagelBot/internal/domain/discord"

	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

var ErrConfigUnavailable = errors.New("discordstore: guild settings need the discord-data-backed store")

var ErrConfigConflict = errors.New("discordstore: guild settings changed since they were read")

var ErrStoreUnavailable = errors.New("discordstore: discord-data is unreachable")

var ErrNotBound = errors.New("discordstore: guild is not bound to this broadcaster")

type Guild struct{ ID string }

type Channel struct{ ID string }

type Member struct {
	GuildID string
	UserID  string
}

func (m Member) key() string { return m.GuildID + ":" + m.UserID }

type Broadcaster struct{ ID string }

type Binding struct {
	Guild         Guild
	Broadcaster   Broadcaster
	InstalledBy   string
	BoundAtUnixMs int64
}

type BindingSource int

const (
	BindingFromStore BindingSource = iota
	BindingFromCache
)

type SetConfig struct {
	Guild           Guild
	Broadcaster     Broadcaster
	Config          ddiscord.Config
	ExpectedVersion int
}

type GuildConfigOf struct {
	Guild  Guild
	Config ddiscord.Config
}

type Store interface {
	Broadcaster(ctx context.Context, g Guild) (Broadcaster, bool)
	// Ownership checks must use BindingOf and refuse BindingFromCache.
	BindingOf(ctx context.Context, g Guild) (Broadcaster, BindingSource, bool)
	BindGuild(ctx context.Context, bind Binding) error
	UnbindGuild(ctx context.Context, bind Binding) error

	GuildsOf(ctx context.Context, b Broadcaster) ([]Binding, error)
	GuildConfig(ctx context.Context, g Guild) (ddiscord.Config, int, bool)
	SetGuildConfig(ctx context.Context, set SetConfig) (int, error)
	Invalidate(ctx context.Context, g Guild)

	TrackClone(ctx context.Context, c Clone) error
	Clone(ctx context.Context, ch Channel) (Clone, bool)
	CloneCount(ctx context.Context, g Guild) int
	ForgetClone(ctx context.Context, c Clone) error

	TrackTicket(ctx context.Context, t TicketOpen) (TicketOpenResult, error)
	Ticket(ctx context.Context, g Guild, ch Channel) (Ticket, bool)
	OpenTicketCount(ctx context.Context, m Member) int
	ClaimTicket(ctx context.Context, c TicketClaim) error
	CloseTicket(ctx context.Context, c TicketClose) error
	PutTranscript(ctx context.Context, t Transcript) error

	TicketsDurable(ctx context.Context) bool

	MarkPendingClose(ctx context.Context, c TicketClose) error
	PendingClose(ctx context.Context, ch Channel) (TicketClose, bool)
	ClearPendingClose(ctx context.Context, ch Channel) error

	ClaimSummary(ctx context.Context, ticketID int) bool

	ClaimDesk(ctx context.Context, g Guild) bool
	RememberDesk(ctx context.Context, p DeskPanel) error
	Desk(ctx context.Context, g Guild) (DeskPanel, bool)

	AddXP(ctx context.Context, m Member) (xp int, leveled bool, level int)
	ClaimDaily(ctx context.Context, m Member) (ok bool, xp int)
	Rank(ctx context.Context, m Member) (xp, level int)

	UpdateVoiceOccupancy(ctx context.Context, seat VoiceSeat) (left string, leftEmpty bool)
}

type valkeyStore struct {
	client valkey.Client
}

func New(client valkey.Client) Store { return newLocal(client) }

func guildKey(g Guild) string { return "discord:guild:" + g.ID }

func guildsKey(b Broadcaster) string { return "discord:guilds:" + b.ID }

func cfgKey(g Guild) string { return "discord:cfg:" + g.ID }

func (s valkeyStore) BindingOf(ctx context.Context, g Guild) (Broadcaster, BindingSource, bool) {
	b, ok := s.Broadcaster(ctx, g)
	return b, BindingFromStore, ok
}

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

func (s valkeyStore) BindGuild(ctx context.Context, bind Binding) error {
	if bind.Guild.ID == "" || bind.Broadcaster.ID == "" {
		return nil
	}
	s.dropGuilds(ctx, bind.Broadcaster)
	return s.client.Do(ctx, s.client.B().Set().Key(guildKey(bind.Guild)).Value(bind.Broadcaster.ID).Build()).Error()
}

func (s valkeyStore) UnbindGuild(ctx context.Context, bind Binding) error {
	s.dropGuilds(ctx, bind.Broadcaster)
	return s.client.Do(ctx, s.client.B().Del().Key(guildKey(bind.Guild)).Build()).Error()
}

func (s valkeyStore) GuildsOf(_ context.Context, b Broadcaster) ([]Binding, error) {
	zap.L().Warn("discord guild list needs the discord-data-backed store; refusing",
		zap.String("broadcaster_id", b.ID))
	return nil, ErrStoreUnavailable
}

func (s valkeyStore) GuildConfig(_ context.Context, g Guild) (ddiscord.Config, int, bool) {
	zap.L().Warn("discord guild settings need the discord-data-backed store; answering not-found",
		zap.String("guild_id", g.ID))
	return ddiscord.Config{}, 0, false
}

func (s valkeyStore) SetGuildConfig(_ context.Context, set SetConfig) (int, error) {
	zap.L().Warn("discord guild settings need the discord-data-backed store; refusing the write",
		zap.String("guild_id", set.Guild.ID))
	return 0, ErrConfigUnavailable
}

func (s valkeyStore) Invalidate(ctx context.Context, g Guild) {
	s.dropConfig(ctx, g)
}

type Mem struct {
	mu          sync.Mutex
	guild       map[string]string
	clones      map[string]Clone
	cloneCount  map[string]int
	tickets     map[string]Ticket
	pending     map[string]TicketClose
	summaries   map[int]bool
	transcripts map[int]Transcript
	ticketSeq   int
	desk        map[string]DeskPanel
	xp          map[string]int
	xpCD        map[string]bool
	daily       map[string]bool
	occupants   map[string]map[string]struct{}
	seats       map[string]string
	configs     map[string]memConfig
	guildsCache map[string][]Binding
}

type memConfig struct {
	Config  ddiscord.Config
	Version int
}

func NewMem() *Mem {
	return &Mem{
		guild:       map[string]string{},
		clones:      map[string]Clone{},
		cloneCount:  map[string]int{},
		tickets:     map[string]Ticket{},
		pending:     map[string]TicketClose{},
		summaries:   map[int]bool{},
		transcripts: map[int]Transcript{},
		desk:        map[string]DeskPanel{},
		xp:          map[string]int{},
		xpCD:        map[string]bool{},
		daily:       map[string]bool{},
		occupants:   map[string]map[string]struct{}{},
		seats:       map[string]string{},
		configs:     map[string]memConfig{},
		guildsCache: map[string][]Binding{},
	}
}

func (m *Mem) PutGuild(g Guild, b Broadcaster) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.guild[g.ID] = b.ID
}

func (m *Mem) BindingOf(ctx context.Context, g Guild) (Broadcaster, BindingSource, bool) {
	b, ok := m.Broadcaster(ctx, g)
	return b, BindingFromStore, ok
}

func (m *Mem) Broadcaster(_ context.Context, g Guild) (Broadcaster, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.guild[g.ID]
	return Broadcaster{ID: v}, ok
}

func (m *Mem) BindGuild(_ context.Context, bind Binding) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if bind.Guild.ID == "" || bind.Broadcaster.ID == "" {
		return nil
	}
	m.guild[bind.Guild.ID] = bind.Broadcaster.ID
	return nil
}

func (m *Mem) UnbindGuild(_ context.Context, bind Binding) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.guild, bind.Guild.ID)
	return nil
}

func (m *Mem) GuildsOf(_ context.Context, b Broadcaster) ([]Binding, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := make([]string, 0, len(m.guild))
	for guildID, broadcasterID := range m.guild {
		if broadcasterID == b.ID {
			ids = append(ids, guildID)
		}
	}
	sort.Strings(ids)
	out := make([]Binding, 0, len(ids))
	for _, id := range ids {
		out = append(out, Binding{Guild: Guild{ID: id}, Broadcaster: b})
	}
	return out, nil
}

func (m *Mem) GuildConfig(_ context.Context, g Guild) (ddiscord.Config, int, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	got, ok := m.configs[g.ID]
	if !ok {
		return ddiscord.Config{}, 0, false
	}
	return got.Config, got.Version, true
}

func (m *Mem) SetGuildConfig(_ context.Context, set SetConfig) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.guild[set.Guild.ID] != set.Broadcaster.ID || set.Broadcaster.ID == "" {
		return 0, ErrNotBound
	}
	if m.configs[set.Guild.ID].Version != set.ExpectedVersion {
		return 0, ErrConfigConflict
	}
	version := set.ExpectedVersion + 1
	m.configs[set.Guild.ID] = memConfig{Config: set.Config, Version: version}
	return version, nil
}

func (m *Mem) Invalidate(_ context.Context, _ Guild) {}

func (m *Mem) PutGuildConfig(g Guild, cfg ddiscord.Config) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.configs[g.ID] = memConfig{Config: cfg, Version: m.configs[g.ID].Version + 1}
}
