// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package rpc holds outgress's two RPC surfaces: the dashboard-facing guild
// setup/layout/unbind/post RPC (setup_rpc.go, ported unchanged from
// app/dingress/internal/egress/rpc.go -- see that file's original doc for
// why the wire types live in internal/domain/rpc/outgress) and the new
// engine-facing channel/live RPC (engine_rpc.go, see
// internal/domain/rpc/discordoutgress's doc for why it exists at all).
package rpc

import (
	"context"
	"errors"
	"time"

	"ItsBagelBot/app/discord/outgress/internal/kv"
	"ItsBagelBot/app/discord/outgress/internal/setup"
	"ItsBagelBot/internal/discordstore"
	outgressrpc "ItsBagelBot/internal/domain/rpc/outgress"
	"ItsBagelBot/pkg/bus"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"
)

// setupHandleTimeout bounds one guild fill: 4 roles and ~17 channels created
// one REST call at a time at 100-300 ms each, plus whatever Retry-After the
// create buckets dictate. 45s clears a worst-case fill with the dashboard's
// own client wait still above it.
const setupHandleTimeout = 45 * time.Second

// layoutHandleTimeout covers two listings.
const layoutHandleTimeout = 10 * time.Second

// handleTimeout bounds the unbind and post handlers: one Valkey round trip
// (unbind) or one REST call (post).
const handleTimeout = 1500 * time.Millisecond

// configHandleTimeout bounds a settings read or write: an ownership lookup
// plus one discord-data round trip, each of which is a single indexed query.
const configHandleTimeout = 3 * time.Second

// guildsHandleTimeout bounds the server picker: one binding listing plus one
// GetGuild per bound guild, serially. Ten seconds covers a streamer with a
// dozen servers even with a Retry-After in the middle; the alternative,
// fanning the lookups out concurrently, would multiply this bot's share of
// Discord's global bucket by the number of open dashboards.
const guildsHandleTimeout = 10 * time.Second

// SetupWiring is what SubscribeSetup needs from main: the connection, the
// subject prefix and queue group, and the observability handles.
type SetupWiring struct {
	NC     *nats.Conn
	Prefix string
	Queue  string
	App    *newrelic.Application
	// Reauth reports guilds whose bot role predates CHANGE_NICKNAME so the
	// dashboard can prompt a re-authorization. Optional; nil simply never
	// raises the prompt.
	Reauth reauthReader
	Log    *zap.Logger
}

// SubscribeSetup wires guild setup, layout listing, unbind, and the
// operator post path -- unchanged wire contract from app/dingress's
// ROLE=egress (bagel.rpc.dingress.discord.*), so the console needs no
// change. With no bot token attached (w's REST client nil) every handler
// answers "discord client unavailable" so the dashboard shows a real error
// instead of a no-responders timeout.
func SubscribeSetup(w *setup.Worker, wire SetupWiring) error {
	if w == nil {
		return nil
	}
	d := &discordRPC{w: w, reauth: wire.Reauth, log: wire.Log}
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
	if err := bus.QueueSubscribeJSON[outgressrpc.DiscordPostRequest, outgressrpc.DiscordPostReply](
		wire.NC, wire.Prefix+".discord.post", wire.Queue, handleTimeout, wire.App, wire.Log, d.handlePost); err != nil {
		return err
	}
	return subscribeGuildConfig(d, wire)
}

type discordRPC struct {
	w      *setup.Worker
	reauth reauthReader
	log    *zap.Logger
}

// reauthReader is the read slice of kv.ReauthStore. Only the read half is
// taken: this RPC reports the flag, outgress's command handlers are what set
// and clear it (a rename either being refused or succeeding is the only
// evidence either way).
type reauthReader interface {
	NeedsReauth(ctx context.Context, guildID kv.GuildID) bool
}

func (d *discordRPC) handleSetup(ctx context.Context, req outgressrpc.DiscordSetupRequest) outgressrpc.DiscordSetupReply {
	if req.GuildID == "" || req.UserID == "" {
		return outgressrpc.DiscordSetupReply{Error: "missing guild_id or user_id"}
	}
	got, err := d.w.SetupGuild(ctx, setup.GuildSetupRequest{GuildID: req.GuildID, BroadcasterID: req.UserID, Subscribers: req.Subscribers})
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
		SubsChannelID:    got.SubsChannelID,
		SubsCategoryID:   got.SubsCategoryID,
		VIPChannelID:     got.VIPChannelID,
		VIPCategoryID:    got.VIPCategoryID,
		OwnerRoleID:      got.OwnerRoleID,
		LeadModRoleID:    got.LeadModRoleID,
		ModsRoleID:       got.ModsRoleID,
		VIPRoleID:        got.VIPRoleID,
		SubscriberRoleID: got.SubscriberRoleID,
		RegularsRoleID:   got.RegularsRoleID,
		MemberRoleID:     got.MemberRoleID,
		Refused:          got.Refused,
	}
}

func (d *discordRPC) handleLayout(ctx context.Context, req outgressrpc.DiscordLayoutRequest) outgressrpc.DiscordLayoutReply {
	if req.GuildID == "" || req.UserID == "" {
		return outgressrpc.DiscordLayoutReply{Error: "missing guild_id or user_id"}
	}
	layout, err := d.w.GuildLayout(ctx, setup.GuildSetupRequest{GuildID: req.GuildID, BroadcasterID: req.UserID})
	if err != nil {
		return outgressrpc.DiscordLayoutReply{Error: err.Error()}
	}
	return outgressrpc.DiscordLayoutReply{
		Channels:    layoutEntries(layout.Channels),
		Roles:       layoutEntries(layout.Roles),
		NeedsReauth: d.needsReauth(ctx, kv.GuildID(req.GuildID)),
	}
}

