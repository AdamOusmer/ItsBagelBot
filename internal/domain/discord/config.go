// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package discord holds the module blob and community-template contracts
// shared by outgress (live posts, setup fill), dingress (welcomes,
// auto-voice, tickets, slash), and the dashboard.
package discord

import (
	"strconv"
	"strings"

	"ItsBagelBot/pkg/codec"
)

// ModuleName is the ModuleView key. The dashboard Discord tile writes this
// row; outgress reads it on stream.online without going through sesame.
const ModuleName = "discord"

// Config is the module blob. Channel and role snowflakes are not secrets;
// the fleet bot token never rides here.
type Config struct {
	GuildID          string `json:"guildId"`
	LiveChannelID    string `json:"liveChannelId"`
	ClipsChannelID   string `json:"clipsChannelId"`
	WelcomeChannelID string `json:"welcomeChannelId"`
	VoiceHubID       string `json:"voiceHubId"`
	LogChannelID     string `json:"logChannelId"`
	TicketChannelID  string `json:"ticketChannelId"`
	TicketCategoryID string `json:"ticketCategoryId"`
	OwnerRoleID      string `json:"ownerRoleId"`
	LeadModRoleID    string `json:"leadModRoleId"`
	ModsRoleID       string `json:"modsRoleId"`
	VIPRoleID        string `json:"vipRoleId"`
	SubscriberRoleID string `json:"subscriberRoleId"`
	RegularsRoleID   string `json:"regularsRoleId"`
	MemberRoleID     string `json:"memberRoleId"`

	// SubsChannelID/SubsCategoryID and VIPChannelID/VIPCategoryID are the
	// gated tier rooms the fill creates. They were produced by setup long
	// before they were persisted here, which left them orphaned: the fill
	// returned ids nothing ever stored, so the dashboard could not show the
	// rooms and no module could post into them. See TierRooms.
	SubsChannelID  string `json:"subsChannelId"`
	SubsCategoryID string `json:"subsCategoryId"`
	VIPChannelID   string `json:"vipChannelId"`
	VIPCategoryID  string `json:"vipCategoryId"`

	// TicketArchiveCategoryID is the category a closed ticket channel moves
	// into instead of being deleted. Empty means delete on close, which is
	// the behaviour a guild that never ran a fill with an Archive category
	// keeps.
	TicketArchiveCategoryID string `json:"ticketArchiveCategoryId"`
	// TicketStaffRoles is a comma-separated list of role ids that may see
	// and claim tickets. Empty falls back to StaffRoleIDs, so a guild that
	// never touched the setting keeps the Owner/Lead Mod/Mods default.
	TicketStaffRoles string `json:"ticketStaffRoleIds"`
	// TicketOpenLimit is the per-user cap on simultaneously open tickets,
	// "1".."5". See TicketOpenLimitN for the clamp.
	TicketOpenLimit string `json:"ticketOpenLimit"`
	// TicketTranscriptEnabled is default-ON (alertOn): a guild that turned
	// the desk on wants the record of what was said in it, and losing a
	// closed ticket's history is unrecoverable, unlike an unwanted upload.
	TicketTranscriptEnabled string `json:"ticketTranscriptEnabled"`
	// TicketLogChannelID is where close summaries and transcripts land.
	// Empty falls back to LogChannelID (see TicketLogChannel).
	TicketLogChannelID string `json:"ticketLogChannelId"`
	// TicketPanel* are the desk embed's copy. Empty fields take the
	// defaults in TicketPanel below rather than rendering blank.
	TicketPanelTitle  string `json:"ticketPanelTitle"`
	TicketPanelBody   string `json:"ticketPanelBody"`
	TicketPanelColor  string `json:"ticketPanelColor"`
	TicketPanelButton string `json:"ticketPanelButton"`

	// PinnedRoles is a comma-separated list of slot=roleId pairs naming an
	// EXISTING guild role the streamer picked for a template slot. Setup
	// adopts these instead of creating or matching by name. See PinnedRole.
	PinnedRoles string `json:"pinnedRoles"`
	// AutoRoleEnabled is default-ON (alertOn): the member role on join is
	// what the whole template's channel gating rests on, so a blank value
	// must not leave every joiner without it.
	AutoRoleEnabled string `json:"autoRoleEnabled"`

	// Toggles are dashboard "on"/"off" strings. Empty means the default
	// documented on each helper (live/clips on; goodbye off).
	LiveEnabled      string `json:"liveEnabled"`
	ClipsEnabled     string `json:"clipsEnabled"`
	WelcomeEnabled   string `json:"welcomeEnabled"`
	GoodbyeEnabled   string `json:"goodbyeEnabled"`
	VoiceEnabled     string `json:"voiceEnabled"`
	TicketsEnabled   string `json:"ticketsEnabled"`
	LogsEnabled      string `json:"logsEnabled"`
	LevelsEnabled    string `json:"levelsEnabled"`
	LinkGuardEnabled string `json:"linkGuardEnabled"`
	// SubscribersEnabled gates the subscriber tier: the Subscriber role and
	// its locked category. Off by default, because a server with no linked
	// subs would otherwise show an empty category nobody can open.
	SubscribersEnabled string `json:"subscribersEnabled"`

	// CategoryAllow / CategoryDeny are comma-separated Twitch category
	// names (Scenes twin). Empty allow means every category; a deny match
	// always wins.
	CategoryAllow string `json:"categoryAllow"`
	CategoryDeny  string `json:"categoryDeny"`

	// LinkAllowList is a comma-separated list of link substrings (e.g. a
	// partner server's invite, or a domain the guild links often on
	// purpose) that app/discord/engine/modules.LinkGuard exempts from
	// automod, the same shape as CategoryAllow/CategoryDeny above. See
	// LinkAllowed.
	LinkAllowList string `json:"linkAllowList"`

	TwitchLogin string `json:"twitchLogin"`
}

