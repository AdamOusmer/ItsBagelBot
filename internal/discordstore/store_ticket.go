// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordstore

import (
	"context"
	"strconv"
	"strings"

	"github.com/valkey-io/valkey-go"
)

type Ticket struct {
	ID             int
	ChannelID      string
	GuildID        string
	OpenerID       string
	Status         string
	ClaimedBy      string
	PanelMessageID string
}

type TicketOpen struct {
	GuildID        string
	ChannelID      string
	OpenerID       string
	Subject        string
	PanelMessageID string
	OpenLimit      int
}

type TicketOpenResult struct {
	TicketID  int
	OpenCount int
	AtLimit   bool
}

type TicketClaim struct {
	GuildID   string
	ChannelID string
	StaffID   string
}

type TicketClose struct {
	GuildID           string
	ChannelID         string
	ClosedBy          string
	ArchivedChannelID string
}

type Transcript struct {
	TicketID     int
	Body         string
	MessageCount int
}

type DeskPanel struct {
	GuildID   string
	ChannelID string
	MessageID string
}

func ticketKey(ch Channel) string { return "discord:ticket:" + ch.ID }

func deskKey(g Guild) string { return "discord:ticketdesk:" + g.ID }

func pendingCloseKey(ch Channel) string { return "discord:ticket:pendingclose:" + ch.ID }

func summaryKey(ticketID int) string { return "discord:ticket:summary:" + strconv.Itoa(ticketID) }

const pendingCloseTTL = 24 * 60 * 60

const summaryTTL = 24 * 60 * 60

const deskClaimed = "1"

func parseDeskValue(guildID, raw string) (DeskPanel, bool) {
	channelID, messageID, ok := strings.Cut(raw, "|")
	if !ok {
		return DeskPanel{GuildID: guildID}, true
	}
	return DeskPanel{GuildID: guildID, ChannelID: channelID, MessageID: messageID}, true
}

const (
	TicketStatusOpen     = "open"
	TicketStatusClaimed  = "claimed"
	TicketStatusClosed   = "closed"
	TicketStatusArchived = "archived"
)

func TicketOver(status string) bool {
	return status == TicketStatusClosed || status == TicketStatusArchived
}

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

func (s valkeyStore) TrackTicket(ctx context.Context, t TicketOpen) (TicketOpenResult, error) {
	ch := Channel{ID: t.ChannelID}
	value := ticketValue(Ticket{GuildID: t.GuildID, OpenerID: t.OpenerID, PanelMessageID: t.PanelMessageID})
	err := s.client.Do(ctx, s.client.B().Set().Key(ticketKey(ch)).Value(value).Build()).Error()
	return TicketOpenResult{}, err
}

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

func (s valkeyStore) OpenTicketCount(context.Context, Member) int { return 0 }

func (s valkeyStore) ClaimTicket(ctx context.Context, c TicketClaim) error {
	t, ok := s.Ticket(ctx, Guild{ID: c.GuildID}, Channel{ID: c.ChannelID})
	if !ok {
		return nil
	}
	t.ClaimedBy = c.StaffID
	return s.client.Do(ctx, s.client.B().Set().Key(ticketKey(Channel{ID: c.ChannelID})).Value(ticketValue(t)).Build()).Error()
}

func (s valkeyStore) CloseTicket(ctx context.Context, c TicketClose) error {
	return s.client.Do(ctx, s.client.B().Del().Key(ticketKey(Channel{ID: c.ChannelID})).Build()).Error()
}

func (s valkeyStore) PutTranscript(context.Context, Transcript) error { return nil }

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

func (s valkeyStore) RememberDesk(ctx context.Context, p DeskPanel) error {
	key := deskKey(Guild{ID: p.GuildID})
	if p.MessageID == "" {
		return ignoreNil(s.client.Do(ctx, s.client.B().Set().Key(key).Value(deskClaimed).Nx().Build()).Error())
	}
	value := p.ChannelID + "|" + p.MessageID
	return s.client.Do(ctx, s.client.B().Set().Key(key).Value(value).Build()).Error()
}

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

func (m *Mem) PutTranscript(_ context.Context, t Transcript) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.transcripts[t.TicketID] = t
	return nil
}

func (m *Mem) Transcript(id int) (Transcript, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.transcripts[id]
	return t, ok
}

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

func (m *Mem) SeedTicket(t Ticket) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t.ID == 0 {
		m.ticketSeq++
		t.ID = m.ticketSeq
	}
	m.tickets[t.ChannelID] = t
}
