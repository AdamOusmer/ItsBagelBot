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

	"ItsBagelBot/app/discord/outgress/internal/identity"
	discapi "ItsBagelBot/internal/discordapi"
	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"
	outgressrpc "ItsBagelBot/internal/domain/rpc/outgress"

	"go.uber.org/zap"
)

const (
	overwriteRole          = 0
	overwriteMember        = 1
	permViewChannel  int64 = 1024
	permSendMessages int64 = 2048
	permEmbedLinks   int64 = 16384
	permAttachFiles  int64 = 32768
	permBotPost            = permViewChannel | permSendMessages | permEmbedLinks | permAttachFiles

	channelAnnouncement = 5

	maxCreateRetries = 3
)

var ErrGuildBoundElsewhere = errors.New("this Discord server is already linked to another Twitch channel")

// Must keep ErrGuildBoundElsewhere's exact message: older consoles match the text.
var ErrGuildNotBound = fmt.Errorf("%w", ErrGuildBoundElsewhere)

var ErrDiscordUnavailable = errors.New("discord client unavailable")

type GuildSetupResult struct {
	outgressrpc.DiscordSetupIDs
	Refused     string
	DroppedPins []string
}

type GuildEntry struct {
	ID       string
	Name     string
	Type     int
	ParentID string
	CanSend  *bool
	CanEmbed *bool
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
	Mode           string
}

type TemplatePreview struct {
	LivedIn     bool
	DroppedPins []string
	Items       []outgressrpc.DiscordTemplateItem
}

func (w *Worker) SetupGuild(ctx context.Context, req GuildSetupRequest) (GuildSetupResult, error) {
	req.GuildID = strings.TrimSpace(req.GuildID)
	out := GuildSetupResult{DiscordSetupIDs: outgressrpc.DiscordSetupIDs{GuildID: req.GuildID}}
	fill, err := w.newGuildFill(ctx, req)
	if err != nil {
		return GuildSetupResult{}, err
	}
	out.DroppedPins = fill.droppedPins
	defer w.invalidateConfig(ctx, req.GuildID)
	if req.Mode == outgressrpc.SetupModeAdopt {
		fill.adopt(&out)
		return out, nil
	}
	return out, fill.apply(ctx, &out)
}

func (w *Worker) PreviewTemplate(ctx context.Context, req GuildSetupRequest) (TemplatePreview, error) {
	req.GuildID = strings.TrimSpace(req.GuildID)
	if err := w.readyFor(req); err != nil {
		return TemplatePreview{}, err
	}
	if err := w.requireOwnerStrict(ctx, req, ownerCheck{}); err != nil {
		return TemplatePreview{}, err
	}
	fill, err := w.loadGuildFill(ctx, req)
	if err != nil {
		return TemplatePreview{}, err
	}
	fill.dry = true
	if err := fill.apply(ctx, &GuildSetupResult{}); err != nil {
		return TemplatePreview{}, err
	}
	return TemplatePreview{LivedIn: fill.livedIn(), DroppedPins: fill.droppedPins, Items: fill.items}, nil
}

func (f *guildFill) apply(ctx context.Context, out *GuildSetupResult) error {
	if f.livedIn() {
		return f.applyLivedIn(ctx, out)
	}
	if err := f.ensureRoles(ctx, out); err != nil {
		return err
	}
	if err := f.ensureChannels(ctx, out); err != nil {
		return err
	}
	f.postTicketDesk(ctx, *out)
	return nil
}

func (f *guildFill) applyLivedIn(ctx context.Context, out *GuildSetupResult) error {
	out.Refused = "this server already has a layout; Bagel adopted the channels it recognised and added only the bound ones it was missing, pick the rest below"
	f.adopt(out)
	if err := f.ensureBoundChannels(ctx, out); err != nil {
		return err
	}
	f.postTicketDesk(ctx, *out)
	return nil
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
	out := GuildLayout{Channels: entries(channels), Roles: entries(roles)}
	if member, ok := w.botMember(ctx, req.guild()); ok {
		annotateBotAccess(out.Channels, channels, member, roles)
	}
	return out, nil
}

type botMemberReader interface {
	GetGuildMember(ctx context.Context, m discapi.GuildMember) (discapi.GuildMemberInfo, error)
}

func (w *Worker) botMember(ctx context.Context, guild discapi.Guild) (discapi.MemberPermissions, bool) {
	reader, ok := w.discord.(botMemberReader)
	if !ok || w.botID == "" {
		return discapi.MemberPermissions{}, false
	}
	info, err := reader.GetGuildMember(ctx, discapi.GuildMember{GuildID: guild.ID, UserID: w.botID})
	if err != nil {
		w.log.Warn("bot member lookup failed; layout carries no send flags", zap.String("guild_id", guild.ID), zap.Error(err))
		return discapi.MemberPermissions{}, false
	}
	return discapi.MemberPermissions{GuildID: guild.ID, UserID: w.botID, Roles: info.Roles}, true
}

