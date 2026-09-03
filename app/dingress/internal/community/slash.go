// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package community

import (
	"context"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/dingress/internal/store"
	"ItsBagelBot/internal/discordapi"
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/pkg/codec"
)

func (b *Bot) onInteraction(ctx context.Context, raw []byte) error {
	in, err := decode[interactionEvent](raw)
	if err != nil {
		return err
	}
	cfg, ok := b.bound(ctx, store.Guild{ID: in.GuildID})
	if !ok {
		return nil
	}
	if in.Data.CustomID != "" {
		return b.onButton(ctx, cfg, in)
	}
	return b.slash(ctx, cfg, in)
}

func (b *Bot) onButton(ctx context.Context, cfg ddiscord.Config, in interactionEvent) error {
	h, ok := buttonCmds[in.Data.CustomID]
	if !ok {
		return nil
	}
	return h(b, ctx, cfg, in)
}

var buttonCmds = map[string]slashFn{
	discordapi.CustomTicketOpen:  (*Bot).openTicket,
	discordapi.CustomTicketClose: (*Bot).ticketCloseSlash,
	discordapi.CustomVoiceLock:   (*Bot).voiceLockButton,
	discordapi.CustomVoiceUnlock: (*Bot).voiceUnlockButton,
	discordapi.CustomDailyClaim:  (*Bot).daily,
}

func (b *Bot) slash(ctx context.Context, cfg ddiscord.Config, in interactionEvent) error {
	h, ok := slashCmds[in.Data.Name]
	if !ok {
		return b.reply(ctx, in, "Unknown command.")
	}
	return h(b, ctx, cfg, in)
}

type slashFn func(*Bot, context.Context, ddiscord.Config, interactionEvent) error

var slashCmds = map[string]slashFn{
	"ticket":  (*Bot).ticketSlash,
	"voice":   (*Bot).voiceSlash,
	"timeout": (*Bot).modTimeout,
	"kick":    (*Bot).modKick,
	"ban":     (*Bot).modBan,
	"purge":   (*Bot).modPurge,
	"daily":   (*Bot).daily,
	"rank":    (*Bot).rank,
}

func (b *Bot) ticketSlash(ctx context.Context, cfg ddiscord.Config, in interactionEvent) error {
	h, ok := ticketSubs[firstSub(in.Data.Options).Name]
	if !ok {
		return b.reply(ctx, in, "Use /ticket open, close, or panel.")
	}
	return h(b, ctx, cfg, in)
}

var ticketSubs = map[string]slashFn{
	"open":  (*Bot).openTicket,
	"close": (*Bot).ticketCloseSlash,
	"panel": (*Bot).postTicketPanel,
}

func (b *Bot) ticketCloseSlash(ctx context.Context, _ ddiscord.Config, in interactionEvent) error {
	return b.closeTicket(ctx, in)
}

func (b *Bot) voiceSlash(ctx context.Context, _ ddiscord.Config, in interactionEvent) error {
	return b.voiceCommand(ctx, in, firstSub(in.Data.Options))
}

func (b *Bot) requireMod(in interactionEvent) bool {
	return canMod(permBits{Raw: in.Member.Permissions})
}

func (b *Bot) modTimeout(ctx context.Context, cfg ddiscord.Config, in interactionEvent) error {
	if !b.requireMod(in) {
		return b.reply(ctx, in, "Mods only.")
	}
	userID := optionUser(in.Data.Options, "user")
	mins := optionIntFrom(in.Data.Options, "minutes")
	if userID == "" {
		return b.reply(ctx, in, "Need a user and a duration in minutes.")
	}
	if mins <= 0 {
		return b.reply(ctx, in, "Need a user and a duration in minutes.")
	}
	until := time.Now().UTC().Add(time.Duration(mins) * time.Minute).Format(time.RFC3339)
	err := b.REST.TimeoutMember(ctx, discordapi.MemberTimeout{
		GuildID: in.GuildID, UserID: userID, UntilISO: until,
	})
	if err != nil {
		return err
	}
	line := mention(userRef{ID: userID}) + " for " + strconv.Itoa(mins) + " minutes"
	_ = b.logLine(ctx, cfg, logEntry{Title: "Timeout", Body: line})
	return b.reply(ctx, in, "Timed out "+line+".")
}