// Parse decodes a module blob. An empty or malformed blob is a zero Config
// (not connected), so callers can treat that as "do nothing".
func Parse(raw []byte) Config {
	var c Config
	if len(raw) == 0 {
		return c
	}
	_ = codec.Unmarshal(raw, &c)
	return c
}

// Connected is a guild id we can actually post into.
func (c Config) Connected() bool { return strings.TrimSpace(c.GuildID) != "" }

// Named views over the raw strings Config stores.
//
// The FIELDS stay `string` and keep their JSON tags: the blob is a wire
// contract the console reads by tag and app/db writes back verbatim, so
// retyping a field would be a migration, not a refactor. The helpers below
// take these instead. Every one of them used to take a bare `string`, and
// since almost every field on Config is a string of some other kind, a call
// that handed a toggle to a list splitter -- alertOn(c.CategoryAllow),
// splitCSV(c.LiveEnabled) -- compiled and answered plausibly wrong. Naming
// the four kinds moves that into a compile error at no runtime cost.
type (
	// toggleText is one module toggle's stored text: "", "on" or "off".
	toggleText string
	// listText is a comma-separated stored list: role ids, "slot=roleId"
	// pairs, or category names.
	listText string
	// nameText is one already-trimmed, already-lowercased name, compared
	// against splitCSV output.
	nameText string
	// copyText is one piece of streamer-authored panel copy, which falls
	// back to a default when blank.
	copyText string
)

func alertOn(v toggleText) bool { return v != "off" }

func (c Config) LiveOn() bool    { return alertOn(toggleText(c.LiveEnabled)) }
func (c Config) ClipsOn() bool   { return alertOn(toggleText(c.ClipsEnabled)) }
func (c Config) WelcomeOn() bool { return alertOn(toggleText(c.WelcomeEnabled)) }
func (c Config) GoodbyeOn() bool { return c.GoodbyeEnabled == "on" }
func (c Config) VoiceOn() bool   { return alertOn(toggleText(c.VoiceEnabled)) }
func (c Config) TicketsOn() bool { return alertOn(toggleText(c.TicketsEnabled)) }
func (c Config) LogsOn() bool    { return alertOn(toggleText(c.LogsEnabled)) }
func (c Config) LevelsOn() bool  { return alertOn(toggleText(c.LevelsEnabled)) }

