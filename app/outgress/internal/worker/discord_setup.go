// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	discapi "ItsBagelBot/app/outgress/internal/discord"
	ddiscord "ItsBagelBot/internal/domain/discord"
)

const (
	overwriteRole          = 0
	permViewChannel  int64 = 1024
	permSendMessages int64 = 2048

	// maxCreateRetries bounds how many Retry-After waits one create pays
	// before the fill gives up; the guild create buckets are tight and a
	// 21-call fill can trip them twice.
	maxCreateRetries = 3
)

// ErrGuildBoundElsewhere refuses a guild already linked to a different
// Twitch channel. It fires before any write, so a caller who can reach the
// RPC cannot re-point someone else's server at their own broadcaster id.
var ErrGuildBoundElsewhere = errors.New("this Discord server is already linked to another Twitch channel")

var errDiscordUnavailable = errors.New("discord client unavailable")

// GuildSetupResult is the snowflakes the dashboard writes into the Discord
// module blob after a fill. Outgress does not write modules.
type GuildSetupResult struct {
	GuildID          string
	LiveChannelID    string
	ClipsChannelID   string
	WelcomeChannelID string
	AlertsChannelID  string
	VoiceHubID       string
	LiveRoleID       string
	ModsRoleID       string
	RegularsRoleID   string
	MemberRoleID     string
	Refused          string // non-empty when the guild looked lived-in
}

// GuildEntry is one channel or role the dashboard can pick from.
type GuildEntry struct {
	ID   string
	Name string
	Type int
}

// GuildLayout is a guild's channels and roles.
type GuildLayout struct {
	Channels []GuildEntry
	Roles    []GuildEntry
}

// GuildSetupRequest names the guild to fill and the broadcaster it binds to.
// EveryoneRoleID may be empty: Discord's @everyone role id equals the guild id.
type GuildSetupRequest struct {
	GuildID        string
	EveryoneRoleID string
	BroadcasterID  string
}

// SetupGuild fills the Bagel community template into an existing guild.
// Proving the caller installed the bot in guildID is the dashboard's job
// (OAuth code exchange); outgress enforces the one thing it can see, that
// the guild is not already bound to another broadcaster, and binds BEFORE
// any write so a refused caller never touches the server.
//
// A lived-in community (enough channels that are not ours) is not rebuilt;
// its channels and roles are adopted by name instead. Template-named
// channels never count as lived-in, so a fill cut short by a timeout is
// completed on the next attempt rather than refused forever.
func (w *Worker) SetupGuild(ctx context.Context, req GuildSetupRequest) (GuildSetupResult, error) {
	req.GuildID = strings.TrimSpace(req.GuildID)
	out := GuildSetupResult{GuildID: req.GuildID}
	fill, err := w.newGuildFill(ctx, req)
	if err != nil {
		return GuildSetupResult{}, err
	}
	if fill.livedIn() {
		out.Refused = "this server already has a layout; Bagel adopted the channels it recognised, pick the rest below"
		fill.adopt(&out)
		return out, nil
	}
	if err := fill.ensureRoles(ctx, &out); err != nil {
		return out, err
	}
	if err := fill.ensureChannels(ctx, &out); err != nil {
		return out, err
	}
	return out, nil
}

// GuildLayout lists a bound guild's channels and roles for the dashboard
// pickers. Only the bound broadcaster may read it.
func (w *Worker) GuildLayout(ctx context.Context, guildID, broadcasterID string) (GuildLayout, error) {
	guild, ok := w.guildAPI()
	if !ok {
		return GuildLayout{}, errDiscordUnavailable
	}
	if err := w.requireBound(ctx, guildID, broadcasterID); err != nil {
		return GuildLayout{}, err
	}
	channels, err := guild.ListGuildChannels(ctx, guildID)
	if err != nil {
		return GuildLayout{}, err
	}
	roles, err := guild.ListGuildRoles(ctx, guildID)
	if err != nil {
		return GuildLayout{}, err
	}
	return GuildLayout{Channels: entries(channels), Roles: entries(roles)}, nil
}

// UnbindGuild drops the guild→broadcaster reverse index on disconnect. A
// guild bound to someone else is left alone.
func (w *Worker) UnbindGuild(ctx context.Context, guildID, broadcasterID string) error {
	if err := w.requireOwner(ctx, guildID, broadcasterID, true); err != nil {
		return err
	}
	if w.discordKV == nil {
		return nil
	}
	return w.discordKV.DeleteGuild(ctx, guildID)
}

