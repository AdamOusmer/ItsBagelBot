// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"strconv"
	"strings"

	"ItsBagelBot/app/discord/engine/internal/cmd"
	"ItsBagelBot/app/discord/engine/internal/decode"
	"ItsBagelBot/app/discord/engine/module"
	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"
)

const (
	unknownAuthor  = "Unknown author"
	unknownContent = "Unknown (not cached)"
	emptyText      = "(empty)"
	fieldMaxRunes  = 1000
	namePrefix     = "="
)

type logEntry struct {
	Title           string
	Body            string
	Footer          string
	Fields          []ddiscord.EmbedField
	SourceChannelID string
}

func logTo(c *module.Context, emit module.Emit, cat ddiscord.LogCategory, entry logEntry) {
	if !c.Config.LogCategoryOn(cat) || c.Config.LogIgnores(entry.SourceChannelID) {
		return
	}
	channelID := c.Config.LogChannelFor(cat)
	if channelID == "" {
		return
	}
	emit(cmd.PostEmbed(cmd.ChannelTarget(c.Config.GuildID, channelID), ddiscord.LogEmbed(ddiscord.LogLine{
		Category: cat, Title: entry.Title, Body: entry.Body, Fields: entry.Fields, Footer: entry.Footer,
	})))
}

type logsModule struct {
	store discordstore.Store
}

type logHandler[T any] func(ctx context.Context, c *module.Context, emit module.Emit, ev T) error

func logEvent[T any](cat ddiscord.LogCategory, fn logHandler[T]) module.Handler {
	return func(ctx context.Context, c *module.Context, emit module.Emit) error {
		if !c.Config.LogCategoryOn(cat) {
			return nil
		}
		ev, err := decode.Decode[T](c.Event.Raw)
		if err != nil {
			return err
		}
		return fn(ctx, c, emit, ev)
	}
}

func Logs(store discordstore.Store) module.Module {
	h := logsModule{store: store}
	b := module.NewModule("logs")
	b.On("MESSAGE_DELETE", logEvent(ddiscord.LogMessages, h.messageDelete))
	b.On("MESSAGE_UPDATE", logEvent(ddiscord.LogMessages, h.messageEdit))
	b.On("MESSAGE_DELETE_BULK", logEvent(ddiscord.LogMessages, messageBulkDelete))
	b.On("GUILD_MEMBER_ADD", logEvent(ddiscord.LogMembers, memberJoined))
	b.On("GUILD_MEMBER_REMOVE", logEvent(ddiscord.LogMembers, memberLeft))
	b.On("GUILD_MEMBER_UPDATE", logEvent(ddiscord.LogMembers, h.memberUpdate))
	b.On("GUILD_BAN_ADD", logEvent(ddiscord.LogModeration, banned))
	b.On("GUILD_BAN_REMOVE", logEvent(ddiscord.LogModeration, unbanned))
	b.On("CHANNEL_CREATE", logEvent(ddiscord.LogChannels, h.channelCreated))
	b.On("CHANNEL_UPDATE", logEvent(ddiscord.LogChannels, h.channelUpdated))
	b.On("CHANNEL_DELETE", logEvent(ddiscord.LogChannels, channelDeleted))
	b.On("THREAD_CREATE", logEvent(ddiscord.LogChannels, threadCreated))
	b.On("THREAD_DELETE", logEvent(ddiscord.LogChannels, threadDeleted))
	b.On("GUILD_ROLE_CREATE", logEvent(ddiscord.LogRoles, h.roleCreated))
	b.On("GUILD_ROLE_UPDATE", logEvent(ddiscord.LogRoles, h.roleUpdated))
	b.On("GUILD_ROLE_DELETE", logEvent(ddiscord.LogRoles, h.roleDeleted))
	b.On("GUILD_UPDATE", logEvent(ddiscord.LogServer, h.guildUpdated))
	b.On("INVITE_CREATE", logEvent(ddiscord.LogServer, inviteCreated))
	b.On("INVITE_DELETE", logEvent(ddiscord.LogServer, inviteDeleted))
	return b.Build()
}

