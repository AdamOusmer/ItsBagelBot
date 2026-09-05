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
	"strconv"
	"strings"
	"sync"

	ddiscord "ItsBagelBot/internal/domain/discord"

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
//
// ID, Status, ClaimedBy and PanelMessageID are only ever populated by the RPC
// store: they are columns on discord-data's tickets row. The pure-Valkey
// fallback keeps the three fields the old key held (channel, guild, opener)
// and leaves the rest zero, which is what the degraded mode's boot WARN is
// warning about -- see NewRPC.
type Ticket struct {
	ID             int
	ChannelID      string
	GuildID        string
	OpenerID       string
	Status         string
	ClaimedBy      string
	PanelMessageID string
}

// TicketOpen records one newly created ticket channel.
type TicketOpen struct {
	GuildID   string
	ChannelID string
	OpenerID  string
	Subject   string
	// PanelMessageID is the ticket card already posted into the channel.
	PanelMessageID string
	// OpenLimit is the guild's per-member cap on simultaneously open tickets.
	// Zero means unlimited. The check happens where the rows are (see
	// TicketOpenResult.AtLimit): a caller-side count would need a second round
	// trip and would still race with itself.
	OpenLimit int
}

// TicketOpenResult is what recording a ticket answered. AtLimit is the refusal:
// the row was NOT written, and OpenCount is how many the opener already holds.
type TicketOpenResult struct {
	TicketID  int
	OpenCount int
	AtLimit   bool
}

// TicketClaim marks a ticket as being handled by StaffID.
type TicketClaim struct {
	GuildID   string
	ChannelID string
	StaffID   string
}

// TicketClose ends a ticket. A non-empty ArchivedChannelID means the channel
// was moved into the archive category rather than deleted.
type TicketClose struct {
	GuildID           string
	ChannelID         string
	ClosedBy          string
	ArchivedChannelID string
}

// Transcript is one closed ticket's rendered history.
type Transcript struct {
	TicketID     int
	Body         string
	MessageCount int
}