func (w *Worker) BotCanRename(ctx context.Context, guildID string) bool {
	if w.discord == nil {
		return false
	}
	guild := discapi.Guild{ID: guildID}
	member, ok := w.botMember(ctx, guild)
	if !ok {
		return false
	}
	roles, err := w.discord.ListGuildRoles(ctx, guild)
	if err != nil {
		return false
	}
	return discapi.HasPermissions(discapi.BasePermissions(member, roles), discapi.PermChangeNickname)
}

type currentMemberWriter interface {
	ModifyCurrentMember(ctx context.Context, m discapi.CurrentMember) error
}

func (w *Worker) ApplyIdentity(ctx context.Context, guildID string, want ddiscord.GuildIdentity) error {
	writer, ok := w.discord.(currentMemberWriter)
	if !ok {
		return ErrDiscordUnavailable
	}
	return writer.ModifyCurrentMember(ctx, identity.Member(guildID, want))
}

func annotateBotAccess(out []GuildEntry, channels []discapi.Snowflake, member discapi.MemberPermissions, roles []discapi.Snowflake) {
	base := discapi.BasePermissions(member, roles)
	for i, ch := range channels {
		if !postableType(ch.Type) {
			continue
		}
		perms := discapi.ChannelPermissions(member, base, ch.PermissionOverwrites)
		canSend := discapi.HasPermissions(perms, discapi.PermViewChannel|discapi.PermSendMessages)
		canEmbed := canSend && discapi.HasPermissions(perms, discapi.PermEmbedLinks)
		out[i].CanSend, out[i].CanEmbed = &canSend, &canEmbed
	}
}

func postableType(t int) bool {
	return t == ddiscord.ChannelText || t == channelAnnouncement
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
		out = append(out, GuildEntry{ID: s.ID, Name: s.Name, Type: s.Type, ParentID: s.ParentID})
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
	dry         bool
	items       []outgressrpc.DiscordTemplateItem
	seen        map[string]bool
}

func (w *Worker) readyFor(req GuildSetupRequest) error {
	if w.discord == nil {
		return ErrDiscordUnavailable
	}
	if req.GuildID == "" {
		return errors.New("missing guild id")
	}
	return nil
}

func (w *Worker) newGuildFill(ctx context.Context, req GuildSetupRequest) (*guildFill, error) {
	if err := w.readyFor(req); err != nil {
		return nil, err
	}
	if err := w.bindGuild(ctx, req); err != nil {
		return nil, err
	}
	return w.loadGuildFill(ctx, req)
}

func (w *Worker) loadGuildFill(ctx context.Context, req GuildSetupRequest) (*guildFill, error) {
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
		items: []outgressrpc.DiscordTemplateItem{}, seen: map[string]bool{},
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
		id := f.roleID(spec.Name)
		out.setRole(namedRef{Name: spec.Name, ID: id})
		f.recordExistingRole(spec, id)
	}
	for _, spec := range ddiscord.CommunityChannels() {
		id := f.chanByName[strings.ToLower(spec.Name)]
		out.setChannel(namedRef{Name: spec.Bind, ID: id})
		f.recordExistingChannel(spec, id)
	}
}

func (f *guildFill) recordExistingRole(spec ddiscord.RoleSpec, id string) {
	if id == "" || !ddiscord.FeatureEnabled(spec.Feature, f.subscribers) {
		return
	}
	action := outgressrpc.TemplateActionAdopt
	if f.pinnedRole(spec.Name) != "" {
		action = outgressrpc.TemplateActionPinned
	}
	f.record(namedRef{Name: spec.Name, Kind: outgressrpc.TemplateKindRole}.item(action, id))
}

func (f *guildFill) recordExistingChannel(spec ddiscord.ChannelSpec, id string) {
	if id == "" || !ddiscord.FeatureEnabled(spec.Feature, f.subscribers) {
		return
	}
	f.record(channelRef(spec).item(outgressrpc.TemplateActionAdopt, id))
}

func channelRef(spec ddiscord.ChannelSpec) namedRef {
	return namedRef{Name: spec.Name, Kind: outgressrpc.TemplateKindChannel, Type: spec.Type, Parent: spec.Parent}
}

func (f *guildFill) record(item outgressrpc.DiscordTemplateItem) {
	if !f.dry {
		return
	}
	key := item.Kind + "/" + strings.ToLower(item.Name)
	if f.seen[key] {
		return
	}
	f.seen[key] = true
	f.items = append(f.items, item)
}