func entries(in []discapi.Snowflake) []GuildEntry {
	out := make([]GuildEntry, 0, len(in))
	for _, s := range in {
		out = append(out, GuildEntry{ID: s.ID, Name: s.Name, Type: s.Type})
	}
	return out
}

// bindGuild writes the reverse index unless the guild already belongs to a
// different broadcaster.
func (w *Worker) bindGuild(ctx context.Context, guildID, broadcasterID string) error {
	if w.discordKV == nil {
		return nil
	}
	if guildID == "" {
		return nil
	}
	if broadcasterID == "" {
		return nil
	}
	if err := w.requireOwner(ctx, guildID, broadcasterID, true); err != nil {
		return err
	}
	return w.discordKV.PutGuild(ctx, guildID, broadcasterID)
}

func (w *Worker) requireBound(ctx context.Context, guildID, broadcasterID string) error {
	return w.requireOwner(ctx, guildID, broadcasterID, false)
}

// requireOwner checks the reverse index. missingOK treats an unbound guild
// as success (unbind / first bind); otherwise a missing row is the same
// refusal as a row owned by someone else.
func (w *Worker) requireOwner(ctx context.Context, guildID, broadcasterID string, missingOK bool) error {
	if w.discordKV == nil {
		return nil
	}
	owner, ok := w.discordKV.GetGuild(ctx, guildID)
	if !ok {
		if missingOK {
			return nil
		}
		return ErrGuildBoundElsewhere
	}
	if owner != broadcasterID {
		return ErrGuildBoundElsewhere
	}
	return nil
}

// guildFill carries one setup's state: the client, the guild, and name
// indexes of what already exists so every create is idempotent by name.
type guildFill struct {
	w          *Worker
	guild      discordGuildAPI
	guildID    string
	everyone   string
	existing   []discapi.Snowflake
	chanByName map[string]string
	roleByName map[string]string
}

func (w *Worker) newGuildFill(ctx context.Context, req GuildSetupRequest) (*guildFill, error) {
	guild, ok := w.guildAPI()
	if !ok {
		return nil, errDiscordUnavailable
	}
	if req.GuildID == "" {
		return nil, errors.New("missing guild id")
	}
	if err := w.bindGuild(ctx, req.GuildID, req.BroadcasterID); err != nil {
		return nil, err
	}
	// One global token for the whole fill: it is a one-off admin action of
	// ~21 calls, and the create buckets Discord actually enforces are paced
	// per call through Retry-After in create.
	if err := w.takeDiscordGlobal(ctx); err != nil {
		return nil, err
	}
	if req.EveryoneRoleID == "" {
		req.EveryoneRoleID = req.GuildID
	}
	existing, err := guild.ListGuildChannels(ctx, req.GuildID)
	if err != nil {
		return nil, err
	}
	roles, err := guild.ListGuildRoles(ctx, req.GuildID)
	if err != nil {
		return nil, err
	}
	return &guildFill{
		w: w, guild: guild, guildID: req.GuildID, everyone: req.EveryoneRoleID, existing: existing,
		chanByName: idsByName(existing), roleByName: idsByName(roles),
	}, nil
}

func idsByName(list []discapi.Snowflake) map[string]string {
	out := make(map[string]string, len(list))
	for _, s := range list {
		out[strings.ToLower(s.Name)] = s.ID
	}
	return out
}

// livedIn counts channels that are not part of the template. Template-named
// ones are ours (or a name collision we adopt anyway), so a half-finished
// fill is never mistaken for a living community.
func (f *guildFill) livedIn() bool {
	template := map[string]bool{}
	for _, spec := range ddiscord.CommunityChannels() {
		template[strings.ToLower(spec.Name)] = true
	}
	foreign := 0
	for _, ch := range f.existing {
		if !template[strings.ToLower(ch.Name)] {
			foreign++
		}
	}
	return foreign >= ddiscord.LivingCommunityMinChannels
}

// adopt fills the result from existing channels and roles that match the
// template by name, without creating anything.
func (f *guildFill) adopt(out *GuildSetupResult) {
	for _, spec := range ddiscord.CommunityRoles() {
		out.setRole(spec.Name, f.roleByName[strings.ToLower(spec.Name)])
	}
	for _, spec := range ddiscord.CommunityChannels() {
		out.setChannel(spec.Bind, f.chanByName[strings.ToLower(spec.Name)])
	}
}

