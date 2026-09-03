// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"time"

	"ItsBagelBot/app/outgress/internal/worker"
	outgressrpc "ItsBagelBot/internal/domain/rpc/outgress"
	"ItsBagelBot/pkg/bus"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"
)

// setupHandleTimeout bounds one guild fill: 4 roles and 20 channels created
// one REST call at a time at 100-300 ms each, plus whatever Retry-After the
// create buckets dictate. The 4 s followage budget cancelled mid-fill on
// every real guild and left a half-built server; 45 s clears a worst-case
// fill with the dashboard's 60 s client wait still above it.
const setupHandleTimeout = 45 * time.Second

// layoutHandleTimeout covers two listings.
const layoutHandleTimeout = 10 * time.Second

// Wiring is what every RPC subscriber needs from main: the connection, the
// subject prefix and queue group, and the observability handles.
type Wiring struct {
	NC     *nats.Conn
	Prefix string
	Queue  string
	App    *newrelic.Application
	Log    *zap.Logger
}

// SubscribeDiscord wires guild setup, layout listing, unbind and home-server
// posts. The worker is always the system lane worker; with no bot token
// attached every handler answers "discord client unavailable" so the
// dashboard shows a real error instead of a no-responders timeout.
func SubscribeDiscord(w *worker.Worker, wire Wiring) error {
	if w == nil {
		return nil
	}
	d := &discordRPC{w: w, log: wire.Log}
	return subscribeAll(
		func() error {
			return bus.QueueSubscribeJSON[outgressrpc.DiscordSetupRequest, outgressrpc.DiscordSetupReply](
				wire.NC, wire.Prefix+".discord.setup", wire.Queue, setupHandleTimeout, wire.App, wire.Log, d.handleSetup)
		},
		func() error {
			return bus.QueueSubscribeJSON[outgressrpc.DiscordLayoutRequest, outgressrpc.DiscordLayoutReply](
				wire.NC, wire.Prefix+".discord.layout", wire.Queue, layoutHandleTimeout, wire.App, wire.Log, d.handleLayout)
		},
		func() error {
			return bus.QueueSubscribeJSON[outgressrpc.DiscordUnbindRequest, outgressrpc.DiscordUnbindReply](
				wire.NC, wire.Prefix+".discord.unbind", wire.Queue, handleTimeout, wire.App, wire.Log, d.handleUnbind)
		},
		func() error {
			return bus.QueueSubscribeJSON[outgressrpc.DiscordPostRequest, outgressrpc.DiscordPostReply](
				wire.NC, wire.Prefix+".discord.post", wire.Queue, handleTimeout, wire.App, wire.Log, d.handlePost)
		},
	)
}

type discordRPC struct {
	w   *worker.Worker
	log *zap.Logger
}

func (d *discordRPC) handleSetup(ctx context.Context, req outgressrpc.DiscordSetupRequest) outgressrpc.DiscordSetupReply {
	if req.GuildID == "" {
		return outgressrpc.DiscordSetupReply{Error: "missing guild_id or user_id"}
	}
	if req.UserID == "" {
		return outgressrpc.DiscordSetupReply{Error: "missing guild_id or user_id"}
	}
	got, err := d.w.SetupGuild(ctx, worker.GuildSetupRequest{GuildID: req.GuildID, BroadcasterID: req.UserID})
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
	if req.GuildID == "" {
		return outgressrpc.DiscordLayoutReply{Error: "missing guild_id or user_id"}
	}
	if req.UserID == "" {
		return outgressrpc.DiscordLayoutReply{Error: "missing guild_id or user_id"}
	}
	layout, err := d.w.GuildLayout(ctx, worker.GuildSetupRequest{GuildID: req.GuildID, BroadcasterID: req.UserID})
	if err != nil {
		return outgressrpc.DiscordLayoutReply{Error: err.Error()}
	}
	return outgressrpc.DiscordLayoutReply{
		Channels: layoutEntries(layout.Channels),
		Roles:    layoutEntries(layout.Roles),
	}
}

func layoutEntries(in []worker.GuildEntry) []outgressrpc.DiscordLayoutEntry {
	out := make([]outgressrpc.DiscordLayoutEntry, 0, len(in))
	for _, e := range in {
		out = append(out, outgressrpc.DiscordLayoutEntry{ID: e.ID, Name: e.Name, Type: e.Type})
	}
	return out
}

func (d *discordRPC) handleUnbind(ctx context.Context, req outgressrpc.DiscordUnbindRequest) outgressrpc.DiscordUnbindReply {
	if req.GuildID == "" {
		return outgressrpc.DiscordUnbindReply{Error: "missing guild_id or user_id"}
	}
	if req.UserID == "" {
		return outgressrpc.DiscordUnbindReply{Error: "missing guild_id or user_id"}
	}
	if err := d.w.UnbindGuild(ctx, worker.GuildSetupRequest{GuildID: req.GuildID, BroadcasterID: req.UserID}); err != nil {
		return outgressrpc.DiscordUnbindReply{Error: err.Error()}
	}
	return outgressrpc.DiscordUnbindReply{}
}

func (d *discordRPC) handlePost(ctx context.Context, req outgressrpc.DiscordPostRequest) outgressrpc.DiscordPostReply {
	if req.ChannelID == "" {
		return outgressrpc.DiscordPostReply{Error: "missing channel or content"}
	}
	if req.Content == "" {
		return outgressrpc.DiscordPostReply{Error: "missing channel or content"}
	}
	if err := d.w.PostDiscord(ctx, req.ChannelID, req.Content); err != nil {
		return outgressrpc.DiscordPostReply{Error: err.Error()}
	}
	return outgressrpc.DiscordPostReply{}
}
