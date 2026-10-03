// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package dispatch_test

import (
	"context"
	"sync"
	"testing"

	"ItsBagelBot/app/discord/engine/internal/dispatch"
	"ItsBagelBot/app/discord/engine/internal/registry"
	"ItsBagelBot/app/discord/engine/internal/resolve"
	"ItsBagelBot/app/discord/engine/modules"
	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"
	discordoutgress "ItsBagelBot/internal/domain/rpc/discordoutgress"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const (
	testGuild      = "100000000000000001"
	testWelcomeCh  = "100000000000000002"
	testMemberRole = "100000000000000003"
	testLogsCh     = "100000000000000004"
	testVoiceHub   = "100000000000000005"
	testTicketCat  = "100000000000000006"
	testSupportCh  = "100000000000000007"
)

type fakeChannels struct {
	mu      sync.Mutex
	created []string
	deleted []string
	moved   []string
	opened  []discordoutgress.TicketOpenRequest
}

func (f *fakeChannels) TicketOpen(_ context.Context, req discordoutgress.TicketOpenRequest) (discordoutgress.TicketOpenReply, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := "ch-" + req.Name
	f.created = append(f.created, id)
	f.opened = append(f.opened, req)
	return discordoutgress.TicketOpenReply{ChannelID: id, MessageID: "msg-" + id}, nil
}

func (f *fakeChannels) TicketClaim(context.Context, discordoutgress.TicketClaimRequest) (discordoutgress.TicketClaimReply, error) {
	return discordoutgress.TicketClaimReply{}, nil
}

func (f *fakeChannels) TicketClose(_ context.Context, req discordoutgress.TicketCloseRequest) (discordoutgress.TicketCloseReply, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, req.ChannelID)
	return discordoutgress.TicketCloseReply{}, nil
}

func (f *fakeChannels) TicketAddMember(context.Context, discordoutgress.TicketMemberAddRequest) (discordoutgress.TicketMemberAddReply, error) {
	return discordoutgress.TicketMemberAddReply{}, nil
}

func (f *fakeChannels) TicketPanel(context.Context, discordoutgress.TicketPanelRequest) (discordoutgress.TicketPanelReply, error) {
	return discordoutgress.TicketPanelReply{MessageID: "m-panel"}, nil
}

func (f *fakeChannels) CreateChannel(_ context.Context, req discordoutgress.ChannelCreateRequest) (discordoutgress.ChannelCreateReply, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := "ch-" + req.Name
	f.created = append(f.created, id)
	return discordoutgress.ChannelCreateReply{ChannelID: id}, nil
}

func (f *fakeChannels) DeleteChannel(_ context.Context, req discordoutgress.ChannelDeleteRequest) (discordoutgress.ChannelDeleteReply, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, req.ChannelID)
	return discordoutgress.ChannelDeleteReply{}, nil
}

func (f *fakeChannels) ModifyChannel(context.Context, discordoutgress.ChannelModifyRequest) (discordoutgress.ChannelModifyReply, error) {
	return discordoutgress.ChannelModifyReply{}, nil
}

func (f *fakeChannels) MoveMember(_ context.Context, req discordoutgress.MemberMoveRequest) (discordoutgress.MemberMoveReply, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.moved = append(f.moved, req.UserID+">"+req.ChannelID)
	return discordoutgress.MemberMoveReply{}, nil
}

func (f *fakeChannels) Purge(context.Context, discordoutgress.PurgeRequest) (discordoutgress.PurgeReply, error) {
	return discordoutgress.PurgeReply{Deleted: 2}, nil
}

type enabledModules struct{ cfg ddiscord.Config }

func (m enabledModules) GetModule(context.Context, uint64, string) (projection.ModuleView, bool, error) {
	raw, _ := codec.Marshal(m.cfg)
	return projection.ModuleView{IsEnabled: true, Configs: raw}, true, nil
}