func skipBot(c *module.Context, bot bool) bool {
	return bot && c.Config.LogIgnoreBotsOn()
}

func fieldText(s string) string {
	if s == "" {
		return emptyText
	}
	r := []rune(s)
	if len(r) <= fieldMaxRunes {
		return s
	}
	return string(r[:fieldMaxRunes]) + "…"
}

func idFooter(label, id string) string { return label + " " + id }

func channelMention(id string) string { return "<#" + id + ">" }

func roleMentions(ids []string) string {
	if len(ids) == 0 {
		return emptyText
	}
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = "<@&" + id + ">"
	}
	return strings.Join(parts, " ")
}

func beforeAfter(before, after string) []ddiscord.EmbedField {
	return []ddiscord.EmbedField{
		{Name: "Before", Value: fieldText(before)},
		{Name: "After", Value: fieldText(after)},
	}
}

func (h logsModule) trackName(ctx context.Context, ref discordstore.LabelRef, name string) (old string, changed bool) {
	prev, seen := h.store.RecallLabel(ctx, ref)
	h.rememberName(ctx, ref, name)
	if !seen {
		return "", false
	}
	old = strings.TrimPrefix(prev, namePrefix)
	return old, old != name
}

func (h logsModule) rememberName(ctx context.Context, ref discordstore.LabelRef, name string) {
	_ = h.store.RememberLabel(ctx, discordstore.Label{Ref: ref, Name: namePrefix + name})
}

func (h logsModule) recallName(ctx context.Context, ref discordstore.LabelRef) (string, bool) {
	got, ok := h.store.RecallLabel(ctx, ref)
	return strings.TrimPrefix(got, namePrefix), ok
}

func (h logsModule) messageDelete(ctx context.Context, c *module.Context, emit module.Emit, ev decode.MessageEvent) error {
	got, cached := h.store.RecallMessage(ctx, discordstore.Message{ID: ev.ID})
	if cached && skipBot(c, got.Bot) {
		return nil
	}
	logTo(c, emit, ddiscord.LogMessages, logEntry{
		Title:           "Message deleted",
		Fields:          deletedFields(ev.ChannelID, got, cached),
		Footer:          idFooter("Message", ev.ID),
		SourceChannelID: ev.ChannelID,
	})
	return nil
}

func cacheEntry(ev decode.MessageEvent) discordstore.CachedMessage {
	attachments := make([]string, len(ev.Attachments))
	for i, a := range ev.Attachments {
		attachments[i] = a.URL
	}
	return discordstore.CachedMessage{
		ID: ev.ID, GuildID: ev.GuildID, ChannelID: ev.ChannelID,
		AuthorID: ev.Author.ID, AuthorName: decode.DisplayName(decode.Display{User: ev.Author}),
		Bot: ev.Author.Bot, Content: ev.Content, Attachments: attachments,
	}
}

func deletedFields(channelID string, got discordstore.CachedMessage, cached bool) []ddiscord.EmbedField {
	author, content := unknownAuthor, unknownContent
	if cached {
		author, content = decode.Mention(decode.UserRef{ID: got.AuthorID}), got.Content
	}
	fields := []ddiscord.EmbedField{
		{Name: "Author", Value: author, Inline: true},
		{Name: "Channel", Value: channelMention(channelID), Inline: true},
		{Name: "Content", Value: fieldText(content)},
	}
	if len(got.Attachments) > 0 {
		fields = append(fields, ddiscord.EmbedField{Name: "Attachments", Value: fieldText(strings.Join(got.Attachments, "\n"))})
	}
	return fields
}

func (h logsModule) messageEdit(ctx context.Context, c *module.Context, emit module.Emit, ev decode.MessageEvent) error {
	if ev.Content == "" {
		return nil
	}
	got, cached := h.store.RecallMessage(ctx, discordstore.Message{ID: ev.ID})
	if skipBot(c, ev.Author.Bot || got.Bot) || (cached && got.Content == ev.Content) {
		return nil
	}
	h.logEdit(c, emit, ev, got, cached)
	h.refresh(ctx, ev, got)
	return nil
}

