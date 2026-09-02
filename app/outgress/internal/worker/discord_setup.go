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
func (w *Worker) SetupGuild(ctx context.Context, guildID, everyoneRoleID, broadcasterID string) (GuildSetupResult, error) {
	guildID = strings.TrimSpace(guildID)
	out := GuildSetupResult{GuildID: guildID}
	fill, err := w.newGuildFill(ctx, guildID, everyoneRoleID, broadcasterID)
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
	if w.discordKV == nil {
		return nil
	}
	owner, ok := w.discordKV.GetGuild(ctx, guildID)
	if !ok {
		return nil
	}
	if owner != broadcasterID {
		return ErrGuildBoundElsewhere
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
	if w.discordKV == nil || guildID == "" || broadcasterID == "" {
		return nil
	}
	if owner, ok := w.discordKV.GetGuild(ctx, guildID); ok && owner != broadcasterID {
		return ErrGuildBoundElsewhere
	}
	return w.discordKV.PutGuild(ctx, guildID, broadcasterID)
}

func (w *Worker) requireBound(ctx context.Context, guildID, broadcasterID string) error {
	if w.discordKV == nil {
		return nil
	}
	owner, ok := w.discordKV.GetGuild(ctx, guildID)
	if !ok || owner != broadcasterID {
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
	chanByName map[string]discapi.Snowflake
	roleByName map[string]string
}

func (w *Worker) newGuildFill(ctx context.Context, guildID, everyoneRoleID, broadcasterID string) (*guildFill, error) {
	guild, ok := w.guildAPI()
	if !ok {
		return nil, errDiscordUnavailable
	}
	if guildID == "" {
		return nil, errors.New("missing guild id")
	}
	if err := w.bindGuild(ctx, guildID, broadcasterID); err != nil {
		return nil, err
	}
	// One global token for the whole fill: it is a one-off admin action of
	// ~21 calls, and the create buckets Discord actually enforces are paced
	// per call through Retry-After in create.
	if err := w.takeDiscordGlobal(ctx); err != nil {
		return nil, err
	}
	if everyoneRoleID == "" {
		everyoneRoleID = guildID // Discord: @everyone role id equals guild id
	}
	existing, err := guild.ListGuildChannels(ctx, guildID)
	if err != nil {
		return nil, err
	}
	roles, err := guild.ListGuildRoles(ctx, guildID)
	if err != nil {
		return nil, err
	}
	f := &guildFill{w: w, guild: guild, guildID: guildID, everyone: everyoneRoleID, existing: existing,
		chanByName: map[string]discapi.Snowflake{}, roleByName: map[string]string{}}
	for _, ch := range existing {
		f.chanByName[strings.ToLower(ch.Name)] = ch
	}
	for _, r := range roles {
		f.roleByName[strings.ToLower(r.Name)] = r.ID
	}
	return f, nil
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
		out.setChannel(spec.Bind, f.chanByName[strings.ToLower(spec.Name)].ID)
	}
}

func (f *guildFill) ensureRoles(ctx context.Context, out *GuildSetupResult) error {
	for _, spec := range ddiscord.CommunityRoles() {
		key := strings.ToLower(spec.Name)
		id := f.roleByName[key]
		if id == "" {
			created, err := f.create(ctx, func() (discapi.Snowflake, error) {
				return f.guild.CreateRole(ctx, f.guildID, discapi.RoleCreate{
					Name: spec.Name, Hoist: spec.Hoist, Mentionable: spec.Mentionable,
				})
			})
			if err != nil {
				return err
			}
			id = created.ID
			f.roleByName[key] = id
		}
		out.setRole(spec.Name, id)
	}
	return nil
}

// ensureChannels creates categories first (children need their parent id),
// then every other channel under its category.
func (f *guildFill) ensureChannels(ctx context.Context, out *GuildSetupResult) error {
	parentID := map[string]string{}
	for _, spec := range ddiscord.CommunityChannels() {
		if spec.Type != ddiscord.ChannelCategory {
			continue
		}
		id, err := f.ensureChannel(ctx, spec, "")
		if err != nil {
			return err
		}
		parentID[spec.Name] = id
	}
	for _, spec := range ddiscord.CommunityChannels() {
		if spec.Type == ddiscord.ChannelCategory {
			continue
		}
		id, err := f.ensureChannel(ctx, spec, parentID[spec.Parent])
		if err != nil {
			return err
		}
		out.setChannel(spec.Bind, id)
	}
	return nil
}

func (f *guildFill) ensureChannel(ctx context.Context, spec ddiscord.ChannelSpec, parent string) (string, error) {
	key := strings.ToLower(spec.Name)
	if id := f.chanByName[key].ID; id != "" {
		return id, nil
	}
	created, err := f.create(ctx, func() (discapi.Snowflake, error) {
		return f.guild.CreateChannel(ctx, f.guildID, discapi.ChannelCreate{
			Name:                 spec.Name,
			Type:                 spec.Type,
			Topic:                spec.Topic,
			ParentID:             parent,
			PermissionOverwrites: overwrites(spec, f.everyone),
		})
	})
	if err != nil {
		return "", err
	}
	f.chanByName[key] = created
	return created.ID, nil
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
