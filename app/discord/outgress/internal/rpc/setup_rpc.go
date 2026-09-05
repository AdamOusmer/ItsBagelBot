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
	discapi "ItsBagelBot/internal/discordapi"
	ddiscord "ItsBagelBot/internal/domain/discord"
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

// statusHandleTimeout bounds the status handler: one Valkey read plus one
// GetGuildWithCounts. Tight on purpose -- the dashboard polls it while a
// page is open, and a status pill that hangs is worse than one that says
// "unknown".
const statusHandleTimeout = 3 * time.Second

// deskRepostTimeout bounds the repost: one Valkey read, one message delete and
// one panel post. Ten seconds rather than handleTimeout's 1.5s because two of
// the three are REST calls that can each sit behind a Retry-After.
const deskRepostTimeout = 10 * time.Second

// handleTimeout bounds the unbind and post handlers: one Valkey round trip
// (unbind) or one REST call (post).
const handleTimeout = 1500 * time.Millisecond

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
	// Status reads the gateway status key discord-ingress publishes.
	// Optional; nil reports the bot as offline with no close code, which is
	// honest -- outgress genuinely does not know.
	Status botStatusReader
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
	d := &discordRPC{w: w, reauth: wire.Reauth, status: wire.Status, log: wire.Log}
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
	if err := bus.QueueSubscribeJSON[outgressrpc.DiscordStatusRequest, outgressrpc.DiscordStatusReply](
		wire.NC, wire.Prefix+".discord.status", wire.Queue, statusHandleTimeout, wire.App, wire.Log, d.handleStatus); err != nil {
		return err
	}
	if err := bus.QueueSubscribeJSON[outgressrpc.DiscordDeskRepostRequest, outgressrpc.DiscordDeskRepostReply](
		wire.NC, wire.Prefix+".discord.desk.repost", wire.Queue, deskRepostTimeout, wire.App, wire.Log, d.handleDeskRepost); err != nil {
		return err
	}
	return bus.QueueSubscribeJSON[outgressrpc.DiscordPostRequest, outgressrpc.DiscordPostReply](
		wire.NC, wire.Prefix+".discord.post", wire.Queue, handleTimeout, wire.App, wire.Log, d.handlePost)
}

// handleDeskRepost replaces the guild's ticket panel with one rendered from
// the copy the dashboard just saved.
func (d *discordRPC) handleDeskRepost(ctx context.Context, req outgressrpc.DiscordDeskRepostRequest) outgressrpc.DiscordDeskRepostReply {
	if req.GuildID == "" || req.UserID == "" {
		return outgressrpc.DiscordDeskRepostReply{Error: "missing guild_id or user_id", Code: outgressrpc.CodeInvalid}
	}
	id, err := d.w.RepostDesk(ctx, setup.DeskRepostRequest{
		GuildID: req.GuildID, BroadcasterID: req.UserID, ChannelID: req.ChannelID,
		Panel: panelSpec(req.Panel),
	})
	if err != nil {
		return outgressrpc.DiscordDeskRepostReply{Error: err.Error(), Code: codeFor(err)}
	}
	return outgressrpc.DiscordDeskRepostReply{MessageID: id}
}

func panelSpec(in outgressrpc.DiscordPanelSpec) ddiscord.TicketPanelSpec {
	return ddiscord.TicketPanelSpec{Title: in.Title, Body: in.Body, Color: in.Color, Button: in.Button}
}

type discordRPC struct {
	w      *setup.Worker
	reauth reauthReader
	status botStatusReader
	log    *zap.Logger
}

// botStatusReader is the read slice of kv.BotStatusReader (see that type for
// why outgress never writes the key).
type botStatusReader interface {
	BotStatus(ctx context.Context) (ddiscord.BotStatus, bool)
}

