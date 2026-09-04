// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"

	"ItsBagelBot/app/discord/engine/internal/cmd"
	"ItsBagelBot/app/discord/engine/internal/decode"
	"ItsBagelBot/app/discord/engine/module"
	ddiscord "ItsBagelBot/internal/domain/discord"
)

// Welcome ports app/dingress/internal/community/welcome.go: autorole,
// the welcome embed, the goodbye line, and the join/leave log lines. Both
// GUILD_MEMBER_ADD and GUILD_MEMBER_REMOVE ride discord.ingress.event.member
// (see internal/domain/discord's Event doc), distinguished here by
// Event.Type exactly as community's communityEvents map did.
func Welcome(tiers TierReader) module.Module {
	h := welcomeModule{tiers: tiers}
	b := module.NewModule("welcome")
	b.On("GUILD_MEMBER_ADD", h.onMemberAdd)
	b.On("GUILD_MEMBER_REMOVE", onMemberRemove)
	return b.Build()
}

// TierReader answers what Twitch tier a Discord member's linked identity
// carries for the guild's broadcaster: ddiscord.TierSubscriber, TierVIP,
// TierRegular, or (false) not linked at all.
//
// It is an interface with no production implementation yet ON PURPOSE. The
// repo has no Discord-to-Twitch VIEWER link (identitystore is the BOT's
// per-guild appearance, and data.users.changed carries the BROADCASTER's
// Bagel plan, not a viewer's sub status), so the engine wires nil here and
// autorole grants the member role only. The seam exists so that when the
// link lands, tier autorole is one wiring line and not a redesign -- and so
// the add/remove arithmetic (ddiscord.AutoRolePlan) is written and tested
// against the real config today.
type TierReader interface {
	Tier(ctx context.Context, broadcasterID, discordUserID string) (string, bool)
}

type welcomeModule struct {
	tiers TierReader
}

func (h welcomeModule) onMemberAdd(ctx context.Context, c *module.Context, emit module.Emit) error {
	ev, err := decode.Decode[decode.MemberEvent](c.Event.Raw)
	if err != nil {
		return err
	}
	if ev.User.Bot {
		return nil
	}
	h.autorole(ctx, c, ev, emit)
	if !shouldWelcome(c.Config) {
		logJoin(c, ev, emit)
		return nil
	}
	shown := decode.DisplayName(decode.Display{User: ev.User, Nick: ev.Nick})
	emit(cmd.PostEmbed(cmd.ChannelTarget(c.Config.GuildID, c.Config.WelcomeChannelID),
		ddiscord.WelcomeEmbed(ddiscord.WelcomeCard{Display: shown, AvatarURL: decode.AvatarURL(ev.User)})))
	logJoin(c, ev, emit)
	return nil
}

func shouldWelcome(cfg ddiscord.Config) bool {
	if !cfg.WelcomeOn() {
		return false
	}
	return cfg.WelcomeChannelID != ""
}

// autorole applies the member role and, when a tier is known, the tier
// roles. Gated on Config.AutoRoleOn so a streamer who hands roles out by
// hand can turn Bagel off without losing the rest of the welcome flow.
func (h welcomeModule) autorole(ctx context.Context, c *module.Context, ev decode.MemberEvent, emit module.Emit) {
	if !c.Config.AutoRoleOn() {
		return
	}
	plan := ddiscord.AutoRolePlan(c.Config, h.tier(ctx, c, ev.User.ID), ev.Roles, true)
	emitRolePlan(cmd.UserTarget(ev.GuildID, ev.User.ID), plan, emit)
}

// tier reads the member's linked Twitch tier. A missing reader (the current
// production wiring, see TierReader) or an unlinked member is TierNone,
// which AutoRolePlan treats as "touch no tier role".
func (h welcomeModule) tier(ctx context.Context, c *module.Context, userID string) string {
	if h.tiers == nil {
		return ddiscord.TierNone
	}
	tier, ok := h.tiers.Tier(ctx, c.BroadcasterID, userID)
	if !ok {
		return ddiscord.TierNone
	}
	return tier
}

// emitRolePlan turns a RolePlan into lane commands. Adds go out before
// removes so a member never spends a round trip holding none of their tier
// roles, which is visible in the member list.
func emitRolePlan(target cmd.Target, plan ddiscord.RolePlan, emit module.Emit) {
	for _, id := range plan.Add {
		emit(cmd.AddRole(target, cmd.RoleID(id)))
	}
	for _, id := range plan.Remove {
		emit(cmd.RemoveRole(target, cmd.RoleID(id)))
	}
}

func onMemberRemove(_ context.Context, c *module.Context, emit module.Emit) error {
	ev, err := decode.Decode[decode.MemberEvent](c.Event.Raw)
	if err != nil {
		return err
	}
	if ev.User.Bot {
		return nil
	}
	shown := decode.DisplayName(decode.Display{User: ev.User, Nick: ev.Nick})
	if shouldGoodbye(c.Config) {
		emit(cmd.PostChat(cmd.ChannelTarget(c.Config.GuildID, c.Config.WelcomeChannelID), ddiscord.GoodbyeContent(ddiscord.Goodbye{Display: shown})))
	}
	return logLine(c, emit, logEntry{Title: "Member left", Body: shown + " (" + ev.User.ID + ")"})
}

func shouldGoodbye(cfg ddiscord.Config) bool {
	if !cfg.GoodbyeOn() {
		return false
	}
	return cfg.WelcomeChannelID != ""
}

func logJoin(c *module.Context, ev decode.MemberEvent, emit module.Emit) {
	shown := decode.DisplayName(decode.Display{User: ev.User, Nick: ev.Nick})
	_ = logLine(c, emit, logEntry{Title: "Member joined", Body: shown + " (" + ev.User.ID + ")"})
}

// logEntry is one #logs line. Shared by welcome.go and message.go, matching
// community's welcome.go/message.go split before this move.
type logEntry struct {
	Title string
	Body  string
}

// logLine emits a TypePostEmbed Command into the guild's log channel, gated
// on LogsOn and a configured channel, exactly like community's
// Bot.logLine.
func logLine(c *module.Context, emit module.Emit, entry logEntry) error {
	if !c.Config.LogsOn() {
		return nil
	}
	if c.Config.LogChannelID == "" {
		return nil
	}
	emit(cmd.PostEmbed(cmd.ChannelTarget(c.Config.GuildID, c.Config.LogChannelID),
		ddiscord.LogEmbed(ddiscord.LogLine{Title: entry.Title, Body: entry.Body})))
	return nil
}
