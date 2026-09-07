// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Ticket keyspace: tickets, their pending-close markers, close summaries,
// transcripts and the desk panel lock.
//
// One of the four keyspace files store.go was split into; see store_voice.go
// for why the split runs by keyspace across both implementations rather than
// by implementation.

package discordstore

import (
	"context"
	"strconv"
	"strings"

	"github.com/valkey-io/valkey-go"
)

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

// Ticket reads the node-local ticket key, refusing a channel that belongs to a
// different guild than the caller named.
func (s valkeyStore) Ticket(ctx context.Context, g Guild, ch Channel) (Ticket, bool) {
	raw, err := s.client.Do(ctx, s.client.B().Get().Key(ticketKey(ch)).Build()).ToString()
	if err != nil || raw == "" {
		return Ticket{}, false
	}
	t, ok := parseTicketValue(ch.ID, raw)
	if !ok || t.GuildID != g.ID {
		return Ticket{}, false
	}
	return t, true
}

// OpenTicketCount always answers zero. Counting a member's open tickets means
// a per-member index this keyspace does not keep, and the fallback mode is
// already the one without limits (see NewRPC's boot WARN): the number it would
// have to invent is worse than the honest zero, which lets the ticket open.
func (s valkeyStore) OpenTicketCount(context.Context, Member) int { return 0 }

func (s valkeyStore) ClaimTicket(ctx context.Context, c TicketClaim) error {
	t, ok := s.Ticket(ctx, Guild{ID: c.GuildID}, Channel{ID: c.ChannelID})
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

func (m *Mem) Ticket(_ context.Context, g Guild, ch Channel) (Ticket, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tickets[ch.ID]
	if !ok || t.GuildID != g.ID {
		return Ticket{}, false
	}
	return t, true
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
