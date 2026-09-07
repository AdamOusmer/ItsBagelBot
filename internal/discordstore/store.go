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
//
// The package is laid out by KEYSPACE, not by implementation: this file holds
// the guild binding and settings keys, store_voice.go the clone and seat keys,
// store_ticket.go the ticket/desk/transcript keys and store_xp.go the XP ones.
// Each file carries both implementations of its family (valkeyStore and Mem)
// because those two are what must stay in step -- a change to how a key is
// shaped has to land in the memory double in the same edit, or the tests stop
// describing the real store.
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

// ErrConfigUnavailable is what the pure-Valkey store answers on the settings
// write path. Guild settings have no Valkey store of record -- they live in
// discord-data -- so a process holding this store can only refuse, loudly,
// rather than accept a write nothing will ever read back.
var ErrConfigUnavailable = errors.New("discordstore: guild settings need the discord-data-backed store")

// ErrConfigConflict is a settings write whose expected version no longer
// matches: another tab (or another replica) saved first. The caller must
// re-read and re-apply rather than retry.
var ErrConfigConflict = errors.New("discordstore: guild settings changed since they were read")

// ErrStoreUnavailable is a read that could not reach discord-data and had no
// authoritative answer to give. It is distinct from "nothing found" on
// purpose: a caller that turns an unreachable store into an empty list posts
// nothing, disconnects nothing and shows the streamer an empty server picker,
// all of which look like a deliberate answer.
var ErrStoreUnavailable = errors.New("discordstore: discord-data is unreachable")

// ErrNotBound is a settings write into a guild the caller does not own, or
// that nothing owns. The two are one error on purpose: distinguishing them
// would tell an unbound caller that a guild id it guessed is in use.
var ErrNotBound = errors.New("discordstore: guild is not bound to this broadcaster")

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

// Binding is one guild-to-broadcaster link. It is the argument of both write
// verbs and the element GuildsOf returns: bind needs the installer, unbind
// needs the broadcaster for the owner guard, and the listing needs the
// timestamp, so one struct beats three argument lists that drift apart.
type Binding struct {
	Guild       Guild
	Broadcaster Broadcaster
	// InstalledBy is the acting console user's TWITCH user id -- the
	// broadcaster (or staff account) whose dashboard ran the install, which
	// is the id every dashboard-facing RPC already carries as user_id.
	//
	// Not the Discord snowflake, which this side never learns: the install
	// leg exchanges a bot-authorization code, and its token response names
	// the guild, not the human who approved it. Write only: discord-data
	// records it so support can answer "who added this bot", and nothing on
	// this side reads it back.
	InstalledBy string
	// BoundAtUnixMs is filled by GuildsOf and ignored on writes.
	BoundAtUnixMs int64
}

// BindingSource says where a resolved binding came from.
type BindingSource int

const (
	// BindingFromStore: discord-data answered. The only provenance an
	// ownership decision may be made on.
	BindingFromStore BindingSource = iota
	// BindingFromCache: discord-data was unreachable and the cached binding
	// was served instead. Good enough to keep routing gateway events, NOT good
	// enough to decide whether a dashboard caller owns a guild: the cache
	// entry may predate an unbind, and the answer to "is this yours" would
	// then be yes for a server that is no longer theirs.
	BindingFromCache
)

// SetConfig is one guild's settings write. ExpectedVersion is the version the
// caller read (zero when it read nothing); discord-data refuses a mismatch
// rather than letting two dashboard tabs overwrite each other.
type SetConfig struct {
	Guild           Guild
	Broadcaster     Broadcaster
	Config          ddiscord.Config
	ExpectedVersion int
}

// GuildConfigOf pairs a guild with the settings that guild carries. It is what
// a broadcaster-driven producer (go-live, clips) iterates: one Twitch event
// fans out to every guild the broadcaster installed the bot into.
type GuildConfigOf struct {
	Guild  Guild
	Config ddiscord.Config
}

