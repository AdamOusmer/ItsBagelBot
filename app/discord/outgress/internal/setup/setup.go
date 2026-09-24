// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package setup

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	discapi "ItsBagelBot/internal/discordapi"
	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"

	"go.uber.org/zap"
)

const (
	overwriteRole          = 0
	permViewChannel  int64 = 1024
	permSendMessages int64 = 2048

	maxCreateRetries = 3
)

var ErrGuildBoundElsewhere = errors.New("this Discord server is already linked to another Twitch channel")

// Must keep ErrGuildBoundElsewhere's exact message: older consoles match the text.
var ErrGuildNotBound = fmt.Errorf("%w", ErrGuildBoundElsewhere)

var ErrDiscordUnavailable = errors.New("discord client unavailable")

type GuildSetupResult struct {
	GuildID                 string
	LiveChannelID           string
	ClipsChannelID          string
	WelcomeChannelID        string
	VoiceHubID              string
	LogChannelID            string
	TicketChannelID         string
	TicketCategoryID        string
	TicketArchiveCategoryID string
	SubsChannelID           string
	SubsCategoryID          string
	VIPChannelID            string
	VIPCategoryID           string
	OwnerRoleID             string
	LeadModRoleID           string
	ModsRoleID              string
	VIPRoleID               string
	SubscriberRoleID        string
	RegularsRoleID          string
	MemberRoleID            string
	Refused                 string
	DroppedPins             []string
}

type GuildEntry struct {
	ID   string
	Name string
	Type int
}

type GuildLayout struct {
	Channels []GuildEntry
	Roles    []GuildEntry
}

type GuildSetupRequest struct {
	GuildID        string
	EveryoneRoleID string
	BroadcasterID  string
	InstalledBy    string
	Subscribers    bool
	PinnedRoles    map[string]string
}

func (w *Worker) SetupGuild(ctx context.Context, req GuildSetupRequest) (GuildSetupResult, error) {
	req.GuildID = strings.TrimSpace(req.GuildID)
	out := GuildSetupResult{GuildID: req.GuildID}
	fill, err := w.newGuildFill(ctx, req)
	if err != nil {
		return GuildSetupResult{}, err
	}
	out.DroppedPins = fill.droppedPins
	defer w.invalidateConfig(ctx, req.GuildID)
	if fill.livedIn() {
		out.Refused = "this server already has a layout; Bagel adopted the channels it recognised, pick the rest below"
		fill.adopt(&out)
		fill.postTicketDesk(ctx, out)
		return out, nil
	}
	if err := fill.ensureRoles(ctx, &out); err != nil {
		return out, err
	}
	if err := fill.ensureChannels(ctx, &out); err != nil {
		return out, err
	}
	fill.postTicketDesk(ctx, out)
	return out, nil
}

func (w *Worker) invalidateConfig(ctx context.Context, guildID string) {
	if w.store == nil || guildID == "" {
		return
	}
	w.store.Invalidate(ctx, discordstore.Guild{ID: guildID})
}

func (w *Worker) GuildLayout(ctx context.Context, req GuildSetupRequest) (GuildLayout, error) {
	if w.discord == nil {
		return GuildLayout{}, ErrDiscordUnavailable
	}
	if err := w.requireOwnerStrict(ctx, req, ownerCheck{}); err != nil {
		return GuildLayout{}, err
	}
	channels, err := w.discord.ListGuildChannels(ctx, req.guild())
	if err != nil {
		return GuildLayout{}, err
	}
	roles, err := w.discord.ListGuildRoles(ctx, req.guild())
	if err != nil {
		return GuildLayout{}, err
	}
	return GuildLayout{Channels: entries(channels), Roles: entries(roles)}, nil
}

func (w *Worker) GuildInfo(ctx context.Context, req GuildSetupRequest) (discapi.GuildInfo, error) {
	if w.discord == nil {
		return discapi.GuildInfo{}, ErrDiscordUnavailable
	}
	if err := w.requireOwnerStrict(ctx, req, ownerCheck{}); err != nil {
		return discapi.GuildInfo{}, err
	}
	return w.discord.GetGuildWithCounts(ctx, req.guild())
}

func (req GuildSetupRequest) guild() discapi.Guild {
	return discapi.Guild{ID: req.GuildID}
}

type ownerCheck struct {
	MissingOK bool
}