// codeFor maps an error to the reply code the console switches on. The
// message is still returned alongside it for one release; this is what
// replaces the console matching substrings of that message.
//
// An unrecognised error deliberately maps to CodeOK rather than to a
// catch-all: a wrong code makes the console render a confident, wrong
// explanation, while no code makes it fall back to showing the message,
// which is at least true.
func codeFor(err error) string {
	switch {
	case err == nil:
		return outgressrpc.CodeOK
	case errors.Is(err, setup.ErrGuildNotBound):
		return outgressrpc.CodeNotBound
	case errors.Is(err, setup.ErrGuildBoundElsewhere):
		return outgressrpc.CodeBoundElsewhere
	case errors.Is(err, setup.ErrDiscordUnavailable), errors.Is(err, discapi.ErrAuth):
		return outgressrpc.CodeDiscordUnavailable
	case errors.Is(err, discapi.ErrForbidden):
		return outgressrpc.CodeForbidden
	case errors.Is(err, discapi.ErrRateLimited):
		return outgressrpc.CodeRateLimited
	case errors.Is(err, discapi.ErrBadRequest):
		return outgressrpc.CodeInvalid
	default:
		return outgressrpc.CodeOK
	}
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
		return outgressrpc.DiscordSetupReply{Error: "missing guild_id or user_id", Code: outgressrpc.CodeInvalid}
	}
	got, err := d.w.SetupGuild(ctx, setup.GuildSetupRequest{
		GuildID: req.GuildID, BroadcasterID: req.UserID,
		Subscribers: req.Subscribers, PinnedRoles: req.PinnedRoles,
	})
	if err != nil {
		return outgressrpc.DiscordSetupReply{Error: err.Error(), Code: codeFor(err)}
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

		TicketArchiveCategoryID: got.TicketArchiveCategoryID,

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
		return outgressrpc.DiscordLayoutReply{Error: "missing guild_id or user_id", Code: outgressrpc.CodeInvalid}
	}
	layout, err := d.w.GuildLayout(ctx, setup.GuildSetupRequest{GuildID: req.GuildID, BroadcasterID: req.UserID})
	if err != nil {
		return outgressrpc.DiscordLayoutReply{Error: err.Error(), Code: codeFor(err)}
	}
	reply := outgressrpc.DiscordLayoutReply{
		Channels:    layoutEntries(layout.Channels, notCategory),
		Categories:  layoutEntries(layout.Channels, isCategory),
		Roles:       layoutEntries(layout.Roles, everything),
		NeedsReauth: d.needsReauth(ctx, kv.GuildID(req.GuildID)),
	}
	d.fillBotFields(ctx, &reply)
	reply.Guild = d.guildInfo(ctx, req.GuildID, req.UserID)
	return reply
}

// Channel-kind predicates. Categories are split out of Channels here rather
// than in the console so nothing outside this package has to carry a copy of
// Discord's channel-type numbering (see discord.ChannelCategory).
func isCategory(e setup.GuildEntry) bool  { return e.Type == ddiscord.ChannelCategory }
func notCategory(e setup.GuildEntry) bool { return e.Type != ddiscord.ChannelCategory }
func everything(setup.GuildEntry) bool    { return true }

func layoutEntries(in []setup.GuildEntry, keep func(setup.GuildEntry) bool) []outgressrpc.DiscordLayoutEntry {
	out := make([]outgressrpc.DiscordLayoutEntry, 0, len(in))
	for _, e := range in {
		if !keep(e) {
			continue
		}
		out = append(out, outgressrpc.DiscordLayoutEntry{ID: e.ID, Name: e.Name, Type: e.Type})
	}
	return out
}

// fillBotFields copies the gateway status onto a layout reply. Best effort:
// the pickers are the point of the layout call, and a missing status key
// must not cost the streamer their channel list.
func (d *discordRPC) fillBotFields(ctx context.Context, reply *outgressrpc.DiscordLayoutReply) {
	st, ok := d.botStatus(ctx)
	if !ok {
		return
	}
	reply.BotOnline = st.Connected
	reply.BotSinceUnixMS = st.SinceUnixMS
	reply.LastCloseCode = st.LastCloseCode
}

