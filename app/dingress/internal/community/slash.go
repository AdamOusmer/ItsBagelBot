// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package community

import (
	"context"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/internal/discordapi"
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/pkg/codec"
)

func (b *Bot) onInteraction(ctx context.Context, raw []byte) error {
	in, err := decode[interactionEvent](raw)
	if err != nil {
		return err
	}
	cfg, ok := b.bound(ctx, in.GuildID)
	if !ok {
		return nil
	}
	if in.Data.CustomID != "" {
		return b.onTicketButton(ctx, cfg, in)
	}
	return b.slash(ctx, cfg, in)
}

func (b *Bot) slash(ctx context.Context, cfg ddiscord.Config, in interactionEvent) error {
	switch in.Data.Name {
	case "ticket":
		return b.ticketSlash(ctx, cfg, in)
	case "voice":
		return b.voiceSlash(ctx, in)
	case "timeout":
		return b.modTimeout(ctx, cfg, in)
	case "kick":
		return b.modKick(ctx, cfg, in)
	case "ban":
		return b.modBan(ctx, cfg, in)
	case "purge":
		return b.modPurge(ctx, cfg, in)
	case "daily":
		return b.daily(ctx, cfg, in)
	case "rank":
		return b.rank(ctx, cfg, in)
	default:
		return b.reply(ctx, in, "Unknown command.")
	}
}

func (b *Bot) ticketSlash(ctx context.Context, cfg ddiscord.Config, in interactionEvent) error {
	sub := firstSub(in.Data.Options)
	switch sub.Name {
	case "open":
		return b.openTicket(ctx, cfg, in)
	case "close":
		return b.closeTicket(ctx, in)
	case "panel":
		return b.postTicketPanel(ctx, cfg, in)
	default:
		return b.reply(ctx, in, "Use /ticket open, close, or panel.")
	}
}

func (b *Bot) voiceSlash(ctx context.Context, in interactionEvent) error {
	return b.voiceCommand(ctx, in, firstSub(in.Data.Options))
}

func (b *Bot) requireMod(in interactionEvent) bool { return canMod(in.Member.Permissions) }

func (b *Bot) modTimeout(ctx context.Context, cfg ddiscord.Config, in interactionEvent) error {
	if !b.requireMod(in) {
		return b.reply(ctx, in, "Mods only.")
	}
	userID := optionUser(in.Data.Options, "user")
	mins := optionIntFrom(in.Data.Options, "minutes")
	if userID == "" || mins <= 0 {
		return b.reply(ctx, in, "Need a user and a duration in minutes.")
	}
	until := time.Now().UTC().Add(time.Duration(mins) * time.Minute).Format(time.RFC3339)
	err := b.REST.TimeoutMember(ctx, discordapi.MemberTimeout{
		GuildID: in.GuildID, UserID: userID, UntilISO: until,
	})
	if err != nil {
		return err
	}
	_ = b.logLine(ctx, cfg, "Timeout", mention(userID)+" for "+strconv.Itoa(mins)+" minutes")
	return b.reply(ctx, in, "Timed out "+mention(userID)+" for "+strconv.Itoa(mins)+" minutes.")
}

func (b *Bot) modKick(ctx context.Context, cfg ddiscord.Config, in interactionEvent) error {
	if !b.requireMod(in) {
		return b.reply(ctx, in, "Mods only.")
	}
	userID := optionUser(in.Data.Options, "user")
	if userID == "" {
		return b.reply(ctx, in, "Need a user.")
	}
	err := b.REST.KickMember(ctx, discordapi.GuildMember{GuildID: in.GuildID, UserID: userID})
	if err != nil {
		return err
	}
	_ = b.logLine(ctx, cfg, "Kick", mention(userID))
	return b.reply(ctx, in, "Kicked "+mention(userID)+".")
}

func (b *Bot) modBan(ctx context.Context, cfg ddiscord.Config, in interactionEvent) error {
	if !b.requireMod(in) {
		return b.reply(ctx, in, "Mods only.")
	}
	userID := optionUser(in.Data.Options, "user")
	if userID == "" {
		return b.reply(ctx, in, "Need a user.")
	}
	err := b.REST.BanMember(ctx, discordapi.GuildMember{GuildID: in.GuildID, UserID: userID})
	if err != nil {
		return err
	}
	_ = b.logLine(ctx, cfg, "Ban", mention(userID))
	return b.reply(ctx, in, "Banned "+mention(userID)+".")
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
	msgs, err := b.REST.ListMessages(ctx, in.ChannelID, n)
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
	_ = b.logLine(ctx, cfg, "Purge", strconv.Itoa(len(ids))+" messages in <#"+in.ChannelID+">")
	return b.reply(ctx, in, "Deleted "+strconv.Itoa(len(ids))+" messages.")
}

func (b *Bot) daily(ctx context.Context, cfg ddiscord.Config, in interactionEvent) error {
	if !cfg.LevelsOn() {
		return b.reply(ctx, in, "Levels are off.")
	}
	ok, xp := b.Store.ClaimDaily(ctx, in.GuildID, in.Member.User.ID)
	if !ok {
		return b.reply(ctx, in, "Already claimed today.")
	}
	return b.reply(ctx, in, "Daily claimed. You have "+strconv.Itoa(xp)+" crumbs.")
}

func (b *Bot) rank(ctx context.Context, cfg ddiscord.Config, in interactionEvent) error {
	if !cfg.LevelsOn() {
		return b.reply(ctx, in, "Levels are off.")
	}
	userID := optionUser(in.Data.Options, "user")
	if userID == "" {
		userID = in.Member.User.ID
	}
	xp, level := b.Store.Rank(ctx, in.GuildID, userID)
	return b.reply(ctx, in, mention(userID)+" is level "+strconv.Itoa(level)+" ("+strconv.Itoa(xp)+" crumbs).")
}

func (b *Bot) reply(ctx context.Context, in interactionEvent, content string) error {
	return b.REST.InteractionCallback(ctx, discordapi.Callback{
		Interaction: discordapi.Interaction{ID: in.ID, Token: in.Token},
		Type:        4,
		Content:     content,
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