func (w *Worker) UnbindGuild(ctx context.Context, req GuildSetupRequest) error {
	if err := w.requireOwnerStrict(ctx, req, ownerCheck{MissingOK: true}); err != nil {
		return err
	}
	if w.store == nil {
		return nil
	}
	return w.store.UnbindGuild(ctx, discordstore.Binding{
		Guild:       discordstore.Guild{ID: req.GuildID},
		Broadcaster: discordstore.Broadcaster{ID: req.BroadcasterID},
	})
}

func entries(in []discapi.Snowflake) []GuildEntry {
	out := make([]GuildEntry, 0, len(in))
	for _, s := range in {
		out = append(out, GuildEntry{ID: s.ID, Name: s.Name, Type: s.Type})
	}
	return out
}

func (w *Worker) bindGuild(ctx context.Context, req GuildSetupRequest) error {
	if w.store == nil {
		return nil
	}
	if req.GuildID == "" || req.BroadcasterID == "" {
		return nil
	}
	if err := w.requireOwner(ctx, req, ownerCheck{MissingOK: true}); err != nil {
		return err
	}
	return w.store.BindGuild(ctx, discordstore.Binding{
		Guild:       discordstore.Guild{ID: req.GuildID},
		Broadcaster: discordstore.Broadcaster{ID: req.BroadcasterID},
		InstalledBy: req.InstalledBy,
	})
}

func (w *Worker) requireOwner(ctx context.Context, req GuildSetupRequest, check ownerCheck) error {
	if w.store == nil {
		return nil
	}
	owner, ok := w.store.Broadcaster(ctx, discordstore.Guild{ID: req.GuildID})
	if !ok {
		return missingBinding(check)
	}
	if owner.ID != req.BroadcasterID {
		return ErrGuildBoundElsewhere
	}
	return nil
}

func missingBinding(check ownerCheck) error {
	if check.MissingOK {
		return nil
	}
	return ErrGuildNotBound
}

type guildFill struct {
	w           *Worker
	api         discordGuildAPI
	target      discapi.Guild
	everyone    string
	existing    []discapi.Snowflake
	chanByName  map[string]string
	roleByName  map[string]string
	subscribers bool
	pinned      map[string]string
	droppedPins []string
}

func (w *Worker) newGuildFill(ctx context.Context, req GuildSetupRequest) (*guildFill, error) {
	if w.discord == nil {
		return nil, ErrDiscordUnavailable
	}
	if req.GuildID == "" {
		return nil, errors.New("missing guild id")
	}
	if err := w.bindGuild(ctx, req); err != nil {
		return nil, err
	}
	if req.EveryoneRoleID == "" {
		req.EveryoneRoleID = req.GuildID
	}
	existing, err := w.discord.ListGuildChannels(ctx, req.guild())
	if err != nil {
		return nil, err
	}
	roles, err := w.discord.ListGuildRoles(ctx, req.guild())
	if err != nil {
		return nil, err
	}
	pinned, dropped := adoptablePins(req.PinnedRoles, roles)
	if len(dropped) > 0 {
		w.log.Warn("pinned roles name roles this guild no longer has; falling back to the template",
			zap.String("guild_id", req.GuildID), zap.Strings("slots", dropped))
	}
	return &guildFill{
		w: w, api: w.discord, target: req.guild(), everyone: req.EveryoneRoleID, existing: existing,
		chanByName: idsByName(existing), roleByName: idsByName(roles),
		subscribers: req.Subscribers, pinned: pinned, droppedPins: dropped,
	}, nil
}

func adoptablePins(pins map[string]string, roles []discapi.Snowflake) (map[string]string, []string) {
	if len(pins) == 0 {
		return nil, nil
	}
	live := make(map[string]bool, len(roles))
	for _, r := range roles {
		live[r.ID] = true
	}
	out := make(map[string]string, len(pins))
	var dropped []string
	for slot, id := range pins {
		if live[id] {
			out[slot] = id
			continue
		}
		dropped = append(dropped, slot)
	}
	sort.Strings(dropped)
	return out, dropped
}

func (f *guildFill) pinnedRole(templateName string) string {
	slot := ddiscord.SlotForRoleName(templateName)
	if slot == "" {
		return ""
	}
	return f.pinned[string(slot)]
}