// guildInfo fetches the server card, or nil when Discord refuses. Nil rather
// than an error on the reply: the guild lookup and the channel listing fail
// independently, and one failing is not a reason to drop the other.
func (d *discordRPC) guildInfo(ctx context.Context, guildID, userID string) *outgressrpc.DiscordGuildInfo {
	got, err := d.w.GuildInfo(ctx, setup.GuildSetupRequest{GuildID: guildID, BroadcasterID: userID})
	if err != nil {
		d.logger().Warn("discord guild info failed", zap.String("guild_id", guildID), zap.Error(err))
		return nil
	}
	return &outgressrpc.DiscordGuildInfo{
		ID: got.ID, Name: got.Name, IconURL: got.IconURL(), MemberCount: got.ApproximateMemberCount,
	}
}

// handleStatus answers the dashboard's bot pill. The gateway session and
// this guild's membership are independent facts and fail independently: a
// bot that is online but was kicked from the server reports online with
// guild_present false, which the old dashboard could not tell apart from a
// bot that was down.
func (d *discordRPC) handleStatus(ctx context.Context, req outgressrpc.DiscordStatusRequest) outgressrpc.DiscordStatusReply {
	if req.GuildID == "" || req.UserID == "" {
		return outgressrpc.DiscordStatusReply{Error: "missing guild_id or user_id", Code: outgressrpc.CodeInvalid}
	}
	st, _ := d.botStatus(ctx)
	reply := outgressrpc.DiscordStatusReply{
		Online:         st.Connected,
		SinceUnixMS:    st.SinceUnixMS,
		SessionResumes: st.Resumes,
		LastCloseCode:  st.LastCloseCode,
		NeedsReauth:    d.needsReauth(ctx, kv.GuildID(req.GuildID)),
	}
	got, err := d.w.GuildInfo(ctx, setup.GuildSetupRequest{GuildID: req.GuildID, BroadcasterID: req.UserID})
	if err != nil {
		reply.Error, reply.Code = err.Error(), codeFor(err)
		return reply
	}
	reply.GuildPresent = true
	reply.GuildName, reply.IconURL, reply.MemberCount = got.Name, got.IconURL(), got.ApproximateMemberCount
	return reply
}

// botStatus reads the published gateway status. A nil reader (no Valkey
// wired, as in tests) reports not-ok rather than a zero status, so callers
// can tell "the bot is offline" from "we do not know".
func (d *discordRPC) botStatus(ctx context.Context) (ddiscord.BotStatus, bool) {
	if d.status == nil {
		return ddiscord.BotStatus{}, false
	}
	return d.status.BotStatus(ctx)
}

func (d *discordRPC) logger() *zap.Logger {
	if d.log == nil {
		return zap.NewNop()
	}
	return d.log
}

func (d *discordRPC) handleUnbind(ctx context.Context, req outgressrpc.DiscordUnbindRequest) outgressrpc.DiscordUnbindReply {
	if req.GuildID == "" || req.UserID == "" {
		return outgressrpc.DiscordUnbindReply{Error: "missing guild_id or user_id", Code: outgressrpc.CodeInvalid}
	}
	if err := d.w.UnbindGuild(ctx, setup.GuildSetupRequest{GuildID: req.GuildID, BroadcasterID: req.UserID}); err != nil {
		return outgressrpc.DiscordUnbindReply{Error: err.Error(), Code: codeFor(err)}
	}
	return outgressrpc.DiscordUnbindReply{}
}

func (d *discordRPC) handlePost(ctx context.Context, req outgressrpc.DiscordPostRequest) outgressrpc.DiscordPostReply {
	if req.ChannelID == "" || req.Content == "" {
		return outgressrpc.DiscordPostReply{Error: "missing channel or content", Code: outgressrpc.CodeInvalid}
	}
	if err := d.w.PostDiscord(ctx, req.ChannelID, req.Content); err != nil {
		return outgressrpc.DiscordPostReply{Error: err.Error(), Code: codeFor(err)}
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
