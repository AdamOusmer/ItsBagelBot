// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordrate

import (
	"context"

	"ItsBagelBot/internal/discordapi"
)

// rest is the subset of *discordapi.Client the Discord services call
// through (app/discord/ingress for the interaction defer only;
// app/discord/outgress for everything else). LimitedClient wraps it so
// every method pays the shared bucket before delegating -- one choke point
// instead of a call site remembering to pay it. It is an interface (rather
// than *discordapi.Client directly) only so tests can inject a recorder
// without a live Discord endpoint.
type rest interface {
	SendChat(ctx context.Context, post discordapi.ChatPost) error
	SendEmbed(ctx context.Context, post discordapi.EmbedPost) (discordapi.Message, error)
	SendPanel(ctx context.Context, post discordapi.EmbedPost, buttons []discordapi.Button) (discordapi.Message, error)
	EditMessage(ctx context.Context, m discordapi.Message, patch discordapi.MessagePatch) error
	DeleteMessage(ctx context.Context, m discordapi.Message) error
	CreateChannel(ctx context.Context, ch discordapi.GuildChannel) (discordapi.Snowflake, error)
	DeleteChannel(ctx context.Context, ch discordapi.Snowflake) error
	CreateRole(ctx context.Context, role discordapi.GuildRole) (discordapi.Snowflake, error)
	AddMemberRole(ctx context.Context, r discordapi.MemberRole) error
	ModifyCurrentMember(ctx context.Context, m discordapi.CurrentMember) error
	RemoveMemberRole(ctx context.Context, r discordapi.MemberRole) error
	RemoveMemberRoleWithReason(ctx context.Context, r discordapi.MemberRole, reason string) error
	MoveMember(ctx context.Context, move discordapi.VoiceMove) error
	ModifyChannel(ctx context.Context, patch discordapi.ChannelPatch) error
	TimeoutMember(ctx context.Context, t discordapi.MemberTimeout) error
	KickMember(ctx context.Context, m discordapi.GuildMember) error
	BanMember(ctx context.Context, m discordapi.GuildMember) error
	BulkDeleteMessages(ctx context.Context, p discordapi.Purge) error
	ListMessages(ctx context.Context, q discordapi.MessageQuery) ([]discordapi.Snowflake, error)
	ListGuildChannels(ctx context.Context, guild discordapi.Guild) ([]discordapi.Snowflake, error)
	ListGuildRoles(ctx context.Context, guild discordapi.Guild) ([]discordapi.Snowflake, error)
	GetGuild(ctx context.Context, guild discordapi.Guild) (discordapi.Snowflake, error)
	GetGuildWithCounts(ctx context.Context, guild discordapi.Guild) (discordapi.GuildInfo, error)
	InteractionCallback(ctx context.Context, cb discordapi.Callback) error
	InteractionFollowup(ctx context.Context, f discordapi.Followup) error
	BulkOverwriteCommands(ctx context.Context, cat discordapi.CommandCatalog) error
	GetCurrentApplication(ctx context.Context) (discordapi.Snowflake, error)
	GetInvite(ctx context.Context, code string) (discordapi.Invite, error)
	GetGuildMember(ctx context.Context, m discordapi.GuildMember) (discordapi.GuildMemberInfo, error)
	ListGuildChannelsFull(ctx context.Context, guild discordapi.Guild) ([]discordapi.ChannelInfo, error)
	ModifyGuild(ctx context.Context, patch discordapi.GuildPatch) error
	SetChannelOverwrite(ctx context.Context, o discordapi.ChannelOverwrite) error
	ListMessagesFull(ctx context.Context, page discordapi.MessagePage) ([]discordapi.FullMessage, error)
	SendFile(ctx context.Context, up discordapi.FileUpload) (discordapi.Message, error)
}

// LimitedClient is the Decorator over a Discord REST client: same method set
// as rest, with the shared global bucket paid on the way through. app/discord/ingress (the interaction defer) and
// app/discord/outgress (welcomes, tickets, go-live embeds, guild fills,
// interaction followups) each send through one of these, so every call
// either service makes pays the same fleet-wide token before it reaches
// Discord.
type LimitedClient struct {
	rest rest
	gate Gate
}

// NewLimitedClient wraps client so every call pays gate first.
func NewLimitedClient(client rest, gate Gate) *LimitedClient {
	return &LimitedClient{rest: client, gate: gate}
}

// gated0 and gated1 are the whole decoration: pay the gate, then delegate.
// Every method below is one of the two, so "pay before you send" is written
// once instead of once per method -- the shape this file used to have, where
// the rule was 34 copies of the same three lines and a new method could
// silently ship without them. gated1 exists separately only because Go has no
// way to spell the zero value of a method's result type without naming it.
//
// The delegate is a closure rather than a bound method plus its arguments
// (gated1(ctx, c.gate, arg, c.rest.SendEmbed)) because two methods here take
// two arguments and would need a second five-parameter helper: one shape for
// all 34 beats two shapes that differ only in arity.
func gated0(ctx context.Context, gate Gate, call func(context.Context) error) error {
	if err := gate.Take(ctx); err != nil {
		return err
	}
	return call(ctx)
}