// DeskPanel is the remembered ticket-desk panel message, so a repost can
// delete the previous one instead of stacking a second panel under the first.
type DeskPanel struct {
	GuildID   string
	ChannelID string
	MessageID string
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

	TrackTicket(ctx context.Context, t TicketOpen) (TicketOpenResult, error)
	Ticket(ctx context.Context, ch Channel) (Ticket, bool)
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

func cloneKey(ch Channel) string { return "discord:voice:" + ch.ID }

func cloneSet(g Guild) string { return "discord:voices:" + g.ID }

func ticketKey(ch Channel) string { return "discord:ticket:" + ch.ID }

func deskKey(g Guild) string { return "discord:ticketdesk:" + g.ID }

func pendingCloseKey(ch Channel) string { return "discord:ticket:pendingclose:" + ch.ID }

func summaryKey(ticketID int) string { return "discord:ticket:summary:" + strconv.Itoa(ticketID) }

// pendingCloseTTL is how long an unrecorded close waits for a retry.
//
// 24 hours, matching the summary memo. Long enough that a discord-data outage
// measured in hours still ends with the row written by whoever next touches
// the channel; short enough that a marker for a channel nobody ever opens
// again expires instead of accumulating. Indefinite was the alternative and is
// worse: the retry is best effort, and a key with no TTL is a leak with no
// reader.
const pendingCloseTTL = 24 * 60 * 60

// summaryTTL is how long the "this ticket's summary was posted" memo lives.
// A retry that arrives a day after the close is not a duplicate press, it is a
// new decision, and the log channel should show it.
const summaryTTL = 24 * 60 * 60

// deskClaimed is the desk key's value when the panel was posted but its
// message id is unknown -- the engine's EnsureDesk posts through a
// fire-and-forget Command and never learns one. It is the byte the key has
// always held, kept so an existing claim keeps reading as a claim.
const deskClaimed = "1"

// parseDeskValue reads the desk key. "1" (or anything without a separator) is
// a claim with no remembered message; "<channel>|<message>" is a panel a
// repost can delete first.
func parseDeskValue(guildID, raw string) (DeskPanel, bool) {
	channelID, messageID, ok := strings.Cut(raw, "|")
	if !ok {
		return DeskPanel{GuildID: guildID}, true
	}
	return DeskPanel{GuildID: guildID, ChannelID: channelID, MessageID: messageID}, true
}

// Ticket status values, mirroring discorddata's (which mirror the ent enum).
// Duplicated rather than imported: the pure-Valkey path must not depend on the
// data service's wire package to name the state of its own key.
const (
	TicketStatusOpen     = "open"
	TicketStatusClaimed  = "claimed"
	TicketStatusClosed   = "closed"
	TicketStatusArchived = "archived"
)

// TicketOver reports whether a ticket has reached a terminal state. A row in
// one of these still EXISTS (discord-data keeps closed tickets; the archived
// channel is often still in the guild), so every desk action has to ask rather
// than assume that a row it found is a live ticket.
func TicketOver(status string) bool {
	return status == TicketStatusClosed || status == TicketStatusArchived
}

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

// ticketValue is the pure-Valkey ticket record. It is a pipe-joined triple
// rather than JSON to stay byte-compatible with the key this store has always
// written; the claim and panel fields are appended, and a value written by an
// older build simply parses with them empty.
func ticketValue(t Ticket) string {
	return strings.Join([]string{t.GuildID, t.OpenerID, t.ClaimedBy, t.PanelMessageID}, "|")
}

func parseTicketValue(channelID, raw string) (Ticket, bool) {
	parts := strings.Split(raw, "|")
	if len(parts) < 2 {
		return Ticket{}, false
	}
	t := Ticket{ChannelID: channelID, GuildID: parts[0], OpenerID: parts[1], Status: TicketStatusOpen}
	if len(parts) > 2 {
		t.ClaimedBy = parts[2]
	}
	if len(parts) > 3 {
		t.PanelMessageID = parts[3]
	}
	if t.ClaimedBy != "" {
		t.Status = TicketStatusClaimed
	}
	return t, true
}

func parsePendingClose(channelID, raw string) (TicketClose, bool) {
	parts := strings.Split(raw, "|")
	if len(parts) < 3 {
		return TicketClose{}, false
	}
	return TicketClose{
		GuildID: parts[0], ChannelID: channelID, ClosedBy: parts[1], ArchivedChannelID: parts[2],
	}, true
}

// TrackTicket writes the channel key. The open limit is NOT enforced here: a
// per-member count would mean a second Valkey keyspace that nothing else reads
// and that no other replica's writes are ordered against, and this store is
// already the degraded fallback (see NewRPC's boot WARN).
func (s valkeyStore) TrackTicket(ctx context.Context, t TicketOpen) (TicketOpenResult, error) {
	ch := Channel{ID: t.ChannelID}
	value := ticketValue(Ticket{GuildID: t.GuildID, OpenerID: t.OpenerID, PanelMessageID: t.PanelMessageID})
	err := s.client.Do(ctx, s.client.B().Set().Key(ticketKey(ch)).Value(value).Build()).Error()
	return TicketOpenResult{}, err
}

func (s valkeyStore) Ticket(ctx context.Context, ch Channel) (Ticket, bool) {
	raw, err := s.client.Do(ctx, s.client.B().Get().Key(ticketKey(ch)).Build()).ToString()
	if err != nil || raw == "" {
		return Ticket{}, false
	}
	return parseTicketValue(ch.ID, raw)
}

// OpenTicketCount always answers zero. Counting a member's open tickets means
// a per-member index this keyspace does not keep, and the fallback mode is
// already the one without limits (see NewRPC's boot WARN): the number it would
// have to invent is worse than the honest zero, which lets the ticket open.
func (s valkeyStore) OpenTicketCount(context.Context, Member) int { return 0 }

func (s valkeyStore) ClaimTicket(ctx context.Context, c TicketClaim) error {
	t, ok := s.Ticket(ctx, Channel{ID: c.ChannelID})
	if !ok {
		return nil
	}
	t.ClaimedBy = c.StaffID
	return s.client.Do(ctx, s.client.B().Set().Key(ticketKey(Channel{ID: c.ChannelID})).Value(ticketValue(t)).Build()).Error()
}

// CloseTicket drops the key. Nothing survives a close in this mode -- the
// history the desk shows lives on discord-data's row, which the fallback has
// no access to.
func (s valkeyStore) CloseTicket(ctx context.Context, c TicketClose) error {
	return s.client.Do(ctx, s.client.B().Del().Key(ticketKey(Channel{ID: c.ChannelID})).Build()).Error()
}

// PutTranscript drops the transcript. There is no ticket row to attach it to
// and a 2 MiB Valkey value per closed ticket, kept forever, is the wrong shape
// for a cache; the close summary still posts to the log channel either way.
func (s valkeyStore) PutTranscript(context.Context, Transcript) error { return nil }

// TicketsDurable is false: this store keeps one key per live ticket channel
// and nothing else. See the interface's doc for what the desk does about it.
func (valkeyStore) TicketsDurable(context.Context) bool { return false }

func (s valkeyStore) MarkPendingClose(ctx context.Context, c TicketClose) error {
	value := strings.Join([]string{c.GuildID, c.ClosedBy, c.ArchivedChannelID}, "|")
	key := pendingCloseKey(Channel{ID: c.ChannelID})
	return s.client.Do(ctx, s.client.B().Set().Key(key).Value(value).ExSeconds(pendingCloseTTL).Build()).Error()
}

func (s valkeyStore) PendingClose(ctx context.Context, ch Channel) (TicketClose, bool) {
	raw, err := s.client.Do(ctx, s.client.B().Get().Key(pendingCloseKey(ch)).Build()).ToString()
	if err != nil || raw == "" {
		return TicketClose{}, false
	}
	return parsePendingClose(ch.ID, raw)
}

func (s valkeyStore) ClearPendingClose(ctx context.Context, ch Channel) error {
	return s.client.Do(ctx, s.client.B().Del().Key(pendingCloseKey(ch)).Build()).Error()
}

func (s valkeyStore) ClaimSummary(ctx context.Context, ticketID int) bool {
	if ticketID <= 0 {
		return true
	}
	key := summaryKey(ticketID)
	err := s.client.Do(ctx, s.client.B().Set().Key(key).Value("1").Nx().ExSeconds(summaryTTL).Build()).Error()
	return err == nil
}

func (s valkeyStore) ClaimDesk(ctx context.Context, g Guild) bool {
	err := s.client.Do(ctx, s.client.B().Set().Key(deskKey(g)).Value(deskClaimed).Nx().Build()).Error()
	return err == nil
}

// RememberDesk stores the panel pointer. A remember with NO message id is
// set-if-absent, never an overwrite: it is a claim, and letting it overwrite
// would erase the id of a panel that IS posted, which is the one thing a
// repost needs to delete the old panel instead of stacking a second one.
func (s valkeyStore) RememberDesk(ctx context.Context, p DeskPanel) error {
	key := deskKey(Guild{ID: p.GuildID})
	if p.MessageID == "" {
		return ignoreNil(s.client.Do(ctx, s.client.B().Set().Key(key).Value(deskClaimed).Nx().Build()).Error())
	}
	value := p.ChannelID + "|" + p.MessageID
	return s.client.Do(ctx, s.client.B().Set().Key(key).Value(value).Build()).Error()
}

// ignoreNil folds SET NX's "the key already existed" answer into success: not
// writing because someone else already did is the outcome the caller wanted.
func ignoreNil(err error) error {
	if valkey.IsValkeyNil(err) {
		return nil
	}
	return err
}

func (s valkeyStore) Desk(ctx context.Context, g Guild) (DeskPanel, bool) {
	raw, err := s.client.Do(ctx, s.client.B().Get().Key(deskKey(g)).Build()).ToString()
	if err != nil || raw == "" {
		return DeskPanel{}, false
	}
	return parseDeskValue(g.ID, raw)
}

func (s valkeyStore) AddXP(ctx context.Context, m Member) (int, bool, int) {
	if !s.takeXPCooldown(ctx, m) {
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

// levelOf is the XP->level curve, owned by internal/domain/discord so the
// discord-data repository (which stores the level column) and this fast path
// cannot drift apart. Kept as a local shim because every caller here holds an
// int, not the int64 the stored column uses.
func levelOf(xp int) int { return ddiscord.LevelOf(int64(xp)) }

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

// TrackTicket records a ticket and enforces the open limit, which the Valkey
// store does not: the double is what module tests exercise the limit refusal
// against, and a double that cannot refuse would let that path go untested.
func (m *Mem) TrackTicket(_ context.Context, t TicketOpen) (TicketOpenResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := m.openCountLocked(t.GuildID, t.OpenerID)
	if t.OpenLimit > 0 && count >= t.OpenLimit {
		return TicketOpenResult{OpenCount: count, AtLimit: true}, nil
	}
	m.ticketSeq++
	m.tickets[t.ChannelID] = Ticket{
		ID: m.ticketSeq, ChannelID: t.ChannelID, GuildID: t.GuildID, OpenerID: t.OpenerID,
		Status: TicketStatusOpen, PanelMessageID: t.PanelMessageID,
	}
	return TicketOpenResult{TicketID: m.ticketSeq, OpenCount: count + 1}, nil
}

func (m *Mem) openCountLocked(guildID, openerID string) int {
	n := 0
	for _, t := range m.tickets {
		if t.GuildID == guildID && t.OpenerID == openerID {
			n++
		}
	}
	return n
}

func (m *Mem) Ticket(_ context.Context, ch Channel) (Ticket, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tickets[ch.ID]
	return t, ok
}

func (m *Mem) OpenTicketCount(_ context.Context, mem Member) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.openCountLocked(mem.GuildID, mem.UserID)
}

func (m *Mem) ClaimTicket(_ context.Context, c TicketClaim) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tickets[c.ChannelID]
	if !ok {
		return nil
	}
	t.ClaimedBy = c.StaffID
	t.Status = TicketStatusClaimed
	m.tickets[c.ChannelID] = t
	return nil
}

func (m *Mem) CloseTicket(_ context.Context, c TicketClose) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.tickets, c.ChannelID)
	return nil
}