func (f *guildFill) roleID(templateName string) string {
	if id := f.pinnedRole(templateName); id != "" {
		return id
	}
	return f.roleByName[strings.ToLower(templateName)]
}

func idsByName(list []discapi.Snowflake) map[string]string {
	out := make(map[string]string, len(list))
	for _, s := range list {
		out[strings.ToLower(s.Name)] = s.ID
	}
	return out
}

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

func (f *guildFill) adopt(out *GuildSetupResult) {
	for _, spec := range ddiscord.CommunityRoles() {
		out.setRole(namedRef{Name: spec.Name, ID: f.roleID(spec.Name)})
	}
	for _, spec := range ddiscord.CommunityChannels() {
		out.setChannel(namedRef{Name: spec.Bind, ID: f.chanByName[strings.ToLower(spec.Name)]})
	}
}

type namedRef struct {
	Name string
	ID   string
}

type namedCreate func() (discapi.Snowflake, error)

type channelWant struct {
	Spec   ddiscord.ChannelSpec
	Parent string
}

func (f *guildFill) ensureNamed(ctx context.Context, index map[string]string, want namedRef, create namedCreate) (string, error) {
	key := strings.ToLower(want.Name)
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
		if !ddiscord.FeatureEnabled(spec.Feature, f.subscribers) {
			continue
		}
		if id := f.pinnedRole(spec.Name); id != "" {
			f.roleByName[strings.ToLower(spec.Name)] = id
			out.setRole(namedRef{Name: spec.Name, ID: id})
			continue
		}
		id, err := f.ensureNamed(ctx, f.roleByName, namedRef{Name: spec.Name}, f.roleCreator(ctx, spec))
		if err != nil {
			return err
		}
		out.setRole(namedRef{Name: spec.Name, ID: id})
	}
	return nil
}

func (f *guildFill) roleCreator(ctx context.Context, spec ddiscord.RoleSpec) namedCreate {
	return func() (discapi.Snowflake, error) {
		return f.api.CreateRole(ctx, discapi.GuildRole{
			Guild: f.target,
			Spec: discapi.RoleCreate{
				Name: spec.Name, Hoist: spec.Hoist, Mentionable: spec.Mentionable,
				Color: spec.Color, Permissions: rolePermissions(spec),
			},
		})
	}
}

func (f *guildFill) ensureChannels(ctx context.Context, out *GuildSetupResult) error {
	parentID := map[string]string{}
	for _, spec := range ddiscord.CommunityChannels() {
		if spec.Type != ddiscord.ChannelCategory || !ddiscord.FeatureEnabled(spec.Feature, f.subscribers) {
			continue
		}
		id, err := f.ensureNamed(ctx, f.chanByName, namedRef{Name: spec.Name}, f.channelCreator(ctx, channelWant{Spec: spec}))
		if err != nil {
			return err
		}
		parentID[spec.Name] = id
		out.setChannel(namedRef{Name: spec.Bind, ID: id})
	}
	return f.ensureChildChannels(ctx, parentID, out)
}

func (f *guildFill) postTicketDesk(ctx context.Context, out GuildSetupResult) {
	if out.TicketChannelID == "" {
		return
	}
	spec := ddiscord.Config{}.TicketPanel()
	msg, err := f.api.SendPanel(ctx, discapi.EmbedPost{
		ChannelID: out.TicketChannelID,
		Embed:     ddiscord.TicketPanelEmbed(spec),
	}, discapi.TicketDeskButtons(spec.Button))
	if err != nil {
		return
	}
	if f.w.store == nil {
		return
	}
	_ = f.w.store.RememberDesk(ctx, discordstore.DeskPanel{
		GuildID: out.GuildID, ChannelID: out.TicketChannelID, MessageID: msg.ID,
	})
}

func (f *guildFill) ensureChildChannels(ctx context.Context, parentID map[string]string, out *GuildSetupResult) error {
	for _, spec := range ddiscord.CommunityChannels() {
		if spec.Type == ddiscord.ChannelCategory || !ddiscord.FeatureEnabled(spec.Feature, f.subscribers) {
			continue
		}
		id, err := f.ensureNamed(ctx, f.chanByName, namedRef{Name: spec.Name}, f.channelCreator(ctx, channelWant{Spec: spec, Parent: parentID[spec.Parent]}))
		if err != nil {
			return err
		}
		out.setChannel(namedRef{Name: spec.Bind, ID: id})
	}
	return nil
}