func layoutEntries(in []setup.GuildEntry) []outgressrpc.DiscordLayoutEntry {
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
	if err := d.w.UnbindGuild(ctx, setup.GuildSetupRequest{GuildID: req.GuildID, BroadcasterID: req.UserID}); err != nil {
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

// needsReauth reports whether this guild refused the premium rename. Nil
// store means the bookkeeping is not wired (tests), which reads as "no
// prompt" rather than as an error: a missing flag must never block the
// layout the dashboard actually asked for.
func (d *discordRPC) needsReauth(ctx context.Context, guildID kv.GuildID) bool {
	if d.reauth == nil {
		return false
	}
	return d.reauth.NeedsReauth(ctx, guildID)
}

// subscribeGuildConfig wires the multi-guild surface: the settings a page
// loads and saves, and the list of servers the picker offers. Split from
// SubscribeSetup so neither function carries every verb.
func subscribeGuildConfig(d *discordRPC, wire SetupWiring) error {
	if err := bus.QueueSubscribeJSON[outgressrpc.DiscordConfigGetRequest, outgressrpc.DiscordConfigGetReply](
		wire.NC, wire.Prefix+".discord.config.get", wire.Queue, configHandleTimeout, wire.App, wire.Log, d.handleConfigGet); err != nil {
		return err
	}
	if err := bus.QueueSubscribeJSON[outgressrpc.DiscordConfigSetRequest, outgressrpc.DiscordConfigSetReply](
		wire.NC, wire.Prefix+".discord.config.set", wire.Queue, configHandleTimeout, wire.App, wire.Log, d.handleConfigSet); err != nil {
		return err
	}
	return bus.QueueSubscribeJSON[outgressrpc.DiscordGuildsListRequest, outgressrpc.DiscordGuildsListReply](
		wire.NC, wire.Prefix+".discord.guilds.list", wire.Queue, guildsHandleTimeout, wire.App, wire.Log, d.handleGuildsList)
}

func (d *discordRPC) handleConfigGet(ctx context.Context, req outgressrpc.DiscordConfigGetRequest) outgressrpc.DiscordConfigGetReply {
	if req.GuildID == "" || req.UserID == "" {
		return outgressrpc.DiscordConfigGetReply{Error: "missing guild_id or user_id", Code: outgressrpc.DiscordCodeInvalid}
	}
	cfg, version, found, err := d.w.GuildConfig(ctx, setup.GuildSetupRequest{GuildID: req.GuildID, BroadcasterID: req.UserID})
	if err != nil {
		message, code := discordFailure(err)
		return outgressrpc.DiscordConfigGetReply{Error: message, Code: code}
	}
	return outgressrpc.DiscordConfigGetReply{Config: cfg, Version: version, Found: found}
}

func (d *discordRPC) handleConfigSet(ctx context.Context, req outgressrpc.DiscordConfigSetRequest) outgressrpc.DiscordConfigSetReply {
	if req.GuildID == "" || req.UserID == "" {
		return outgressrpc.DiscordConfigSetReply{Error: "missing guild_id or user_id", Code: outgressrpc.DiscordCodeInvalid}
	}
	version, err := d.w.SetGuildConfig(ctx, setup.GuildConfigWrite{
		GuildID:         req.GuildID,
		BroadcasterID:   req.UserID,
		Config:          req.Config,
		ExpectedVersion: req.ExpectedVersion,
	})
	if err != nil {
		message, code := discordFailure(err)
		return outgressrpc.DiscordConfigSetReply{Error: message, Code: code}
	}
	return outgressrpc.DiscordConfigSetReply{Version: version}
}

func (d *discordRPC) handleGuildsList(ctx context.Context, req outgressrpc.DiscordGuildsListRequest) outgressrpc.DiscordGuildsListReply {
	if req.UserID == "" {
		return outgressrpc.DiscordGuildsListReply{Error: "missing user_id", Code: outgressrpc.DiscordCodeInvalid}
	}
	guilds, err := d.w.ListGuilds(ctx, req.UserID)
	if err != nil {
		message, code := discordFailure(err)
		return outgressrpc.DiscordGuildsListReply{Error: message, Code: code}
	}
	out := make([]outgressrpc.DiscordGuildEntry, 0, len(guilds))
	for _, g := range guilds {
		out = append(out, outgressrpc.DiscordGuildEntry{GuildID: g.GuildID, Name: g.Name, BotPresent: g.BotPresent})
	}
	return outgressrpc.DiscordGuildsListReply{Guilds: out}
}

// discordFailure maps an error onto the (message, code) pair the console
// switches on. The message is kept for the one release during which the
// console still reads text.
func discordFailure(err error) (string, string) {
	switch {
	case err == nil:
		return "", outgressrpc.DiscordCodeOK
	case errors.Is(err, setup.ErrNotBound):
		return err.Error(), outgressrpc.DiscordCodeNotBound
	case errors.Is(err, setup.ErrGuildBoundElsewhere):
		return err.Error(), outgressrpc.DiscordCodeBoundElsewhere
	case errors.Is(err, discordstore.ErrConfigConflict):
		return err.Error(), outgressrpc.DiscordCodeConflict
	case errors.Is(err, discordstore.ErrNotBound):
		return err.Error(), outgressrpc.DiscordCodeNotBound
	default:
		return err.Error(), outgressrpc.DiscordCodeUnavailable
	}
}