// PutTranscript keeps the body so a test can assert what was rendered.
func (m *Mem) PutTranscript(_ context.Context, t Transcript) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.transcripts[t.TicketID] = t
	return nil
}

// Transcript reads back what PutTranscript stored. Test-only: it is not part
// of Store, because nothing in production reads a transcript back through it.
func (m *Mem) Transcript(id int) (Transcript, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.transcripts[id]
	return t, ok
}

// TicketsDurable is true on the double: it enforces the open limit and keeps
// ticket ids, which is what the module tests exercise the durable path with.
func (*Mem) TicketsDurable(context.Context) bool { return true }

func (m *Mem) MarkPendingClose(_ context.Context, c TicketClose) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pending[c.ChannelID] = c
	return nil
}

func (m *Mem) PendingClose(_ context.Context, ch Channel) (TicketClose, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.pending[ch.ID]
	return c, ok
}

func (m *Mem) ClearPendingClose(_ context.Context, ch Channel) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.pending, ch.ID)
	return nil
}

func (m *Mem) ClaimSummary(_ context.Context, ticketID int) bool {
	if ticketID <= 0 {
		return true
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.summaries[ticketID] {
		return false
	}
	m.summaries[ticketID] = true
	return true
}

func (m *Mem) ClaimDesk(_ context.Context, g Guild) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, claimed := m.desk[g.ID]; claimed {
		return false
	}
	m.desk[g.ID] = DeskPanel{GuildID: g.ID}
	return true
}

// RememberDesk mirrors the Valkey store's rule: a remember with no message id
// never erases a pointer that has one.
func (m *Mem) RememberDesk(_ context.Context, p DeskPanel) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, held := m.desk[p.GuildID]; held && p.MessageID == "" {
		return nil
	}
	m.desk[p.GuildID] = p
	return nil
}

func (m *Mem) Desk(_ context.Context, g Guild) (DeskPanel, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.desk[g.ID]
	return p, ok
}

func (m *Mem) AddXP(_ context.Context, mem Member) (int, bool, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := mem.key()
	if !m.takeXPCooldownLocked(mem) {
		xp := m.xp[k]
		return xp, false, levelOf(xp)
	}
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

// SeedTicket plants a ticket row in whatever state a test needs, including
// the terminal ones TrackTicket cannot produce. Test-only.
func (m *Mem) SeedTicket(t Ticket) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t.ID == 0 {
		m.ticketSeq++
		t.ID = m.ticketSeq
	}
	m.tickets[t.ChannelID] = t
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
