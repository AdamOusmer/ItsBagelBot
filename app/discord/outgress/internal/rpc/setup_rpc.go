// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"ItsBagelBot/internal/domain/rpc"
	"context"
	"errors"
	"strings"
	"time"

	"ItsBagelBot/app/discord/outgress/internal/kv"
	"ItsBagelBot/app/discord/outgress/internal/setup"
	discapi "ItsBagelBot/internal/discordapi"
	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"
	outgressrpc "ItsBagelBot/internal/domain/rpc/outgress"

	"go.uber.org/zap"
)

const setupHandleTimeout = 45 * time.Second

const layoutHandleTimeout = 10 * time.Second

const statusHandleTimeout = 2500 * time.Millisecond

const deskRepostTimeout = 8 * time.Second

const handleTimeout = 1500 * time.Millisecond

const configGetHandleTimeout = 1500 * time.Millisecond

const configHandleTimeout = 3 * time.Second

const guildsHandleTimeout = 7 * time.Second

type SetupWiring struct {
	Wiring
	Reauth reauthReader
	Status botStatusReader
}

func SubscribeSetup(w *setup.Worker, wire SetupWiring) error {
	if w == nil {
		return nil
	}
	d := &discordRPC{w: w, reauth: wire.Reauth, status: wire.Status, log: wire.Log}
	if err := errors.Join(
		register[outgressrpc.DiscordSetupRequest, outgressrpc.DiscordSetupReply](
			wire.Wiring, verb{Name: "discord.setup", Timeout: setupHandleTimeout}, d.handleSetup),
		register[outgressrpc.DiscordLayoutRequest, outgressrpc.DiscordLayoutReply](
			wire.Wiring, verb{Name: "discord.layout", Timeout: layoutHandleTimeout}, d.handleLayout),
		register[outgressrpc.DiscordUnbindRequest, outgressrpc.DiscordUnbindReply](
			wire.Wiring, verb{Name: "discord.unbind", Timeout: handleTimeout}, d.handleUnbind),
		register[outgressrpc.DiscordStatusRequest, outgressrpc.DiscordStatusReply](
			wire.Wiring, verb{Name: "discord.status", Timeout: statusHandleTimeout}, d.handleStatus),
		register[outgressrpc.DiscordDeskRepostRequest, outgressrpc.DiscordDeskRepostReply](
			wire.Wiring, verb{Name: "discord.desk.repost", Timeout: deskRepostTimeout}, d.handleDeskRepost),
		register[outgressrpc.DiscordPostRequest, outgressrpc.DiscordPostReply](
			wire.Wiring, verb{Name: "discord.post", Timeout: handleTimeout}, d.handlePost),
	); err != nil {
		return err
	}
	return subscribeGuildConfig(d, wire)
}

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

type botStatusReader interface {
	BotStatus(ctx context.Context) (ddiscord.BotStatus, bool)
}

func codeFor(err error) rpc.Code {
	switch {
	case err == nil:
		return outgressrpc.CodeOK
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return outgressrpc.CodeTimeout
	}
	if code := bindingCode(err); code != "" {
		return code
	}
	if code := discordCode(err); code != "" {
		return code
	}
	return outgressrpc.CodeUnknown
}

func bindingCode(err error) rpc.Code {
	switch {
	case errors.Is(err, setup.ErrGuildNotBound), errors.Is(err, setup.ErrNotBound),
		errors.Is(err, discordstore.ErrNotBound):
		return outgressrpc.CodeNotBound
	case errors.Is(err, setup.ErrGuildBoundElsewhere), errors.Is(err, discordstore.ErrBoundElsewhere):
		return outgressrpc.CodeBoundElsewhere
	case errors.Is(err, discordstore.ErrConfigConflict):
		return outgressrpc.CodeConflict
	case errors.Is(err, discordstore.ErrStoreUnavailable), errors.Is(err, discordstore.ErrConfigUnavailable):
		return outgressrpc.CodeDiscordUnavailable
	}
	return ""
}