func (h logsModule) logEdit(c *module.Context, emit module.Emit, ev decode.MessageEvent, got discordstore.CachedMessage, cached bool) {
	before := unknownContent
	if cached {
		before = got.Content
	}
	logTo(c, emit, ddiscord.LogMessages, logEntry{
		Title:           "Message edited",
		Body:            "Edited in " + channelMention(ev.ChannelID),
		Fields:          append([]ddiscord.EmbedField{editAuthor(ev, got)}, beforeAfter(before, ev.Content)...),
		Footer:          idFooter("Message", ev.ID),
		SourceChannelID: ev.ChannelID,
	})
}

func (h logsModule) refresh(ctx context.Context, ev decode.MessageEvent, got discordstore.CachedMessage) {
	entry := cacheEntry(ev)
	if entry.AuthorID == "" {
		entry.AuthorID, entry.AuthorName, entry.Bot = got.AuthorID, got.AuthorName, got.Bot
	}
	if entry.AuthorID == "" {
		return
	}
	_ = h.store.RememberMessage(ctx, entry)
}

func editAuthor(ev decode.MessageEvent, got discordstore.CachedMessage) ddiscord.EmbedField {
	id := ev.Author.ID
	if id == "" {
		id = got.AuthorID
	}
	if id == "" {
		return ddiscord.EmbedField{Name: "Author", Value: unknownAuthor}
	}
	return ddiscord.EmbedField{Name: "Author", Value: decode.Mention(decode.UserRef{ID: id})}
}

func messageBulkDelete(_ context.Context, c *module.Context, emit module.Emit, ev decode.BulkDeleteEvent) error {
	logTo(c, emit, ddiscord.LogMessages, logEntry{
		Title:           "Messages bulk deleted",
		Body:            strconv.Itoa(len(ev.IDs)) + " messages deleted in " + channelMention(ev.ChannelID),
		SourceChannelID: ev.ChannelID,
	})
	return nil
}

func memberJoined(_ context.Context, c *module.Context, emit module.Emit, ev decode.MemberEvent) error {
	logMember(c, emit, "Member joined", ev)
	return nil
}

func memberLeft(_ context.Context, c *module.Context, emit module.Emit, ev decode.MemberEvent) error {
	logMember(c, emit, "Member left", ev)
	return nil
}

func logMember(c *module.Context, emit module.Emit, title string, ev decode.MemberEvent) {
	if skipBot(c, ev.User.Bot) {
		return
	}
	shown := decode.DisplayName(decode.Display{User: ev.User, Nick: ev.Nick})
	logTo(c, emit, ddiscord.LogMembers, logEntry{
		Title: title, Body: decode.Mention(ev.User) + " (" + shown + ")", Footer: idFooter("User", ev.User.ID),
	})
}

func (h logsModule) memberUpdate(ctx context.Context, c *module.Context, emit module.Emit, ev decode.MemberUpdateEvent) error {
	if skipBot(c, ev.User.Bot) {
		return nil
	}
	h.nickChange(ctx, c, emit, ev)
	h.roleChange(ctx, c, emit, ev)
	return nil
}

func (h logsModule) nickChange(ctx context.Context, c *module.Context, emit module.Emit, ev decode.MemberUpdateEvent) {
	ref := discordstore.LabelRef{Kind: discordstore.LabelNick, GuildID: ev.GuildID, ID: ev.User.ID}
	old, changed := h.trackName(ctx, ref, ev.Nick)
	if !changed {
		return
	}
	logTo(c, emit, ddiscord.LogMembers, logEntry{
		Title: "Nickname changed", Body: decode.Mention(ev.User),
		Fields: beforeAfter(old, ev.Nick), Footer: idFooter("User", ev.User.ID),
	})
}