func (b *Bot) modKick(ctx context.Context, cfg ddiscord.Config, in interactionEvent) error {
	return b.modRemove(ctx, cfg, in, "Kick", "Kicked ", b.REST.KickMember)
}

func (b *Bot) modBan(ctx context.Context, cfg ddiscord.Config, in interactionEvent) error {
	return b.modRemove(ctx, cfg, in, "Ban", "Banned ", b.REST.BanMember)
}

func (b *Bot) modRemove(
	ctx context.Context,
	cfg ddiscord.Config,
	in interactionEvent,
	title, prefix string,
	act func(context.Context, discordapi.GuildMember) error,
) error {
	if !b.requireMod(in) {
		return b.reply(ctx, in, "Mods only.")
	}
	userID := optionUser(in.Data.Options, "user")
	if userID == "" {
		return b.reply(ctx, in, "Need a user.")
	}
	if err := act(ctx, discordapi.GuildMember{GuildID: in.GuildID, UserID: userID}); err != nil {
		return err
	}
	who := mention(userRef{ID: userID})
	_ = b.logLine(ctx, cfg, logEntry{Title: title, Body: who})
	return b.reply(ctx, in, prefix+who+".")
}

func (b *Bot) modPurge(ctx context.Context, cfg ddiscord.Config, in interactionEvent) error {
	if !b.requireMod(in) {
		return b.reply(ctx, in, "Mods only.")
	}
	n := optionIntFrom(in.Data.Options, "count")
	if n < 2 {
		n = 2
	}
	if n > 100 {
		n = 100
	}
	msgs, err := b.REST.ListMessages(ctx, discordapi.MessageQuery{ChannelID: in.ChannelID, Limit: n})
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(msgs))
	for _, m := range msgs {
		ids = append(ids, m.ID)
	}
	if len(ids) < 2 {
		return b.reply(ctx, in, "Not enough messages to purge.")
	}
	if err := b.REST.BulkDeleteMessages(ctx, discordapi.Purge{ChannelID: in.ChannelID, MessageIDs: ids}); err != nil {
		return err
	}
	_ = b.logLine(ctx, cfg, logEntry{Title: "Purge", Body: strconv.Itoa(len(ids)) + " messages in <#" + in.ChannelID + ">"})
	return b.reply(ctx, in, "Deleted "+strconv.Itoa(len(ids))+" messages.")
}

func (b *Bot) daily(ctx context.Context, cfg ddiscord.Config, in interactionEvent) error {
	if !cfg.LevelsOn() {
		return b.reply(ctx, in, "Levels are off.")
	}
	ok, xp := b.Store.ClaimDaily(ctx, store.Member{GuildID: in.GuildID, UserID: in.Member.User.ID})
	return b.replyEmbed(ctx, in, ddiscord.DailyEmbed(ddiscord.DailyCard{XP: xp, Fresh: ok}), nil)
}

func (b *Bot) rank(ctx context.Context, cfg ddiscord.Config, in interactionEvent) error {
	if !cfg.LevelsOn() {
		return b.reply(ctx, in, "Levels are off.")
	}
	userID := optionUser(in.Data.Options, "user")
	if userID == "" {
		userID = in.Member.User.ID
	}
	xp, level := b.Store.Rank(ctx, store.Member{GuildID: in.GuildID, UserID: userID})
	card := ddiscord.RankEmbed(ddiscord.RankCard{
		Who: mention(userRef{ID: userID}), Level: level, XP: xp,
	})
	buttons := []discordapi.Button{}
	if userID == in.Member.User.ID {
		buttons = discordapi.DailyClaimButtons()
	}
	return b.replyEmbed(ctx, in, card, buttons)
}