func discordCode(err error) rpc.Code {
	switch {
	case errors.Is(err, setup.ErrDiscordUnavailable), errors.Is(err, discapi.ErrAuth):
		return outgressrpc.CodeDiscordUnavailable
	case errors.Is(err, discapi.ErrForbidden):
		return outgressrpc.CodeForbidden
	case errors.Is(err, discapi.ErrRateLimited):
		return outgressrpc.CodeRateLimited
	case errors.Is(err, discapi.ErrChannelNotFound):
		return outgressrpc.CodeNotFound
	case errors.Is(err, discapi.ErrBadRequest):
		return outgressrpc.CodeInvalid
	}
	return ""
}

type reauthReader interface {
	NeedsReauth(ctx context.Context, guildID kv.GuildID) bool
}

func (d *discordRPC) handleSetup(ctx context.Context, req outgressrpc.DiscordSetupRequest) outgressrpc.DiscordSetupReply {
	if req.GuildID == "" || req.UserID == "" {
		return outgressrpc.DiscordSetupReply{Error: "missing guild_id or user_id", Code: outgressrpc.CodeInvalid}
	}
	if bad := validateSetup(req); len(bad) > 0 {
		return outgressrpc.DiscordSetupReply{
			Code: outgressrpc.CodeInvalid, Fields: bad,
			Error: "some settings are not valid: " + strings.Join(bad, ", "),
		}
	}
	got, err := d.w.SetupGuild(ctx, setup.GuildSetupRequest{
		GuildID:       req.GuildID,
		BroadcasterID: req.UserID,
		Subscribers:   req.Subscribers,
		PinnedRoles:   req.PinnedRoles,
		InstalledBy:   req.InstalledBy,
	})
	if err != nil {
		return outgressrpc.DiscordSetupReply{Error: err.Error(), Code: codeFor(err)}
	}
	return outgressrpc.DiscordSetupReply{
		DroppedPins:      got.DroppedPins,
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

func validateSetup(req outgressrpc.DiscordSetupRequest) []string {
	cfg := ddiscord.Config{
		GuildID:     req.GuildID,
		PinnedRoles: ddiscord.FormatPinnedRoles(req.PinnedRoles),
	}
	var out []string
	seen := map[string]bool{}
	for _, fe := range ddiscord.ValidateConfig(cfg) {
		if seen[fe.Field] {
			continue
		}
		seen[fe.Field] = true
		out = append(out, fe.Field)
	}
	return out
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

func (d *discordRPC) fillBotFields(ctx context.Context, reply *outgressrpc.DiscordLayoutReply) {
	st, ok := d.botStatus(ctx)
	if !ok {
		return
	}
	reply.BotOnline = botOnline(st, time.Now())
	reply.BotSinceUnixMS = st.SinceUnixMS
	reply.LastCloseCode = st.LastCloseCode
}

func botOnline(st ddiscord.BotStatus, now time.Time) bool {
	return st.Connected && !st.HeartbeatStale(now)
}

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

func (d *discordRPC) handleStatus(ctx context.Context, req outgressrpc.DiscordStatusRequest) outgressrpc.DiscordStatusReply {
	if req.GuildID == "" || req.UserID == "" {
		return outgressrpc.DiscordStatusReply{Error: "missing guild_id or user_id", Code: outgressrpc.CodeInvalid}
	}
	st, _ := d.botStatus(ctx)
	reply := outgressrpc.DiscordStatusReply{
		Online:         botOnline(st, time.Now()),
		SinceUnixMS:    st.SinceUnixMS,
		SessionResumes: st.Resumes,
		LastCloseCode:  st.LastCloseCode,
		NeedsReauth:    d.needsReauth(ctx, kv.GuildID(req.GuildID)),
	}
	withBudget(&reply, st)
	got, err := d.w.GuildInfo(ctx, setup.GuildSetupRequest{GuildID: req.GuildID, BroadcasterID: req.UserID})
	if err != nil {
		reply.Error, reply.Code = err.Error(), codeFor(err)
		return reply
	}
	reply.GuildPresent = true
	reply.GuildName, reply.IconURL, reply.MemberCount = got.Name, got.IconURL(), got.ApproximateMemberCount
	return reply
}

func withBudget(reply *outgressrpc.DiscordStatusReply, st ddiscord.BotStatus) {
	reply.Flapping = st.Flapping
	reply.ConnectsInWindow = st.ConnectsInWindow
	reply.AtCeiling = st.AtCeiling
	reply.ParkUntilUnixMS = st.ParkUntilUnixMS
}

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

func (d *discordRPC) needsReauth(ctx context.Context, guildID kv.GuildID) bool {
	flag, _ := d.reauthFlag(ctx, guildID)
	return flag
}

func (d *discordRPC) reauthFlag(ctx context.Context, guildID kv.GuildID) (flag, known bool) {
	if d.reauth == nil {
		return false, true
	}
	if ctx.Err() != nil {
		return false, false
	}
	return d.reauth.NeedsReauth(ctx, guildID), true
}

func subscribeGuildConfig(d *discordRPC, wire SetupWiring) error {
	return errors.Join(
		register[outgressrpc.DiscordConfigGetRequest, outgressrpc.DiscordConfigGetReply](
			wire.Wiring, verb{Name: "discord.config.get", Timeout: configGetHandleTimeout}, d.handleConfigGet),
		register[outgressrpc.DiscordConfigSetRequest, outgressrpc.DiscordConfigSetReply](
			wire.Wiring, verb{Name: "discord.config.set", Timeout: configHandleTimeout}, d.handleConfigSet),
		register[outgressrpc.DiscordGuildsListRequest, outgressrpc.DiscordGuildsListReply](
			wire.Wiring, verb{Name: "discord.guilds.list", Timeout: guildsHandleTimeout}, d.handleGuildsList),
	)
}

func (d *discordRPC) handleConfigGet(ctx context.Context, req outgressrpc.DiscordConfigGetRequest) outgressrpc.DiscordConfigGetReply {
	if req.GuildID == "" || req.UserID == "" {
		return outgressrpc.DiscordConfigGetReply{Error: "missing guild_id or user_id", Code: outgressrpc.CodeInvalid}
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
		return outgressrpc.DiscordConfigSetReply{Error: "missing guild_id or user_id", Code: outgressrpc.CodeInvalid}
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
		return outgressrpc.DiscordGuildsListReply{Error: "missing user_id", Code: outgressrpc.CodeInvalid}
	}
	listing, err := d.w.ListGuilds(ctx, req.UserID)
	out := d.guildEntries(ctx, listing.Guilds)
	if err != nil {
		message, code := discordFailure(err)
		return outgressrpc.DiscordGuildsListReply{
			Guilds: out, Truncated: listing.Truncated, Error: message, Code: code,
		}
	}
	return outgressrpc.DiscordGuildsListReply{Guilds: out, Truncated: listing.Truncated}
}

func (d *discordRPC) guildEntries(ctx context.Context, guilds []setup.GuildSummary) []outgressrpc.DiscordGuildEntry {
	out := make([]outgressrpc.DiscordGuildEntry, 0, len(guilds))
	for _, g := range guilds {
		flag, known := d.reauthFlag(ctx, kv.GuildID(g.GuildID))
		out = append(out, outgressrpc.DiscordGuildEntry{
			GuildID:       g.GuildID,
			Name:          g.Name,
			IconURL:       g.IconURL,
			MemberCount:   g.MemberCount,
			BotPresent:    g.BotPresent,
			BoundAtUnixMs: g.BoundAtUnixMs,
			NeedsReauth:   flag,
			ReauthUnknown: !known,
		})
	}
	return out
}

func discordFailure(err error) (string, rpc.Code) {
	if err == nil {
		return "", outgressrpc.CodeOK
	}
	return err.Error(), codeFor(err)
}
