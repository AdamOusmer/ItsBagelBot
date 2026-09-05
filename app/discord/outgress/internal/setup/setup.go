// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package setup

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	discapi "ItsBagelBot/internal/discordapi"
	"ItsBagelBot/internal/discordstore"
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

// ErrGuildNotBound is "no binding at all", as opposed to
// ErrGuildBoundElsewhere's "bound to someone else". It wraps that error, and
// carries the identical message, on purpose: the console branched on the
// message substring before reply codes existed, so the two cases have to
// stay indistinguishable on the wire while being distinguishable in Go (the
// RPC layer maps them to different codes -- see rpc.codeFor).
var ErrGuildNotBound = fmt.Errorf("%w", ErrGuildBoundElsewhere)

// ErrDiscordUnavailable is returned when no Discord client is attached (no
// bot token) -- exported so the RPC layer can map it to a reply code instead
// of matching its message.
var ErrDiscordUnavailable = errors.New("discord client unavailable")

// GuildSetupResult is the snowflakes the dashboard writes into the Discord
// module blob after a fill. Outgress does not write modules.
type GuildSetupResult struct {
	GuildID          string
	LiveChannelID    string
	ClipsChannelID   string
	WelcomeChannelID string
	VoiceHubID       string
	LogChannelID     string
	TicketChannelID  string
	TicketCategoryID string
	// TicketArchiveCategoryID is the Archive category closed tickets move
	// into. Empty when the fill ran before the category existed, which the
	// ticket close path reads as "delete the channel instead".
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
	Refused                 string // non-empty when the guild looked lived-in
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
	// Subscribers mirrors the streamer's subscriber toggle. The fill skips
	// the Subscriber role and its locked category when it is off, so a server
	// that does not use the tier never grows a category nobody can open.
	Subscribers bool
	// PinnedRoles is slot -> EXISTING guild role id (see
	// ddiscord.RoleSlots). A pinned slot is ADOPTED: the fill neither
	// creates nor name-matches that role, it takes the id the streamer
	// chose. This is the only way to attach the template to a server whose
	// staff role is called something else and whose members already hold
	// it -- creating a second "Mods" role next to the real one is the
	// failure this exists to prevent.
	PinnedRoles map[string]string
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

// GuildLayout lists a bound guild's channels and roles for the dashboard
// pickers. Only the bound broadcaster may read it.
func (w *Worker) GuildLayout(ctx context.Context, req GuildSetupRequest) (GuildLayout, error) {
	if w.discord == nil {
		return GuildLayout{}, ErrDiscordUnavailable
	}
	if err := w.requireBound(ctx, req); err != nil {
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

// GuildInfo is the bound guild's name, icon and member count for the
// dashboard's server card. Bound-only, like GuildLayout: an unbound guild is
// somebody else's server and its name is not ours to hand out.
func (w *Worker) GuildInfo(ctx context.Context, req GuildSetupRequest) (discapi.GuildInfo, error) {
	if w.discord == nil {
		return discapi.GuildInfo{}, ErrDiscordUnavailable
	}
	if err := w.requireBound(ctx, req); err != nil {
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

// UnbindGuild drops the guild->broadcaster reverse index on disconnect. A
// guild bound to someone else is left alone.
func (w *Worker) UnbindGuild(ctx context.Context, req GuildSetupRequest) error {
	if err := w.requireOwner(ctx, req, ownerCheck{MissingOK: true}); err != nil {
		return err
	}
	if w.store == nil {
		return nil
	}
	return w.store.UnbindGuild(ctx, discordstore.Guild{ID: req.GuildID})
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
	return w.store.BindGuild(ctx, discordstore.Guild{ID: req.GuildID}, discordstore.Broadcaster{ID: req.BroadcasterID})
}

func (w *Worker) requireBound(ctx context.Context, req GuildSetupRequest) error {
	return w.requireOwner(ctx, req, ownerCheck{})
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

// guildFill carries one setup's state: the client, the guild, and name
// indexes of what already exists so every create is idempotent by name.
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
	return &guildFill{
		w: w, api: w.discord, target: req.guild(), everyone: req.EveryoneRoleID, existing: existing,
		chanByName: idsByName(existing), roleByName: idsByName(roles),
		subscribers: req.Subscribers, pinned: req.PinnedRoles,
	}, nil
}

// pinnedRole is the id the streamer pinned to the slot this template role
// name belongs to, or "" when nothing is pinned there.
func (f *guildFill) pinnedRole(templateName string) string {
	slot := ddiscord.SlotForRoleName(templateName)
	if slot == "" {
		return ""
	}
	return f.pinned[slot]
}

// roleID resolves a template role name to a guild role id, pinned id first.
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
		out.setRole(namedRef{Name: spec.Name, ID: f.roleID(spec.Name)})
	}
	for _, spec := range ddiscord.CommunityChannels() {
		out.setChannel(namedRef{Name: spec.Bind, ID: f.chanByName[strings.ToLower(spec.Name)]})
	}
}

// namedRef is one template name and the snowflake it resolved to.
type namedRef struct {
	Name string
	ID   string
}

type namedCreate func() (discapi.Snowflake, error)

type channelWant struct {
	Spec   ddiscord.ChannelSpec
	Parent string
}

// ensureNamed returns the id of the entry called name in index, creating it
// when absent. Creation is idempotent by name, which is what lets a fill cut
// short by a timeout finish on the next attempt.
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
			// Index the pinned id under the template name so the channel
			// gates below (gatedOverwrites looks roles up by name) allow the
			// role the streamer actually pinned, not a same-named stranger.
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

// ensureChannels walks the template categories-first (a child needs its
// parent's id) and records every bound channel on the result.
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

// postTicketDesk posts the first desk panel. The spec is a zero Config's --
// the template defaults -- because setup runs BEFORE the streamer has ever
// opened the ticket section of the dashboard, so there is no saved copy to
// render yet; editing it there and pressing Repost is what replaces this
// message with their own (see the desk.repost RPC).
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

// create runs one Discord create, sleeping the server-dictated Retry-After
// on a 429 instead of surfacing it as a hard mid-fill error.
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

// roleSlot mirrors channelSlot below: a map from template role name to the
// GuildSetupResult field it fills, so adding a role never grows a switch.
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

// gatedOverwrites denies @everyone the channel and allows it back to each
// named role. A role the fill did not create (or could not find by name) is
// skipped rather than sent as an empty id, which Discord rejects and which
// would fail the whole channel create over one missing role.
//
// Deny-then-allow is the only ordering Discord honours here: an overwrite
// allowing a role does not implicitly deny anyone else, so without the
// @everyone deny the "private" channel is world-readable.
func (f *guildFill) gatedOverwrites(spec ddiscord.ChannelSpec) []discapi.PermissionOverwrite {
	out := []discapi.PermissionOverwrite{{
		ID: f.everyone, Type: overwriteRole, Allow: "0", Deny: fmt.Sprintf("%d", permViewChannel),
	}}
	allow := permViewChannel | permSendMessages
	deny := int64(0)
	if spec.ReadOnly {
		// A read-only gated channel (the ticket Archive) allows the gate
		// through to LOOK, not to write. Denying SEND explicitly rather than
		// merely not allowing it matters because a staff role may already
		// carry SEND_MESSAGES server-wide, which an absent allow would not
		// take away.
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

// rolePermissions renders a role's permission bitfield the way Discord wants
// it: a decimal string, or empty for a role that grants nothing. See
// discapi.RoleCreate.Permissions for why it is a string and not a number.
func rolePermissions(spec ddiscord.RoleSpec) string {
	if spec.Permissions == 0 {
		return ""
	}
	return strconv.FormatInt(spec.Permissions, 10)
}