// TicketTranscriptOn reports whether a closing ticket is transcribed. See
// the field for why this is default-ON.
func (c Config) TicketTranscriptOn() bool { return alertOn(toggleText(c.TicketTranscriptEnabled)) }

// AutoRoleOn reports whether Bagel grants the member role on join. Default
// ON; see the field. Tier roles are never applied by the engine.
func (c Config) AutoRoleOn() bool { return alertOn(toggleText(c.AutoRoleEnabled)) }

// TierRooms is the gated subscriber/VIP furniture the fill created, in the
// order the dashboard renders it. It exists so the four ids have exactly
// one reader rather than four ad-hoc field reads (they had none at all
// before, see the fields).
type TierRooms struct {
	SubsChannelID  string
	SubsCategoryID string
	VIPChannelID   string
	VIPCategoryID  string
}

// TierRooms returns the subscriber and VIP room ids.
func (c Config) TierRooms() TierRooms {
	return TierRooms{
		SubsChannelID:  c.SubsChannelID,
		SubsCategoryID: c.SubsCategoryID,
		VIPChannelID:   c.VIPChannelID,
		VIPCategoryID:  c.VIPCategoryID,
	}
}

// TicketArchiveCategory is the category closed tickets move into. Empty
// means "delete the channel instead", which is the pre-archive behaviour.
func (c Config) TicketArchiveCategory() string { return strings.TrimSpace(c.TicketArchiveCategoryID) }

// TicketStaffRoleIDs are the roles that may see and claim a ticket. An
// unset list falls back to StaffRoleIDs rather than to nobody: a guild that
// enabled the desk without touching this setting must still have staff able
// to answer, and an empty overwrite list would make every ticket private to
// its opener.
func (c Config) TicketStaffRoleIDs() []string {
	if ids := splitList(listText(c.TicketStaffRoles)); len(ids) > 0 {
		return ids
	}
	return c.StaffRoleIDs()
}

// TicketOpenLimitDefault is the per-user open-ticket cap when unset. One is
// deliberate: a second ticket from the same person is almost always the
// same problem restated, and the desk's whole cost is a staff member's
// attention.
const TicketOpenLimitDefault = 1

// TicketOpenLimitMax bounds what the dashboard may ask for. Five open
// tickets per user already saturates a small mod team.
const TicketOpenLimitMax = 5

// TicketOpenLimitN is the per-user cap, clamped into 1..TicketOpenLimitMax.
// A malformed or out-of-range value reads as the default rather than as
// zero, because zero would silently close the desk to everyone.
func (c Config) TicketOpenLimitN() int {
	n, err := strconv.Atoi(strings.TrimSpace(c.TicketOpenLimit))
	if err != nil || n < 1 {
		return TicketOpenLimitDefault
	}
	if n > TicketOpenLimitMax {
		return TicketOpenLimitMax
	}
	return n
}

// TicketLogChannel is where ticket close summaries and transcripts post.
// Falls back to the general log channel so a guild only has to configure
// one place unless it wants tickets separated.
func (c Config) TicketLogChannel() string {
	if id := strings.TrimSpace(c.TicketLogChannelID); id != "" {
		return id
	}
	return strings.TrimSpace(c.LogChannelID)
}

// TicketPanelSpec is the resolved desk embed: never blank fields, so a
// caller renders it without re-deciding defaults.
type TicketPanelSpec struct {
	Title string
	Body  string
	// Color is the embed's left bar, nil when nobody picked one.
	//
	// A pointer rather than an int because 0 is #000000, a colour a streamer
	// can legitimately choose. The earlier rule ("zero means unset") had no
	// way to tell the two apart, so every black panel came out brand purple
	// and no amount of re-saving fixed it. The alternative considered was a
	// sentinel like -1, which survives JSON but leaks an impossible colour
	// into every reader that forgets to check for it.
	Color  *int
	Button string
}

