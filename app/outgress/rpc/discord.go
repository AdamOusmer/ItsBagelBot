// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"

	"ItsBagelBot/app/outgress/internal/worker"
	outgressrpc "ItsBagelBot/internal/domain/rpc/outgress"
	"ItsBagelBot/pkg/bus"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"
)

// SubscribeDiscord wires guild setup and home-server posts. A nil worker
// (no bot token) is a no-op so outgress still boots.
func SubscribeDiscord(nc *nats.Conn, w *worker.Worker, prefix, queueGroup string, app *newrelic.Application, log *zap.Logger) error {
	if w == nil {
		return nil
	}
	d := &discordRPC{w: w, log: log}
	return subscribeAll(
		func() error {
			return bus.QueueSubscribeJSON[outgressrpc.DiscordSetupRequest, outgressrpc.DiscordSetupReply](
				nc, prefix+".discord.setup", queueGroup, followageHandleTimeout, app, log, d.handleSetup)
		},
		func() error {
			return bus.QueueSubscribeJSON[outgressrpc.DiscordPostRequest, outgressrpc.DiscordPostReply](
				nc, prefix+".discord.post", queueGroup, handleTimeout, app, log, d.handlePost)
		},
	)
}

type discordRPC struct {
	w   *worker.Worker
	log *zap.Logger
}

func (d *discordRPC) handleSetup(ctx context.Context, req outgressrpc.DiscordSetupRequest) outgressrpc.DiscordSetupReply {
	if req.GuildID == "" {
		return outgressrpc.DiscordSetupReply{Error: "missing guild_id"}
	}
	got, err := d.w.SetupGuild(ctx, req.GuildID, "", req.UserID)
	if err != nil {
		return outgressrpc.DiscordSetupReply{Error: err.Error()}
	}
	return outgressrpc.DiscordSetupReply{
		GuildID:          got.GuildID,
		LiveChannelID:    got.LiveChannelID,
		ClipsChannelID:   got.ClipsChannelID,
		WelcomeChannelID: got.WelcomeChannelID,
		AlertsChannelID:  got.AlertsChannelID,
		VoiceHubID:       got.VoiceHubID,
		LiveRoleID:       got.LiveRoleID,
		ModsRoleID:       got.ModsRoleID,
		RegularsRoleID:   got.RegularsRoleID,
		MemberRoleID:     got.MemberRoleID,
		Refused:          got.Refused,
	}
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