func (f *guildFill) channelCreator(ctx context.Context, want channelWant) namedCreate {
	return func() (discapi.Snowflake, error) {
		return f.api.CreateChannel(ctx, discapi.GuildChannel{
			Guild: f.target,
			Spec: discapi.ChannelCreate{
				Name:                 want.Spec.Name,
				Type:                 want.Spec.Type,
				Topic:                want.Spec.Topic,
				ParentID:             want.Parent,
				PermissionOverwrites: f.overwrites(want.Spec),
			},
		})
	}
}

func (f *guildFill) create(ctx context.Context, do func() (discapi.Snowflake, error)) (discapi.Snowflake, error) {
	for attempt := 0; ; attempt++ {
		got, err := do()
		wait := discapi.RetryAfterOf(err)
		if err == nil {
			return got, err
		}
		if wait <= 0 {
			return got, err
		}
		if attempt >= maxCreateRetries {
			return got, err
		}
		select {
		case <-ctx.Done():
			return got, ctx.Err()
		case <-time.After(wait):
		}
	}
}

func (out *GuildSetupResult) setRole(role namedRef) {
	if role.ID == "" {
		return
	}
	field := out.roleSlot(role.Name)
	if field == nil {
		return
	}
	*field = role.ID
}

func (out *GuildSetupResult) roleSlot(name string) *string {
	slots := map[string]*string{
		"Owner":      &out.OwnerRoleID,
		"Lead Mod":   &out.LeadModRoleID,
		"Mods":       &out.ModsRoleID,
		"VIP":        &out.VIPRoleID,
		"Subscriber": &out.SubscriberRoleID,
		"Regulars":   &out.RegularsRoleID,
		"Member":     &out.MemberRoleID,
	}
	return slots[name]
}

func (out *GuildSetupResult) setChannel(ch namedRef) {
	if ch.ID == "" {
		return
	}
	field := out.channelSlot(ch.Name)
	if field == nil {
		return
	}
	*field = ch.ID
}

func (out *GuildSetupResult) channelSlot(name string) *string {
	slots := map[string]*string{
		"live":          &out.LiveChannelID,
		"clips":         &out.ClipsChannelID,
		"welcome":       &out.WelcomeChannelID,
		"voice":         &out.VoiceHubID,
		"logs":          &out.LogChannelID,
		"tickets":       &out.TicketChannelID,
		"ticketcat":     &out.TicketCategoryID,
		"ticketarchive": &out.TicketArchiveCategoryID,
		"subs":          &out.SubsChannelID,
		"subcat":        &out.SubsCategoryID,
		"vip":           &out.VIPChannelID,
		"vipcat":        &out.VIPCategoryID,
	}
	return slots[name]
}

func (f *guildFill) overwrites(spec ddiscord.ChannelSpec) []discapi.PermissionOverwrite {
	if len(spec.AllowRoles) > 0 {
		return f.gatedOverwrites(spec)
	}
	if spec.ReadOnly {
		return []discapi.PermissionOverwrite{{
			ID: f.everyone, Type: overwriteRole, Allow: "0", Deny: fmt.Sprintf("%d", permSendMessages),
		}}
	}
	return nil
}

func (f *guildFill) gatedOverwrites(spec ddiscord.ChannelSpec) []discapi.PermissionOverwrite {
	out := []discapi.PermissionOverwrite{{
		ID: f.everyone, Type: overwriteRole, Allow: "0", Deny: fmt.Sprintf("%d", permViewChannel),
	}}
	allow := permViewChannel | permSendMessages
	deny := int64(0)
	if spec.ReadOnly {
		allow = permViewChannel
		deny = permSendMessages
	}
	for _, name := range spec.AllowRoles {
		id := f.roleByName[strings.ToLower(name)]
		if id == "" {
			continue
		}
		out = append(out, discapi.PermissionOverwrite{
			ID: id, Type: overwriteRole,
			Allow: fmt.Sprintf("%d", allow), Deny: fmt.Sprintf("%d", deny),
		})
	}
	return out
}

func rolePermissions(spec ddiscord.RoleSpec) string {
	if spec.Permissions == 0 {
		return ""
	}
	return strconv.FormatInt(spec.Permissions, 10)
}