func gated1[T any](ctx context.Context, gate Gate, call func(context.Context) (T, error)) (T, error) {
	if err := gate.Take(ctx); err != nil {
		var zero T
		return zero, err
	}
	return call(ctx)
}

func (c *LimitedClient) SendChat(ctx context.Context, post discordapi.ChatPost) error {
	return gated0(ctx, c.gate, func(ctx context.Context) error { return c.rest.SendChat(ctx, post) })
}

func (c *LimitedClient) SendEmbed(ctx context.Context, post discordapi.EmbedPost) (discordapi.Message, error) {
	return gated1(ctx, c.gate, func(ctx context.Context) (discordapi.Message, error) { return c.rest.SendEmbed(ctx, post) })
}

func (c *LimitedClient) SendPanel(ctx context.Context, post discordapi.EmbedPost, buttons []discordapi.Button) (discordapi.Message, error) {
	return gated1(ctx, c.gate, func(ctx context.Context) (discordapi.Message, error) { return c.rest.SendPanel(ctx, post, buttons) })
}

func (c *LimitedClient) EditMessage(ctx context.Context, m discordapi.Message, patch discordapi.MessagePatch) error {
	return gated0(ctx, c.gate, func(ctx context.Context) error { return c.rest.EditMessage(ctx, m, patch) })
}

func (c *LimitedClient) DeleteMessage(ctx context.Context, m discordapi.Message) error {
	return gated0(ctx, c.gate, func(ctx context.Context) error { return c.rest.DeleteMessage(ctx, m) })
}

func (c *LimitedClient) CreateChannel(ctx context.Context, ch discordapi.GuildChannel) (discordapi.Snowflake, error) {
	return gated1(ctx, c.gate, func(ctx context.Context) (discordapi.Snowflake, error) { return c.rest.CreateChannel(ctx, ch) })
}

func (c *LimitedClient) DeleteChannel(ctx context.Context, ch discordapi.Snowflake) error {
	return gated0(ctx, c.gate, func(ctx context.Context) error { return c.rest.DeleteChannel(ctx, ch) })
}

func (c *LimitedClient) CreateRole(ctx context.Context, role discordapi.GuildRole) (discordapi.Snowflake, error) {
	return gated1(ctx, c.gate, func(ctx context.Context) (discordapi.Snowflake, error) { return c.rest.CreateRole(ctx, role) })
}

func (c *LimitedClient) AddMemberRole(ctx context.Context, r discordapi.MemberRole) error {
	return gated0(ctx, c.gate, func(ctx context.Context) error { return c.rest.AddMemberRole(ctx, r) })
}

func (c *LimitedClient) RemoveMemberRole(ctx context.Context, r discordapi.MemberRole) error {
	return gated0(ctx, c.gate, func(ctx context.Context) error { return c.rest.RemoveMemberRole(ctx, r) })
}

func (c *LimitedClient) MoveMember(ctx context.Context, move discordapi.VoiceMove) error {
	return gated0(ctx, c.gate, func(ctx context.Context) error { return c.rest.MoveMember(ctx, move) })
}

func (c *LimitedClient) ModifyChannel(ctx context.Context, patch discordapi.ChannelPatch) error {
	return gated0(ctx, c.gate, func(ctx context.Context) error { return c.rest.ModifyChannel(ctx, patch) })
}

func (c *LimitedClient) TimeoutMember(ctx context.Context, t discordapi.MemberTimeout) error {
	return gated0(ctx, c.gate, func(ctx context.Context) error { return c.rest.TimeoutMember(ctx, t) })
}

func (c *LimitedClient) KickMember(ctx context.Context, m discordapi.GuildMember) error {
	return gated0(ctx, c.gate, func(ctx context.Context) error { return c.rest.KickMember(ctx, m) })
}

func (c *LimitedClient) BanMember(ctx context.Context, m discordapi.GuildMember) error {
	return gated0(ctx, c.gate, func(ctx context.Context) error { return c.rest.BanMember(ctx, m) })
}

func (c *LimitedClient) BulkDeleteMessages(ctx context.Context, p discordapi.Purge) error {
	return gated0(ctx, c.gate, func(ctx context.Context) error { return c.rest.BulkDeleteMessages(ctx, p) })
}

func (c *LimitedClient) ListMessages(ctx context.Context, q discordapi.MessageQuery) ([]discordapi.Snowflake, error) {
	return gated1(ctx, c.gate, func(ctx context.Context) ([]discordapi.Snowflake, error) { return c.rest.ListMessages(ctx, q) })
}

func (c *LimitedClient) ListGuildChannels(ctx context.Context, guild discordapi.Guild) ([]discordapi.Snowflake, error) {
	return gated1(ctx, c.gate, func(ctx context.Context) ([]discordapi.Snowflake, error) { return c.rest.ListGuildChannels(ctx, guild) })
}