// ColorOr resolves the bar colour, falling back only when it was never set.
// A set colour of 0 stays 0.
func (s TicketPanelSpec) ColorOr(fallback int) int {
	if s.Color == nil {
		return fallback
	}
	return *s.Color
}

// Ticket panel defaults. Kept as constants so the dashboard preview and the
// posted embed cannot drift.
const (
	TicketPanelTitleDefault  = "Need help?"
	TicketPanelBodyDefault   = "Open a private ticket with the staff."
	TicketPanelButtonDefault = "Open a ticket"

	// TicketPanelTitleMax / BodyMax / ButtonMax are Discord's own limits,
	// tightened where a shorter one reads better: an embed title may be 256
	// and a description 4096, but a desk panel that long is a wall nobody
	// reads, and a button label over 80 is truncated by Discord itself.
	TicketPanelTitleMax  = 256
	TicketPanelBodyMax   = 1000
	TicketPanelButtonMax = 40
)

// TicketPanel resolves the desk embed copy, filling every empty field with
// its default.
func (c Config) TicketPanel() TicketPanelSpec {
	color := LiveColor
	if parsed, ok := ParseHexColor(c.TicketPanelColor); ok {
		color = parsed
	}
	return TicketPanelSpec{
		Title:  firstNonEmpty(copyText(c.TicketPanelTitle), TicketPanelTitleDefault),
		Body:   firstNonEmpty(copyText(c.TicketPanelBody), TicketPanelBodyDefault),
		Button: firstNonEmpty(copyText(c.TicketPanelButton), TicketPanelButtonDefault),
		Color:  &color,
	}
}

// OrDefaults fills a spec that crossed a process boundary. TicketPanel already
// resolves every field, but a spec that arrived over RPC was assembled by the
// caller and may carry blanks (a dashboard that sent only the fields it
// changed, or no colour at all).
//
// Only an absent colour is defaulted: a spec that carries 0 asked for black
// and gets black. See TicketPanelSpec.Color.
func (s TicketPanelSpec) OrDefaults() TicketPanelSpec {
	s.Title = firstNonEmpty(copyText(s.Title), TicketPanelTitleDefault)
	s.Body = firstNonEmpty(copyText(s.Body), TicketPanelBodyDefault)
	s.Button = firstNonEmpty(copyText(s.Button), TicketPanelButtonDefault)
	if s.Color == nil {
		color := LiveColor
		s.Color = &color
	}
	return s
}

func firstNonEmpty(v, fallback copyText) string {
	if t := strings.TrimSpace(string(v)); t != "" {
		return t
	}
	return string(fallback)
}

// ParseHexColor turns a dashboard "#rrggbb" string into Discord's RGB
// integer. Only the six-digit form is accepted: the three-digit shorthand
// would have to be doubled per nibble, and the colour input the dashboard
// ships (<input type="color">) always emits six digits, so supporting the
// short form buys nothing and hides typos like "#ff00" as valid.
func ParseHexColor(s string) (int, bool) {
	t := strings.TrimSpace(s)
	if len(t) != 7 || t[0] != '#' {
		return 0, false
	}
	// bitSize 24: six hex digits are exactly 24 bits, so the length check
	// above already bounds n. Stating it here keeps the int(n) below provably
	// in range on any int width (CodeQL go/incorrect-integer-conversion
	// flagged the previous bitSize 32 for a 32-bit int it can never reach).
	n, err := strconv.ParseUint(t[1:], 16, 24)
	if err != nil {
		return 0, false
	}
	return int(n), true
}

