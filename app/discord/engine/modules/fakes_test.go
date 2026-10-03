// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules_test

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/discord/engine/internal/decode"
	"ItsBagelBot/app/discord/engine/module"
	"ItsBagelBot/app/discord/engine/modules"
	"ItsBagelBot/internal/discordapi"
	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/internal/domain/discord/linkguard"
	discordoutgress "ItsBagelBot/internal/domain/rpc/discordoutgress"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type rpcTally struct {
	Opened        int
	Renamed       []string
	PanelChannels []string
	Claimed       int
	Closed        int
	Added         []string
	Deleted       []string
}

type stubTickets struct {
	openReply  discordoutgress.TicketOpenReply
	openErr    error
	closeReply discordoutgress.TicketCloseReply
	panelReply discordoutgress.TicketPanelReply

	opened  []discordoutgress.TicketOpenRequest
	claimed []discordoutgress.TicketClaimRequest
	closed  []discordoutgress.TicketCloseRequest
	tally   rpcTally
}

func newStubTickets() *stubTickets {
	return &stubTickets{
		openReply:  discordoutgress.TicketOpenReply{ChannelID: "c-new", MessageID: "m-new"},
		panelReply: discordoutgress.TicketPanelReply{MessageID: "m-panel"},
	}
}

func (s *stubTickets) TicketPanel(_ context.Context, req discordoutgress.TicketPanelRequest) (discordoutgress.TicketPanelReply, error) {
	s.tally.PanelChannels = append(s.tally.PanelChannels, req.ChannelID)
	return s.panelReply, nil
}

func (s *stubTickets) ModifyChannel(_ context.Context, req discordoutgress.ChannelModifyRequest) (discordoutgress.ChannelModifyReply, error) {
	s.tally.Renamed = append(s.tally.Renamed, req.Name)
	return discordoutgress.ChannelModifyReply{}, nil
}

func (s *stubTickets) TicketOpen(_ context.Context, req discordoutgress.TicketOpenRequest) (discordoutgress.TicketOpenReply, error) {
	s.tally.Opened++
	s.opened = append(s.opened, req)
	return s.openReply, s.openErr
}

func (s *stubTickets) TicketClaim(_ context.Context, req discordoutgress.TicketClaimRequest) (discordoutgress.TicketClaimReply, error) {
	s.tally.Claimed++
	s.claimed = append(s.claimed, req)
	return discordoutgress.TicketClaimReply{}, nil
}

func (s *stubTickets) TicketClose(_ context.Context, req discordoutgress.TicketCloseRequest) (discordoutgress.TicketCloseReply, error) {
	s.tally.Closed++
	s.closed = append(s.closed, req)
	return s.closeReply, nil
}

func (s *stubTickets) TicketAddMember(_ context.Context, req discordoutgress.TicketMemberAddRequest) (discordoutgress.TicketMemberAddReply, error) {
	s.tally.Added = append(s.tally.Added, req.UserID)
	return discordoutgress.TicketMemberAddReply{}, nil
}

func (s *stubTickets) DeleteChannel(_ context.Context, req discordoutgress.ChannelDeleteRequest) (discordoutgress.ChannelDeleteReply, error) {
	s.tally.Deleted = append(s.tally.Deleted, req.ChannelID)
	return discordoutgress.ChannelDeleteReply{}, nil
}

type actor struct {
	id    string
	name  string
	perms string
	roles []string
}

var (
	ada      = actor{id: "u1", name: "Ada", perms: "0"}
	helper   = actor{id: "mod1", name: "Hal", perms: "0", roles: []string{"helper"}}
	stranger = actor{id: "u9", name: "Sam", perms: "0", roles: []string{"nobody"}}
)

func (a actor) in(channelID string) decode.InteractionEvent {
	var in decode.InteractionEvent
	in.GuildID = "g1"
	in.ChannelID = channelID
	in.Token = "tok"
	in.Member.User = decode.UserRef{ID: a.id, Username: a.name}
	in.Member.Permissions = a.perms
	in.Member.Roles = a.roles
	return in
}

func (a actor) inSupport() decode.InteractionEvent { return a.in("support") }

func (a actor) inTicket() decode.InteractionEvent {
	in := a.in("c-new")
	in.Channel.Name = "ticket-ada-1"
	return in
}

func runHandler(t *testing.T, h module.Handler, cfg ddiscord.Config, in decode.InteractionEvent) []ddiscord.Command {
	t.Helper()
	raw, err := codec.Marshal(in)
	require.NoError(t, err)
	var emitted []ddiscord.Command
	c := &module.Context{Event: ddiscord.Event{Raw: raw}, Config: cfg, Log: zap.NewNop()}
	require.NoError(t, h(context.Background(), c, func(cmd ddiscord.Command) { emitted = append(emitted, cmd) }))
	return emitted
}