// Store is the Valkey-backed (or in-memory) state engine and outgress share.
type Store interface {
	// Broadcaster resolves a Discord guild to the Twitch broadcaster it is
	// bound to. Written by BindGuild (outgress, on guild setup).
	Broadcaster(ctx context.Context, g Guild) (Broadcaster, bool)
	// BindingOf is Broadcaster with the answer's provenance attached. An
	// ownership check must use this one and refuse BindingFromCache; the
	// event path uses Broadcaster, where a cached answer is the point.
	BindingOf(ctx context.Context, g Guild) (Broadcaster, BindingSource, bool)
	BindGuild(ctx context.Context, bind Binding) error
	UnbindGuild(ctx context.Context, bind Binding) error

	// GuildsOf lists every guild a broadcaster installed the bot into. One
	// broadcaster owns many guilds, so a Twitch-driven producer fans out over
	// this rather than resolving "the" guild. An unreachable store is
	// ErrStoreUnavailable, never an empty slice.
	GuildsOf(ctx context.Context, b Broadcaster) ([]Binding, error)
	// GuildConfig reads one guild's settings and the version to echo back on
	// the next write. Found is false for a guild that was bound but never
	// saved, which reads the same as a guild with everything switched off.
	GuildConfig(ctx context.Context, g Guild) (ddiscord.Config, int, bool)
	// SetGuildConfig writes one guild's settings and returns the version the
	// row now holds.
	SetGuildConfig(ctx context.Context, set SetConfig) (int, error)
	// Invalidate drops one guild's cached settings. Outgress calls it after
	// every successful write so the engine picks the change up on its next
	// event instead of at the end of the cache TTL.
	Invalidate(ctx context.Context, g Guild)

	TrackClone(ctx context.Context, c Clone) error
	Clone(ctx context.Context, ch Channel) (Clone, bool)
	CloneCount(ctx context.Context, g Guild) int
	ForgetClone(ctx context.Context, c Clone) error

	TrackTicket(ctx context.Context, t TicketOpen) (TicketOpenResult, error)
	// Ticket takes the guild the channel belongs to. It used to address a
	// ticket by channel alone -- a Discord channel snowflake is globally
	// unique, so the guild looked redundant -- but that made the guild filter
	// optional all the way down into discord-data, where it is what stops a
	// caller holding a channel id from another guild from reading that
	// guild's ticket. Merge note (2026-09-05): the ticket-desk branch shipped
	// the unguilded form and re-checked the guild in the engine module
	// instead; the scoped read is the same check one layer lower, where every
	// caller gets it rather than only the ones that remember.
	Ticket(ctx context.Context, g Guild, ch Channel) (Ticket, bool)
	// OpenTicketCount is how many live tickets one member holds in one guild.
	// Read before the channel is created: it both refuses an over-limit open
	// without a wasted Discord round trip and numbers the new channel.
	OpenTicketCount(ctx context.Context, m Member) int
	ClaimTicket(ctx context.Context, c TicketClaim) error
	CloseTicket(ctx context.Context, c TicketClose) error
	// PutTranscript stores a closed ticket's rendered history against its row.
	// A zero TicketID is a no-op: the pure-Valkey fallback has no ticket rows
	// to hang a transcript on.
	PutTranscript(ctx context.Context, t Transcript) error

	// TicketsDurable reports whether an opened ticket gets a row that
	// outlives the channel. False is the pure-Valkey fallback, which has no
	// row ids, no per-member open limit and no transcripts; the desk REFUSES
	// to open in that mode rather than handing a member a channel it can
	// neither number, limit nor transcribe.
	TicketsDurable(ctx context.Context) bool

	// MarkPendingClose remembers a close Discord already performed but the
	// ticket row never recorded, so the next interaction on that channel can
	// finish the write instead of leaving a row that says "open" forever.
	MarkPendingClose(ctx context.Context, c TicketClose) error
	PendingClose(ctx context.Context, ch Channel) (TicketClose, bool)
	ClearPendingClose(ctx context.Context, ch Channel) error

	// ClaimSummary reports whether THIS caller is the one that gets to post
	// the close summary for a ticket. It is a set-if-absent with a day's TTL,
	// so a retried close does not stack a second card and a second transcript
	// in the log channel. A zero ticketID (the fallback, which has no row ids)
	// always claims.
	ClaimSummary(ctx context.Context, ticketID int) bool

	ClaimDesk(ctx context.Context, g Guild) bool
	RememberDesk(ctx context.Context, p DeskPanel) error
	Desk(ctx context.Context, g Guild) (DeskPanel, bool)

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

// New builds the node-local store: Valkey-backed, or the in-memory double when
// no client is configured. It is the whole store for a process that keeps its
// Discord state in Valkey; NewRPC wraps one of these to move bindings, tickets
// and XP onto discord-data while keeping the local keyspaces here.
func New(client valkey.Client) Store { return newLocal(client) }

func guildKey(g Guild) string { return "discord:guild:" + g.ID }

// guildsKey caches one broadcaster's whole guild list. Written on every
// listing, dropped by both write verbs, and short-lived: see guildsCacheTTL.
func guildsKey(b Broadcaster) string { return "discord:guilds:" + b.ID }

// cfgKey caches one guild's settings. Read on every gateway event, written
// only from the dashboard, and invalidated directly by outgress on save -- so
// the TTL is only the backstop for an invalidation this process never saw.
func cfgKey(g Guild) string { return "discord:cfg:" + g.ID }

// BindingOf answers from the store of record: a process wired to New holds no
// cache in front of anything, so its binding reads are authoritative by
// construction.
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

// GuildsOf has no Valkey answer. The reverse index this store keeps is
// guild->broadcaster, one key per guild; listing a broadcaster's guilds from
// it would mean a KEYS scan of the whole keyspace on every dashboard load.
// discord-data indexes the column instead, so this direction exists only on
// the RPC-backed store.
func (s valkeyStore) GuildsOf(_ context.Context, b Broadcaster) ([]Binding, error) {
	zap.L().Warn("discord guild list needs the discord-data-backed store; refusing",
		zap.String("broadcaster_id", b.ID))
	return nil, ErrStoreUnavailable
}

// GuildConfig is not served here. Guild settings have no Valkey store of
// record -- discord-data holds them and this store has only the cache in front
// of it -- so answering from the cache would mean serving settings that may
// have been deleted, with no way to ever notice. A process that reaches this
// method is wired to New instead of NewRPC, which is a deployment mistake, so
// it warns rather than failing silently.
func (s valkeyStore) GuildConfig(_ context.Context, g Guild) (ddiscord.Config, int, bool) {
	zap.L().Warn("discord guild settings need the discord-data-backed store; answering not-found",
		zap.String("guild_id", g.ID))
	return ddiscord.Config{}, 0, false
}

// SetGuildConfig refuses for the same reason, loudly: a write accepted here
// would be acknowledged to the dashboard and then read back by nobody.
func (s valkeyStore) SetGuildConfig(_ context.Context, set SetConfig) (int, error) {
	zap.L().Warn("discord guild settings need the discord-data-backed store; refusing the write",
		zap.String("guild_id", set.Guild.ID))
	return 0, ErrConfigUnavailable
}

// Invalidate drops the cached settings. This one IS the Valkey store's job
// even here: the cache is the only part of the settings path that lives in
// Valkey, and outgress runs in a process that may hold either store.
func (s valkeyStore) Invalidate(ctx context.Context, g Guild) {
	s.dropConfig(ctx, g)
}

// Mem is an in-process Store for tests.
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
	// guildsCache backs the localStore guild-list cache the RPC store
	// composes. It is deliberately separate from guild: GuildsOf reads live
	// state, this is the cache in front of discord-data.
	guildsCache map[string][]Binding
}

// memConfig is one guild's stored settings in the memory double.
type memConfig struct {
	Config  ddiscord.Config
	Version int
}

// NewMem builds an empty memory store.
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

// PutGuild is the test helper that stands in for BindGuild.
func (m *Mem) PutGuild(g Guild, b Broadcaster) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.guild[g.ID] = b.ID
}

// BindingOf answers from the memory store of record.
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

// GuildsOf lists the guilds bound to b, in guild-id order so a test asserting
// on the slice does not depend on map iteration.
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

// SetGuildConfig applies the same two checks the real store does: the caller
// must own the guild, and must hold the current version.
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

// Invalidate is a no-op: the memory store has no cache in front of itself.
func (m *Mem) Invalidate(_ context.Context, _ Guild) {}

// PutGuildConfig seeds settings without going through the ownership and
// version checks, for tests that are about something else.
func (m *Mem) PutGuildConfig(g Guild, cfg ddiscord.Config) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.configs[g.ID] = memConfig{Config: cfg, Version: m.configs[g.ID].Version + 1}
}
