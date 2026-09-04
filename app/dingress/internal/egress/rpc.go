// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package egress

import (
	"context"
	"time"

	outgressrpc "ItsBagelBot/internal/domain/rpc/outgress"
	"ItsBagelBot/pkg/bus"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"
)

// The RPC contract types (outgressrpc.DiscordSetupRequest and friends) live
// under internal/domain/rpc/outgress -- a shared domain package, not
// outgress-app-owned code -- because the guild setup/layout/unbind/post RPC
// moved here with the rest of Discord outbound but its wire shape did not
// change, and every existing caller already speaks it.

// setupHandleTimeout bounds one guild fill: 4 roles and ~17 channels created
// one REST call at a time at 100-300 ms each, plus whatever Retry-After the
// create buckets dictate. 45s clears a worst-case fill with the dashboard's
// own client wait still above it (ported from outgress's rpc/discord.go).
const setupHandleTimeout = 45 * time.Second

// layoutHandleTimeout covers two listings.
const layoutHandleTimeout = 10 * time.Second

// handleTimeout bounds the unbind and post handlers: one Valkey round trip
// (unbind) or one REST call (post).
const handleTimeout = 1500 * time.Millisecond

// RPCWiring is what SubscribeRPC needs from main: the connection, the
// subject prefix and queue group, and the observability handles.
type RPCWiring struct {
	NC     *nats.Conn
	Prefix string
	Queue  string
	App    *newrelic.Application
	Log    *zap.Logger
}

// SubscribeRPC wires guild setup, layout listing, unbind, and the operator
// post path. With no bot token attached (w.discord nil) every handler
// answers "discord client unavailable" so the dashboard shows a real error
// instead of a no-responders timeout.
func SubscribeRPC(w *Worker, wire RPCWiring) error {
	if w == nil {
		return nil
	}
	d := &discordRPC{w: w, log: wire.Log}
	if err := bus.QueueSubscribeJSON[outgressrpc.DiscordSetupRequest, outgressrpc.DiscordSetupReply](
		wire.NC, wire.Prefix+".discord.setup", wire.Queue, setupHandleTimeout, wire.App, wire.Log, d.handleSetup); err != nil {
		return err
	}
	if err := bus.QueueSubscribeJSON[outgressrpc.DiscordLayoutRequest, outgressrpc.DiscordLayoutReply](
		wire.NC, wire.Prefix+".discord.layout", wire.Queue, layoutHandleTimeout, wire.App, wire.Log, d.handleLayout); err != nil {
		return err
	}
	if err := bus.QueueSubscribeJSON[outgressrpc.DiscordUnbindRequest, outgressrpc.DiscordUnbindReply](
		wire.NC, wire.Prefix+".discord.unbind", wire.Queue, handleTimeout, wire.App, wire.Log, d.handleUnbind); err != nil {
		return err
	}
	return bus.QueueSubscribeJSON[outgressrpc.DiscordPostRequest, outgressrpc.DiscordPostReply](
		wire.NC, wire.Prefix+".discord.post", wire.Queue, handleTimeout, wire.App, wire.Log, d.handlePost)
}

type discordRPC struct {
	w   *Worker
	log *zap.Logger
}

func (d *discordRPC) handleSetup(ctx context.Context, req outgressrpc.DiscordSetupRequest) outgressrpc.DiscordSetupReply {
	if req.GuildID == "" || req.UserID == "" {
		return outgressrpc.DiscordSetupReply{Error: "missing guild_id or user_id"}
	}
	got, err := d.w.SetupGuild(ctx, GuildSetupRequest{GuildID: req.GuildID, BroadcasterID: req.UserID})
	if err != nil {
		return outgressrpc.DiscordSetupReply{Error: err.Error()}
	}
	return outgressrpc.DiscordSetupReply{
		GuildID:          got.GuildID,
		LiveChannelID:    got.LiveChannelID,
		ClipsChannelID:   got.ClipsChannelID,
		WelcomeChannelID: got.WelcomeChannelID,
		VoiceHubID:       got.VoiceHubID,
		LogChannelID:     got.LogChannelID,
		TicketChannelID:  got.TicketChannelID,
		TicketCategoryID: got.TicketCategoryID,
		LiveRoleID:       got.LiveRoleID,
		ModsRoleID:       got.ModsRoleID,
		RegularsRoleID:   got.RegularsRoleID,
		MemberRoleID:     got.MemberRoleID,
		Refused:          got.Refused,
	}
}

func (d *discordRPC) handleLayout(ctx context.Context, req outgressrpc.DiscordLayoutRequest) outgressrpc.DiscordLayoutReply {
	if req.GuildID == "" || req.UserID == "" {
		return outgressrpc.DiscordLayoutReply{Error: "missing guild_id or user_id"}
	}
	layout, err := d.w.GuildLayout(ctx, GuildSetupRequest{GuildID: req.GuildID, BroadcasterID: req.UserID})
	if err != nil {
		return outgressrpc.DiscordLayoutReply{Error: err.Error()}
	}
	return outgressrpc.DiscordLayoutReply{
		Channels: layoutEntries(layout.Channels),
		Roles:    layoutEntries(layout.Roles),
	}
}

func layoutEntries(in []GuildEntry) []outgressrpc.DiscordLayoutEntry {
	out := make([]outgressrpc.DiscordLayoutEntry, 0, len(in))
	for _, e := range in {
		out = append(out, outgressrpc.DiscordLayoutEntry{ID: e.ID, Name: e.Name, Type: e.Type})
	}
	return out
}

func (d *discordRPC) handleUnbind(ctx context.Context, req outgressrpc.DiscordUnbindRequest) outgressrpc.DiscordUnbindReply {
	if req.GuildID == "" || req.UserID == "" {
		return outgressrpc.DiscordUnbindReply{Error: "missing guild_id or user_id"}
	}
	if err := d.w.UnbindGuild(ctx, GuildSetupRequest{GuildID: req.GuildID, BroadcasterID: req.UserID}); err != nil {
		return outgressrpc.DiscordUnbindReply{Error: err.Error()}
	}
	return outgressrpc.DiscordUnbindReply{}
}

func (d *discordRPC) handlePost(ctx context.Context, req outgressrpc.DiscordPostRequest) outgressrpc.DiscordPostReply {
	if req.ChannelID == "" || req.Content == "" {
		return outgressrpc.DiscordPostReply{Error: "missing channel or content"}
	}
	if err := d.w.PostDiscord(ctx, req.ChannelID, req.Content); err != nil {
		return outgressrpc.DiscordPostReply{Error: err.Error()}
	}
	return outgressrpc.DiscordPostReply{}
}