func (b *Bot) reply(ctx context.Context, in interactionEvent, content string) error {
	return b.REST.InteractionCallback(ctx, discordapi.Callback{
		Interaction: discordapi.Interaction{ID: in.ID, Token: in.Token},
		Type:        4,
		Content:     content,
		Ephemeral:   true,
	})
}

func (b *Bot) replyEmbed(ctx context.Context, in interactionEvent, embed ddiscord.Embed, buttons []discordapi.Button) error {
	return b.REST.InteractionCallback(ctx, discordapi.Callback{
		Interaction: discordapi.Interaction{ID: in.ID, Token: in.Token},
		Type:        4,
		Embeds:      []ddiscord.Embed{embed},
		Buttons:     buttons,
	})
}

func firstSub(opts []interactionOption) interactionOption {
	if len(opts) == 0 {
		return interactionOption{}
	}
	return opts[0]
}

func optionString(sub interactionOption, name string) string {
	for _, o := range sub.Options {
		if o.Name == name {
			return rawString(o.Value)
		}
	}
	return ""
}

func optionInt(sub interactionOption, name string) int {
	for _, o := range sub.Options {
		if o.Name == name {
			n, _ := strconv.Atoi(rawString(o.Value))
			return n
		}
	}
	return 0
}

func optionIntFrom(opts []interactionOption, name string) int {
	for _, o := range opts {
		if o.Name == name {
			n, _ := strconv.Atoi(rawString(o.Value))
			return n
		}
	}
	return 0
}

func optionUser(opts []interactionOption, name string) string {
	for _, o := range opts {
		if o.Name == name {
			return rawString(o.Value)
		}
		if s := optionUser(o.Options, name); s != "" {
			return s
		}
	}
	return ""
}

func rawString(raw codec.RawMessage) string {
	s := strings.Trim(string(raw), `"`)
	return s
}

func slashCatalog() []discordapi.AppCommand {
	user := discordapi.AppCommandOption{Type: 6, Name: "user", Description: "Member", Required: true}
	return []discordapi.AppCommand{
		{
			Name: "ticket", Description: "Support tickets",
			Options: []discordapi.AppCommandOption{
				{Type: 1, Name: "open", Description: "Open a private ticket"},
				{Type: 1, Name: "close", Description: "Close this ticket"},
				{Type: 1, Name: "panel", Description: "Post the ticket button"},
			},
		},
		{
			Name: "voice", Description: "Manage your temporary voice channel",
			Options: []discordapi.AppCommandOption{
				{Type: 1, Name: "name", Description: "Rename", Options: []discordapi.AppCommandOption{
					{Type: 3, Name: "name", Description: "New name", Required: true},
				}},
				{Type: 1, Name: "limit", Description: "User limit", Options: []discordapi.AppCommandOption{
					{Type: 4, Name: "count", Description: "Max users", Required: true},
				}},
				{Type: 1, Name: "lock", Description: "Lock the channel"},
				{Type: 1, Name: "unlock", Description: "Unlock the channel"},
			},
		},
		{Name: "timeout", Description: "Timeout a member", Options: []discordapi.AppCommandOption{
			user,
			{Type: 4, Name: "minutes", Description: "Duration", Required: true},
		}},
		{Name: "kick", Description: "Kick a member", Options: []discordapi.AppCommandOption{user}},
		{Name: "ban", Description: "Ban a member", Options: []discordapi.AppCommandOption{user}},
		{Name: "purge", Description: "Bulk-delete messages", Options: []discordapi.AppCommandOption{
			{Type: 4, Name: "count", Description: "2–100", Required: true},
		}},
		{Name: "daily", Description: "Claim daily crumbs"},
		{Name: "rank", Description: "Show crumb rank", Options: []discordapi.AppCommandOption{
			{Type: 6, Name: "user", Description: "Member"},
		}},
	}
}