func (h logsModule) roleChange(ctx context.Context, c *module.Context, emit module.Emit, ev decode.MemberUpdateEvent) {
	member := discordstore.Member{GuildID: ev.GuildID, UserID: ev.User.ID}
	prev, seen := h.store.RecallRoles(ctx, member)
	_ = h.store.RememberRoles(ctx, discordstore.MemberRoles{Member: member, Roles: ev.Roles})
	if !seen {
		return
	}
	added, removed := missingFrom(ev.Roles, prev), missingFrom(prev, ev.Roles)
	if len(added) == 0 && len(removed) == 0 {
		return
	}
	logTo(c, emit, ddiscord.LogMembers, logEntry{
		Title: "Roles changed", Body: decode.Mention(ev.User),
		Fields: roleFields(added, removed), Footer: idFooter("User", ev.User.ID),
	})
}

func roleFields(added, removed []string) []ddiscord.EmbedField {
	var fields []ddiscord.EmbedField
	if len(added) > 0 {
		fields = append(fields, ddiscord.EmbedField{Name: "Added", Value: roleMentions(added)})
	}
	if len(removed) > 0 {
		fields = append(fields, ddiscord.EmbedField{Name: "Removed", Value: roleMentions(removed)})
	}
	return fields
}

func missingFrom(have, base []string) []string {
	var out []string
	for _, id := range have {
		if !decode.HasRole(base, id) {
			out = append(out, id)
		}
	}
	return out
}

func banned(_ context.Context, c *module.Context, emit module.Emit, ev decode.BanEvent) error {
	logBan(c, emit, "Member banned", ev)
	return nil
}

func unbanned(_ context.Context, c *module.Context, emit module.Emit, ev decode.BanEvent) error {
	logBan(c, emit, "Member unbanned", ev)
	return nil
}

func logBan(c *module.Context, emit module.Emit, title string, ev decode.BanEvent) {
	shown := decode.DisplayName(decode.Display{User: ev.User})
	logTo(c, emit, ddiscord.LogModeration, logEntry{
		Title: title, Body: decode.Mention(ev.User) + " (" + shown + ")", Footer: idFooter("User", ev.User.ID),
	})
}

func channelRef(guildID, id string) discordstore.LabelRef {
	return discordstore.LabelRef{Kind: discordstore.LabelChan, GuildID: guildID, ID: id}
}

func (h logsModule) channelCreated(ctx context.Context, c *module.Context, emit module.Emit, ev decode.ChannelEvent) error {
	h.rememberName(ctx, channelRef(ev.GuildID, ev.ID), ev.Name)
	logTo(c, emit, ddiscord.LogChannels, logEntry{
		Title: "Channel created", Body: channelMention(ev.ID) + " (" + ev.Name + ")",
		Footer: idFooter("Channel", ev.ID), SourceChannelID: ev.ID,
	})
	return nil
}

func (h logsModule) channelUpdated(ctx context.Context, c *module.Context, emit module.Emit, ev decode.ChannelEvent) error {
	old, changed := h.trackName(ctx, channelRef(ev.GuildID, ev.ID), ev.Name)
	if !changed {
		return nil
	}
	logTo(c, emit, ddiscord.LogChannels, logEntry{
		Title: "Channel renamed", Body: channelMention(ev.ID),
		Fields: beforeAfter(old, ev.Name), Footer: idFooter("Channel", ev.ID), SourceChannelID: ev.ID,
	})
	return nil
}

func channelDeleted(_ context.Context, c *module.Context, emit module.Emit, ev decode.ChannelEvent) error {
	logTo(c, emit, ddiscord.LogChannels, logEntry{
		Title: "Channel deleted", Body: nameOrID(ev.Name, ev.ID),
		Footer: idFooter("Channel", ev.ID), SourceChannelID: ev.ID,
	})
	return nil
}