// ensureNamed returns the id of the entry called name in index, creating it
// when absent. Creation is idempotent by name, which is what lets a fill cut
// short by a timeout finish on the next attempt.
func (f *guildFill) ensureNamed(ctx context.Context, index map[string]string, name string, create func() (discapi.Snowflake, error)) (string, error) {
	key := strings.ToLower(name)
	if id := index[key]; id != "" {
		return id, nil
	}
	created, err := f.create(ctx, create)
	if err != nil {
		return "", err
	}
	index[key] = created.ID
	return created.ID, nil
}

func (f *guildFill) ensureRoles(ctx context.Context, out *GuildSetupResult) error {
	for _, spec := range ddiscord.CommunityRoles() {
		id, err := f.ensureNamed(ctx, f.roleByName, spec.Name, func() (discapi.Snowflake, error) {
			return f.guild.CreateRole(ctx, f.guildID, discapi.RoleCreate{
				Name: spec.Name, Hoist: spec.Hoist, Mentionable: spec.Mentionable,
			})
		})
		if err != nil {
			return err
		}
		out.setRole(spec.Name, id)
	}
	return nil
}

// ensureChannels walks the template categories-first (a child needs its
// parent's id) and records every bound channel on the result.
func (f *guildFill) ensureChannels(ctx context.Context, out *GuildSetupResult) error {
	parentID := map[string]string{}
	for _, spec := range categoriesFirst(ddiscord.CommunityChannels()) {
		id, err := f.ensureNamed(ctx, f.chanByName, spec.Name, f.channelCreator(spec, parentID[spec.Parent]))
		if err != nil {
			return err
		}
		parentID[spec.Name] = id
		out.setChannel(spec.Bind, id)
	}
	return nil
}

func (f *guildFill) channelCreator(spec ddiscord.ChannelSpec, parent string) func() (discapi.Snowflake, error) {
	return func() (discapi.Snowflake, error) {
		return f.guild.CreateChannel(context.Background(), f.guildID, discapi.ChannelCreate{
			Name:                 spec.Name,
			Type:                 spec.Type,
			Topic:                spec.Topic,
			ParentID:             parent,
			PermissionOverwrites: overwrites(spec, f.everyone),
		})
	}
}

func categoriesFirst(specs []ddiscord.ChannelSpec) []ddiscord.ChannelSpec {
	out := make([]ddiscord.ChannelSpec, 0, len(specs))
	for _, spec := range specs {
		if spec.Type == ddiscord.ChannelCategory {
			out = append(out, spec)
		}
	}
	for _, spec := range specs {
		if spec.Type != ddiscord.ChannelCategory {
			out = append(out, spec)
		}
	}
	return out
}

// create runs one Discord create, sleeping the server-dictated Retry-After
// on a 429 instead of surfacing it as a hard mid-fill error.
func (f *guildFill) create(ctx context.Context, do func() (discapi.Snowflake, error)) (discapi.Snowflake, error) {
	for attempt := 0; ; attempt++ {
		got, err := do()
		wait := discapi.RetryAfterOf(err)
		if err == nil || wait <= 0 || attempt >= maxCreateRetries {
			return got, err
		}
		select {
		case <-ctx.Done():
			return got, ctx.Err()
		case <-time.After(wait):
		}
	}
}

func (out *GuildSetupResult) setRole(name, id string) {
	if id == "" {
		return
	}
	switch name {
	case "Live":
		out.LiveRoleID = id
	case "Mods":
		out.ModsRoleID = id
	case "Regulars":
		out.RegularsRoleID = id
	case "Member":
		out.MemberRoleID = id
	}
}

func (out *GuildSetupResult) setChannel(bind, id string) {
	if id == "" {
		return
	}
	switch bind {
	case "live":
		out.LiveChannelID = id
	case "clips":
		out.ClipsChannelID = id
	case "welcome":
		out.WelcomeChannelID = id
	case "alerts":
		out.AlertsChannelID = id
	case "voice":
		out.VoiceHubID = id
	}
}

func overwrites(spec ddiscord.ChannelSpec, everyone string) []discapi.PermissionOverwrite {
	if !spec.Staff && !spec.ReadOnly {
		return nil
	}
	deny := permSendMessages
	if spec.Staff {
		deny = permViewChannel
	}
	return []discapi.PermissionOverwrite{{
		ID:    everyone,
		Type:  overwriteRole,
		Allow: "0",
		Deny:  fmt.Sprintf("%d", deny),
	}}
}