func (c *LimitedClient) ListGuildRoles(ctx context.Context, guild discordapi.Guild) ([]discordapi.Snowflake, error) {
	return gated1(ctx, c.gate, func(ctx context.Context) ([]discordapi.Snowflake, error) { return c.rest.ListGuildRoles(ctx, guild) })
}

func (c *LimitedClient) GetGuild(ctx context.Context, guild discordapi.Guild) (discordapi.Snowflake, error) {
	return gated1(ctx, c.gate, func(ctx context.Context) (discordapi.Snowflake, error) { return c.rest.GetGuild(ctx, guild) })
}

func (c *LimitedClient) GetGuildWithCounts(ctx context.Context, guild discordapi.Guild) (discordapi.GuildInfo, error) {
	return gated1(ctx, c.gate, func(ctx context.Context) (discordapi.GuildInfo, error) { return c.rest.GetGuildWithCounts(ctx, guild) })
}

func (c *LimitedClient) InteractionCallback(ctx context.Context, cb discordapi.Callback) error {
	return gated0(ctx, c.gate, func(ctx context.Context) error { return c.rest.InteractionCallback(ctx, cb) })
}

func (c *LimitedClient) BulkOverwriteCommands(ctx context.Context, cat discordapi.CommandCatalog) error {
	return gated0(ctx, c.gate, func(ctx context.Context) error { return c.rest.BulkOverwriteCommands(ctx, cat) })
}

func (c *LimitedClient) InteractionFollowup(ctx context.Context, f discordapi.Followup) error {
	return gated0(ctx, c.gate, func(ctx context.Context) error { return c.rest.InteractionFollowup(ctx, f) })
}

func (c *LimitedClient) GetCurrentApplication(ctx context.Context) (discordapi.Snowflake, error) {
	return gated1(ctx, c.gate, func(ctx context.Context) (discordapi.Snowflake, error) { return c.rest.GetCurrentApplication(ctx) })
}

func (c *LimitedClient) GetInvite(ctx context.Context, code string) (discordapi.Invite, error) {
	return gated1(ctx, c.gate, func(ctx context.Context) (discordapi.Invite, error) { return c.rest.GetInvite(ctx, code) })
}

// ModifyCurrentMember sets the bot's per-guild nickname and avatar. It pays
// the shared bucket like every other call: a premium avatar upload is ~86 KB
// and a fleet-wide reconnect can queue one per guild, which is exactly the
// burst the global per-token limit exists to absorb.
func (c *LimitedClient) ModifyCurrentMember(ctx context.Context, m discordapi.CurrentMember) error {
	return gated0(ctx, c.gate, func(ctx context.Context) error { return c.rest.ModifyCurrentMember(ctx, m) })
}

func (c *LimitedClient) GetGuildMember(ctx context.Context, m discordapi.GuildMember) (discordapi.GuildMemberInfo, error) {
	return gated1(ctx, c.gate, func(ctx context.Context) (discordapi.GuildMemberInfo, error) { return c.rest.GetGuildMember(ctx, m) })
}

func (c *LimitedClient) ListGuildChannelsFull(ctx context.Context, guild discordapi.Guild) ([]discordapi.ChannelInfo, error) {
	return gated1(ctx, c.gate, func(ctx context.Context) ([]discordapi.ChannelInfo, error) {
		return c.rest.ListGuildChannelsFull(ctx, guild)
	})
}

func (c *LimitedClient) ModifyGuild(ctx context.Context, patch discordapi.GuildPatch) error {
	return gated0(ctx, c.gate, func(ctx context.Context) error { return c.rest.ModifyGuild(ctx, patch) })
}

func (c *LimitedClient) SetChannelOverwrite(ctx context.Context, o discordapi.ChannelOverwrite) error {
	return gated0(ctx, c.gate, func(ctx context.Context) error { return c.rest.SetChannelOverwrite(ctx, o) })
}

// RemoveMemberRoleWithReason pays the bucket like its reasonless twin. A raid
// strip is a burst of these, which is exactly what the shared token exists to
// pace.
func (c *LimitedClient) RemoveMemberRoleWithReason(ctx context.Context, r discordapi.MemberRole, reason string) error {
	return gated0(ctx, c.gate, func(ctx context.Context) error { return c.rest.RemoveMemberRoleWithReason(ctx, r, reason) })
}

// ListMessagesFull pays one token per PAGE, not per message: the transcript
// path calls this up to twenty times for one closing ticket, and each call is
// one request against Discord's bucket.
func (c *LimitedClient) ListMessagesFull(ctx context.Context, page discordapi.MessagePage) ([]discordapi.FullMessage, error) {
	return gated1(ctx, c.gate, func(ctx context.Context) ([]discordapi.FullMessage, error) { return c.rest.ListMessagesFull(ctx, page) })
}

func (c *LimitedClient) SendFile(ctx context.Context, up discordapi.FileUpload) (discordapi.Message, error) {
	return gated1(ctx, c.gate, func(ctx context.Context) (discordapi.Message, error) { return c.rest.SendFile(ctx, up) })
}