// splitList splits a comma-separated list WITHOUT lowercasing, unlike
// splitCSV above: that one compares human names (categories, link
// substrings) case-insensitively, while this one carries snowflakes and
// slot keys where case is either irrelevant or, for camelCase slots like
// leadMod, load-bearing.
func splitList(s listText) []string {
	if strings.TrimSpace(string(s)) == "" {
		return nil
	}
	parts := strings.Split(string(s), ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// LinkGuardOn reports whether the linkguard automod module is active for
// this guild. Default OFF (like GoodbyeOn), not the alertOn "anything but
// off" default the cosmetic toggles use above: this module can delete a
// member's message, so a guild should opt in explicitly rather than
// inherit an enforcement behavior it never asked for just because the
// field was left blank.
func (c Config) LinkGuardOn() bool { return c.LinkGuardEnabled == "on" }

// SubscribersOn reports whether the subscriber tier is enabled. Opt-in, not
// alertOn: an unset value means the streamer never chose, and defaulting a
// locked category to on would put furniture in every server.
func (c Config) SubscribersOn() bool { return c.SubscribersEnabled == "on" }

// CategoryAllowed reports whether a Twitch category should produce a go-live
// embed. Names compare case-insensitively, trimmed.
func (c Config) CategoryAllowed(category string) bool {
	cat := nameText(strings.TrimSpace(strings.ToLower(category)))
	if containsName(splitCSV(listText(c.CategoryDeny)), cat) {
		return false
	}
	allow := splitCSV(listText(c.CategoryAllow))
	return len(allow) == 0 || containsName(allow, cat)
}

// HasCategoryAllow reports whether an allow-list is set, which is when an
// unknown category cannot be decided and the caller must fetch it.
func (c Config) HasCategoryAllow() bool { return len(splitCSV(listText(c.CategoryAllow))) > 0 }

// LinkAllowed reports whether raw -- the untouched link text a message
// contained, before linkguard's own NormalizeLink -- matches an entry on
// LinkAllowList. This compares against the raw text with plain
// case-insensitive substring containment rather than replicating
// NormalizeLink's host/path parsing a second time here: this package is
// the shared module blob (imported by outgress and the dashboard too) and
// deliberately does not depend on internal/domain/discord/linkguard, so it
// has no access to that normalization, and substring containment is also
// far more forgiving of exactly how an admin pastes a partner's invite
// (with or without a scheme, trailing slash, letter case) than an exact
// match would be.
func (c Config) LinkAllowed(raw string) bool {
	needle := strings.ToLower(strings.TrimSpace(raw))
	if needle == "" {
		return false
	}
	for _, entry := range splitCSV(listText(c.LinkAllowList)) {
		if strings.Contains(needle, entry) {
			return true
		}
	}
	return false
}

// containsName is a membership test on splitCSV output; an empty needle
// never matches because splitCSV drops empty entries.
func containsName(list []string, needle nameText) bool {
	if needle == "" {
		return false
	}
	for _, v := range list {
		if v == string(needle) {
			return true
		}
	}
	return false
}

func splitCSV(s listText) []string {
	if strings.TrimSpace(string(s)) == "" {
		return nil
	}
	parts := strings.Split(string(s), ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.ToLower(strings.TrimSpace(p)); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// StaffRoleIDs are the roles that count as staff: Owner, Lead Mod and Mods,
// in that order. It exists so "is this member staff" has ONE answer. The
// checks that need it are spread across modules (linkguard's exemption,
// ticket channel access, anything added later), and a new tier added to one
// list but not the others is a silent authorization gap, not a visible bug.
//
// Empty ids are skipped rather than matched: a guild that never ran the fill,
// or picked no role, must not grant staff to everyone whose role list happens
// to contain "".
func (c Config) StaffRoleIDs() []string {
	out := make([]string, 0, 3)
	for _, id := range []string{c.OwnerRoleID, c.LeadModRoleID, c.ModsRoleID} {
		if id != "" {
			out = append(out, id)
		}
	}
	return out
}