type commandLog struct {
	mu   sync.Mutex
	cmds []ddiscord.Command
}

func (l *commandLog) publish(_ context.Context, c ddiscord.Command) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cmds = append(l.cmds, c)
	return nil
}

func (l *commandLog) byType(t string) []ddiscord.Command {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []ddiscord.Command
	for _, c := range l.cmds {
		if c.Type == t {
			out = append(out, c)
		}
	}
	return out
}

func (l *commandLog) followups(t *testing.T) []ddiscord.FollowupPayload {
	t.Helper()
	var out []ddiscord.FollowupPayload
	for _, c := range l.byType(ddiscord.TypeInteractionFollowup) {
		var payload ddiscord.FollowupPayload
		require.NoError(t, codec.Unmarshal(c.Payload, &payload))
		out = append(out, payload)
	}
	return out
}

type harness struct {
	t        *testing.T
	d        *dispatch.Dispatcher
	channels *fakeChannels
	store    *discordstore.Mem
	log      *commandLog
}

func newHarness(t *testing.T, cfg ddiscord.Config) *harness {
	t.Helper()
	channels := &fakeChannels{}
	store := discordstore.NewMem()
	store.PutGuild(discordstore.Guild{ID: cfg.GuildID}, discordstore.Broadcaster{ID: "42"})
	store.PutGuildConfig(discordstore.Guild{ID: cfg.GuildID}, cfg)
	log := &commandLog{}

	resolver := resolve.Resolver{
		Store: store, Modules: enabledModules{cfg: cfg},
		Tier: func(context.Context, uint64) (string, bool) { return "paid", true },
		Log:  zap.NewNop(),
	}
	reg := registry.New(modules.All(modules.Deps{Store: store, Channels: channels, Tickets: channels, Purge: channels, Log: zap.NewNop()})...)
	d := &dispatch.Dispatcher{Registry: reg, Resolver: resolver, Store: store, Publish: log.publish, Log: zap.NewNop()}
	return &harness{t: t, d: d, channels: channels, store: store, log: log}
}

func (h *harness) handle(ctx context.Context, ev ddiscord.Event) {
	h.t.Helper()
	raw, err := codec.Marshal(ev)
	require.NoError(h.t, err)
	msg := bus.NewMessage("test", raw)
	msg.SetContext(ctx)
	require.NoError(h.t, h.d.Handle(msg))
}

func (h *harness) sendCtx(ctx context.Context, guildID, eventType string, payload any) {
	h.t.Helper()
	raw, err := codec.Marshal(payload)
	require.NoError(h.t, err)
	h.handle(ctx, ddiscord.Event{Type: eventType, GuildID: guildID, Raw: raw})
}

func (h *harness) sendTo(guildID, eventType string, payload any) {
	h.t.Helper()
	h.sendCtx(context.Background(), guildID, eventType, payload)
}

func (h *harness) send(eventType string, payload any) {
	h.t.Helper()
	h.sendTo(testGuild, eventType, payload)
}

type interaction struct {
	channelID string
	data      map[string]any
	member    map[string]any
}

func (h *harness) interact(in interaction) {
	h.t.Helper()
	h.send("INTERACTION_CREATE", map[string]any{
		"id": "i1", "token": "tok", "guild_id": testGuild, "channel_id": in.channelID,
		"data": in.data, "member": in.member,
	})
}

func memberPayload(guildID string) map[string]any {
	return map[string]any{
		"guild_id": guildID,
		"user":     map[string]any{"id": "u1", "username": "Ada"},
	}
}

func voicePayload(channelID string) map[string]any {
	return map[string]any{
		"guild_id": testGuild, "channel_id": channelID, "user_id": "u1",
		"member": map[string]any{"user": map[string]any{"id": "u1", "username": "Ada"}},
	}
}

func memberWith(permissions string) map[string]any {
	return map[string]any{"user": map[string]any{"id": "u1", "username": "Ada"}, "permissions": permissions}
}