func ephemeralText(t *testing.T, cmds []ddiscord.Command) string {
	t.Helper()
	require.Len(t, cmds, 1, "an interaction is answered once")
	require.Equal(t, ddiscord.TypeInteractionFollowup, cmds[0].Type)
	var payload ddiscord.FollowupPayload
	require.NoError(t, codec.Unmarshal(cmds[0].Payload, &payload))
	require.True(t, payload.Ephemeral, "desk answers must be ephemeral")
	return payload.Content
}

func baseConfig() ddiscord.Config {
	return ddiscord.Config{
		GuildID: "g1", TicketChannelID: "support", TicketCategoryID: "cat",
		ModsRoleID: "m", TicketStaffRoles: "helper", TicketLogChannelID: "log1",
	}
}

type storeFaults struct {
	nonDurable bool
	staleCount bool
}

type faultyStore struct {
	discordstore.Store
	faults storeFaults
}

func (s faultyStore) TicketsDurable(ctx context.Context) bool {
	return !s.faults.nonDurable && s.Store.TicketsDurable(ctx)
}

func (s faultyStore) OpenTicketCount(ctx context.Context, m discordstore.Member) int {
	if s.faults.staleCount {
		return 0
	}
	return s.Store.OpenTicketCount(ctx, m)
}

type desk struct {
	t   *testing.T
	cfg ddiscord.Config
	mem *discordstore.Mem
	rpc *stubTickets
	mod module.Module
}

func newDesk(t *testing.T, cfg ddiscord.Config) *desk {
	t.Helper()
	d := &desk{t: t, cfg: cfg, mem: discordstore.NewMem(), rpc: newStubTickets()}
	d.useStore(d.mem)
	return d
}

func (d *desk) useStore(store discordstore.Store) {
	d.mod = modules.Ticket(store, d.rpc, zap.NewNop())
}

func (d *desk) button(customID string, in decode.InteractionEvent) string {
	d.t.Helper()
	return ephemeralText(d.t, runHandler(d.t, d.mod.Buttons[customID], d.cfg, in))
}

func (d *desk) slash(sub decode.InteractionOption, in decode.InteractionEvent) string {
	d.t.Helper()
	in.Data.Options = []decode.InteractionOption{sub}
	return ephemeralText(d.t, runHandler(d.t, d.mod.Slash["ticket"], d.cfg, in))
}

func (d *desk) open(a actor) string {
	d.t.Helper()
	return d.button(discordapi.CustomTicketOpen, a.inSupport())
}

func (d *desk) ticket() (discordstore.Ticket, bool) {
	return d.mem.Ticket(context.Background(), discordstore.Guild{ID: "g1"}, discordstore.Channel{ID: "c-new"})
}

func subcommand(name string, opts ...decode.InteractionOption) decode.InteractionOption {
	return decode.InteractionOption{Name: name, Options: opts}
}

func userOption(id string) decode.InteractionOption {
	return decode.InteractionOption{Name: "user", Type: 6, Value: codec.RawMessage(`"` + id + `"`)}
}

type fakeGuard struct {
	verdicts map[string]linkguard.Verdict
	seen     []linkguard.Sighting
}

func (f *fakeGuard) Observe(_ context.Context, s linkguard.Sighting) linkguard.Verdict {
	f.seen = append(f.seen, s)
	norm, invite := linkguard.NormalizeLink(s.Link)
	if v, ok := f.verdicts[norm]; ok {
		return v
	}
	return linkguard.Verdict{Allow: true, Reason: linkguard.ReasonBelowThreshold, NormalizedLink: norm, IsInvite: invite}
}

type fakeOwnInvite struct {
	own   map[string]bool
	err   error
	calls []string
}

func (f *fakeOwnInvite) IsOwnGuildInvite(_ context.Context, _ string, rawLink string) (bool, error) {
	f.calls = append(f.calls, rawLink)
	if f.err != nil {
		return false, f.err
	}
	return f.own[rawLink], nil
}

type fakeInviteResolver struct {
	reply discordoutgress.InviteResolveReply
	err   error
	calls int
}

func (f *fakeInviteResolver) ResolveInvite(context.Context, discordoutgress.InviteResolveRequest) (discordoutgress.InviteResolveReply, error) {
	f.calls++
	return f.reply, f.err
}

type cachedGuild struct {
	GuildID string
	TTL     time.Duration
}

type memInviteCache struct{ entries map[string]cachedGuild }

func (c *memInviteCache) Get(_ context.Context, code string) (string, bool) {
	e, ok := c.entries[code]
	return e.GuildID, ok
}

func (c *memInviteCache) Put(_ context.Context, code, guildID string, ttl time.Duration) error {
	c.entries[code] = cachedGuild{GuildID: guildID, TTL: ttl}
	return nil
}

type fakeApplied struct {
	seen      map[string]string
	recordErr error
	records   int
}

func (f *fakeApplied) Applied(_ context.Context, guildID string) (string, bool) {
	v, ok := f.seen[guildID]
	return v, ok
}

func (f *fakeApplied) Record(_ context.Context, guildID, fingerprint string) error {
	f.records++
	if f.recordErr != nil {
		return f.recordErr
	}
	f.seen[guildID] = fingerprint
	return nil
}