func threadCreated(_ context.Context, c *module.Context, emit module.Emit, ev decode.ChannelEvent) error {
	logTo(c, emit, ddiscord.LogChannels, logEntry{
		Title: "Thread created", Body: channelMention(ev.ID) + " (" + ev.Name + ")",
		Footer: idFooter("Thread", ev.ID), SourceChannelID: ev.ParentID,
	})
	return nil
}

func threadDeleted(_ context.Context, c *module.Context, emit module.Emit, ev decode.ChannelEvent) error {
	logTo(c, emit, ddiscord.LogChannels, logEntry{
		Title: "Thread deleted", Body: nameOrID(ev.Name, ev.ID),
		Footer: idFooter("Thread", ev.ID), SourceChannelID: ev.ParentID,
	})
	return nil
}

func nameOrID(name, id string) string {
	if name != "" {
		return name
	}
	return id
}

func roleRef(guildID, id string) discordstore.LabelRef {
	return discordstore.LabelRef{Kind: discordstore.LabelRole, GuildID: guildID, ID: id}
}

func (h logsModule) roleCreated(ctx context.Context, c *module.Context, emit module.Emit, ev decode.RoleEvent) error {
	h.rememberName(ctx, roleRef(ev.GuildID, ev.Role.ID), ev.Role.Name)
	logTo(c, emit, ddiscord.LogRoles, logEntry{
		Title: "Role created", Body: "<@&" + ev.Role.ID + "> (" + ev.Role.Name + ")", Footer: idFooter("Role", ev.Role.ID),
	})
	return nil
}

func (h logsModule) roleUpdated(ctx context.Context, c *module.Context, emit module.Emit, ev decode.RoleEvent) error {
	old, changed := h.trackName(ctx, roleRef(ev.GuildID, ev.Role.ID), ev.Role.Name)
	if !changed {
		return nil
	}
	logTo(c, emit, ddiscord.LogRoles, logEntry{
		Title: "Role renamed", Body: "<@&" + ev.Role.ID + ">",
		Fields: beforeAfter(old, ev.Role.Name), Footer: idFooter("Role", ev.Role.ID),
	})
	return nil
}

func (h logsModule) roleDeleted(ctx context.Context, c *module.Context, emit module.Emit, ev decode.RoleDeleteEvent) error {
	name, _ := h.recallName(ctx, roleRef(ev.GuildID, ev.RoleID))
	logTo(c, emit, ddiscord.LogRoles, logEntry{
		Title: "Role deleted", Body: nameOrID(name, ev.RoleID), Footer: idFooter("Role", ev.RoleID),
	})
	return nil
}

func (h logsModule) guildUpdated(ctx context.Context, c *module.Context, emit module.Emit, ev decode.GuildUpdateEvent) error {
	ref := discordstore.LabelRef{Kind: discordstore.LabelGuild, GuildID: ev.ID, ID: ev.ID}
	old, changed := h.trackName(ctx, ref, ev.Name)
	if !changed {
		return nil
	}
	logTo(c, emit, ddiscord.LogServer, logEntry{
		Title: "Server renamed", Fields: beforeAfter(old, ev.Name), Footer: idFooter("Server", ev.ID),
	})
	return nil
}

func inviteCreated(_ context.Context, c *module.Context, emit module.Emit, ev decode.InviteEvent) error {
	body := "Invite " + ev.Code + " for " + channelMention(ev.ChannelID)
	if ev.Inviter.ID != "" {
		body += " by " + decode.Mention(decode.UserRef{ID: ev.Inviter.ID})
	}
	logTo(c, emit, ddiscord.LogServer, logEntry{Title: "Invite created", Body: body, Footer: idFooter("Invite", ev.Code)})
	return nil
}

func inviteDeleted(_ context.Context, c *module.Context, emit module.Emit, ev decode.InviteEvent) error {
	logTo(c, emit, ddiscord.LogServer, logEntry{
		Title: "Invite deleted", Body: "Invite " + ev.Code + " for " + channelMention(ev.ChannelID), Footer: idFooter("Invite", ev.Code),
	})
	return nil
}