var livedInCreatable = map[string]bool{"voice": true, "voicecat": true, "logs": true, "ticketcat": true, "ticketarchive": true}

func (f *guildFill) ensureBoundChannels(ctx context.Context, out *GuildSetupResult) error {
	_, err := f.ensureEach(ctx, out, func(spec ddiscord.ChannelSpec) (string, bool) {
		return f.chanByName[strings.ToLower(spec.Parent)], livedInCreatable[spec.Bind]
	})
	return err
}

type channelPass func(spec ddiscord.ChannelSpec) (parent string, ok bool)

func (f *guildFill) ensureEach(ctx context.Context, out *GuildSetupResult, pass channelPass) (map[string]string, error) {
	ensured := map[string]string{}
	for _, spec := range ddiscord.CommunityChannels() {
		parent, ok := pass(spec)
		if !ok || !ddiscord.FeatureEnabled(spec.Feature, f.subscribers) {
			continue
		}
		id, err := f.ensureNamed(ctx, f.chanByName, channelRef(spec), f.channelCreator(ctx, channelWant{Spec: spec, Parent: parent}))
		if err != nil {
			return ensured, err
		}
		ensured[spec.Name] = id
		out.setChannel(namedRef{Name: spec.Bind, ID: id})
	}
	return ensured, nil
}

type namedRef struct {
	Name   string
	ID     string
	Kind   string
	Type   int
	Parent string
}

func (r namedRef) item(action, id string) outgressrpc.DiscordTemplateItem {
	return outgressrpc.DiscordTemplateItem{Kind: r.Kind, Name: r.Name, Type: r.Type, Parent: r.Parent, Action: action, ID: id}
}

const plannedID = "planned"

type namedCreate func() (discapi.Snowflake, error)

type channelWant struct {
	Spec   ddiscord.ChannelSpec
	Parent string
}

func (f *guildFill) ensureNamed(ctx context.Context, index map[string]string, want namedRef, create namedCreate) (string, error) {
	key := strings.ToLower(want.Name)
	if id := index[key]; id != "" {
		f.record(want.item(outgressrpc.TemplateActionAdopt, id))
		return id, nil
	}
	if f.dry {
		f.record(want.item(outgressrpc.TemplateActionCreate, ""))
		index[key] = plannedID
		return plannedID, nil
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
			f.record(namedRef{Name: spec.Name, Kind: outgressrpc.TemplateKindRole}.item(outgressrpc.TemplateActionPinned, id))
			continue
		}
		id, err := f.ensureNamed(ctx, f.roleByName, namedRef{Name: spec.Name, Kind: outgressrpc.TemplateKindRole}, f.roleCreator(ctx, spec))
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
	parentID, err := f.ensureEach(ctx, out, func(spec ddiscord.ChannelSpec) (string, bool) {
		return "", spec.Type == ddiscord.ChannelCategory
	})
	if err != nil {
		return err
	}
	_, err = f.ensureEach(ctx, out, func(spec ddiscord.ChannelSpec) (string, bool) {
		return parentID[spec.Parent], spec.Type != ddiscord.ChannelCategory
	})
	return err
}

func (f *guildFill) postTicketDesk(ctx context.Context, out GuildSetupResult) {
	if f.dry || out.TicketChannelID == "" {
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

func (out *GuildSetupResult) setRole(role namedRef) { assignSlot(out.roleSlot(role.Name), role.ID) }

func assignSlot(slot *string, id string) {
	if id == "" || slot == nil {
		return
	}
	*slot = id
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

func (out *GuildSetupResult) setChannel(ch namedRef) { assignSlot(out.channelSlot(ch.Name), ch.ID) }

func (out *GuildSetupResult) channelSlot(name string) *string {
	slots := map[string]*string{
		"live":          &out.LiveChannelID,
		"clips":         &out.ClipsChannelID,
		"welcome":       &out.WelcomeChannelID,
		"voice":         &out.VoiceHubID,
		"voicecat":      &out.VoiceCategoryID,
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
		return f.withBotAccess([]discapi.PermissionOverwrite{{
			ID: f.everyone, Type: overwriteRole, Allow: "0", Deny: fmt.Sprintf("%d", permSendMessages),
		}})
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
	return f.withBotAccess(out)
}

func (f *guildFill) withBotAccess(in []discapi.PermissionOverwrite) []discapi.PermissionOverwrite {
	if f.w.botID == "" {
		return in
	}
	return append(in, discapi.PermissionOverwrite{
		ID: f.w.botID, Type: overwriteMember,
		Allow: fmt.Sprintf("%d", permBotPost), Deny: "0",
	})
}

func rolePermissions(spec ddiscord.RoleSpec) string {
	if spec.Permissions == 0 {
		return ""
	}
	return strconv.FormatInt(spec.Permissions, 10)
}
