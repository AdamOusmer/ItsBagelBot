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
	b.On("GUILD_MEMBER_ADD", logEvent(ddiscord.LogMembers, memberLog("Member joined")))
	b.On("GUILD_MEMBER_REMOVE", logEvent(ddiscord.LogMembers, memberLog("Member left")))
	b.On("GUILD_MEMBER_UPDATE", logEvent(ddiscord.LogMembers, h.memberUpdate))
	b.On("GUILD_BAN_ADD", logEvent(ddiscord.LogModeration, banLog("Member banned")))
	b.On("GUILD_BAN_REMOVE", logEvent(ddiscord.LogModeration, banLog("Member unbanned")))
	b.On("CHANNEL_CREATE", logEvent(ddiscord.LogChannels, h.channelLog(channelCreatedNote)))
	b.On("CHANNEL_UPDATE", logEvent(ddiscord.LogChannels, h.channelUpdated))
	b.On("CHANNEL_DELETE", logEvent(ddiscord.LogChannels, h.channelLog(channelDeletedNote)))
	b.On("THREAD_CREATE", logEvent(ddiscord.LogChannels, threadLog(threadCreatedNote)))
	b.On("THREAD_DELETE", logEvent(ddiscord.LogChannels, threadLog(threadDeletedNote)))
	b.On("GUILD_ROLE_CREATE", logEvent(ddiscord.LogRoles, h.roleCreated))
	b.On("GUILD_ROLE_UPDATE", logEvent(ddiscord.LogRoles, h.roleUpdated))
	b.On("GUILD_ROLE_DELETE", logEvent(ddiscord.LogRoles, h.roleDeleted))
	b.On("GUILD_UPDATE", logEvent(ddiscord.LogServer, h.guildUpdated))
	b.On("INVITE_CREATE", logEvent(ddiscord.LogServer, inviteLog("Invite created")))
	b.On("INVITE_DELETE", logEvent(ddiscord.LogServer, inviteLog("Invite deleted")))
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
	if skipBot(c, ev.Author.Bot || got.Bot) {
		return nil
	}
	if cached && got.Content == ev.Content {
		return nil
	}
	h.logEdit(c, emit, ev, recalled{msg: got, cached: cached})
	h.refresh(ctx, ev, got)
	return nil
}

type recalled struct {
	msg    discordstore.CachedMessage
	cached bool
}

func (h logsModule) logEdit(c *module.Context, emit module.Emit, ev decode.MessageEvent, prior recalled) {
	got := prior.msg
	before := unknownContent
	if prior.cached {
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

func memberLog(title string) logHandler[decode.MemberEvent] {
	return func(_ context.Context, c *module.Context, emit module.Emit, ev decode.MemberEvent) error {
		if skipBot(c, ev.User.Bot) {
			return nil
		}
		logTo(c, emit, ddiscord.LogMembers, userEntry(title, ev.User, ev.Nick))
		return nil
	}
}

func userEntry(title string, user decode.UserRef, nick string) logEntry {
	shown := decode.DisplayName(decode.Display{User: user, Nick: nick})
	return logEntry{Title: title, Body: decode.Mention(user) + " (" + shown + ")", Footer: idFooter("User", user.ID)}
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
	h.renamed(ctx, c, emit, nameChange{
		ref:  discordstore.LabelRef{Kind: discordstore.LabelNick, GuildID: ev.GuildID, ID: ev.User.ID},
		name: ev.Nick,
		cat:  ddiscord.LogMembers,
		entry: logEntry{
			Title: "Nickname changed", Body: decode.Mention(ev.User), Footer: idFooter("User", ev.User.ID),
		},
	})
}

type nameChange struct {
	ref   discordstore.LabelRef
	name  string
	cat   ddiscord.LogCategory
	entry logEntry
}

func (h logsModule) renamed(ctx context.Context, c *module.Context, emit module.Emit, nc nameChange) {
	old, changed := h.trackName(ctx, nc.ref, nc.name)
	if !changed {
		return
	}
	nc.entry.Fields = beforeAfter(old, nc.name)
	logTo(c, emit, nc.cat, nc.entry)
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

func banLog(title string) logHandler[decode.BanEvent] {
	return func(_ context.Context, c *module.Context, emit module.Emit, ev decode.BanEvent) error {
		logTo(c, emit, ddiscord.LogModeration, userEntry(title, ev.User, ""))
		return nil
	}
}

func channelRef(guildID, id string) discordstore.LabelRef {
	return discordstore.LabelRef{Kind: discordstore.LabelChan, GuildID: guildID, ID: id}
}

type channelNote struct {
	Title   string
	Kind    string
	Created bool
}

var (
	channelCreatedNote = channelNote{Title: "Channel created", Kind: "Channel", Created: true}
	channelDeletedNote = channelNote{Title: "Channel deleted", Kind: "Channel"}
	threadCreatedNote  = channelNote{Title: "Thread created", Kind: "Thread", Created: true}
	threadDeletedNote  = channelNote{Title: "Thread deleted", Kind: "Thread"}
)

func (n channelNote) entry(ev decode.ChannelEvent, source string) logEntry {
	body := nameOrID(ev.Name, ev.ID)
	if n.Created {
		body = channelMention(ev.ID) + " (" + ev.Name + ")"
	}
	return logEntry{Title: n.Title, Body: body, Footer: idFooter(n.Kind, ev.ID), SourceChannelID: source}
}

func (h logsModule) channelLog(note channelNote) logHandler[decode.ChannelEvent] {
	return func(ctx context.Context, c *module.Context, emit module.Emit, ev decode.ChannelEvent) error {
		if note.Created {
			h.rememberName(ctx, channelRef(ev.GuildID, ev.ID), ev.Name)
		}
		if h.isTempVoiceRoom(ctx, c.Config, ev) {
			return nil
		}
		logTo(c, emit, ddiscord.LogChannels, note.entry(ev, ev.ID))
		return nil
	}
}

func threadLog(note channelNote) logHandler[decode.ChannelEvent] {
	return func(_ context.Context, c *module.Context, emit module.Emit, ev decode.ChannelEvent) error {
		logTo(c, emit, ddiscord.LogChannels, note.entry(ev, ev.ParentID))
		return nil
	}
}

func (h logsModule) isTempVoiceRoom(ctx context.Context, cfg ddiscord.Config, ev decode.ChannelEvent) bool {
	if ev.Type != ddiscord.ChannelVoice {
		return false
	}
	if inVoiceCategory(cfg, ev) {
		return true
	}
	_, tracked := h.store.Clone(ctx, discordstore.Channel{ID: ev.ID})
	return tracked
}

func inVoiceCategory(cfg ddiscord.Config, ev decode.ChannelEvent) bool {
	return cfg.VoiceCategoryID != "" && ev.ParentID == cfg.VoiceCategoryID && ev.ID != cfg.VoiceHubID
}

func (h logsModule) channelUpdated(ctx context.Context, c *module.Context, emit module.Emit, ev decode.ChannelEvent) error {
	h.renamed(ctx, c, emit, nameChange{
		ref:  channelRef(ev.GuildID, ev.ID),
		name: ev.Name,
		cat:  ddiscord.LogChannels,
		entry: logEntry{
			Title: "Channel renamed", Body: channelMention(ev.ID), Footer: idFooter("Channel", ev.ID), SourceChannelID: ev.ID,
		},
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
	h.renamed(ctx, c, emit, nameChange{
		ref:  roleRef(ev.GuildID, ev.Role.ID),
		name: ev.Role.Name,
		cat:  ddiscord.LogRoles,
		entry: logEntry{
			Title: "Role renamed", Body: "<@&" + ev.Role.ID + ">", Footer: idFooter("Role", ev.Role.ID),
		},
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
	h.renamed(ctx, c, emit, nameChange{
		ref:   discordstore.LabelRef{Kind: discordstore.LabelGuild, GuildID: ev.ID, ID: ev.ID},
		name:  ev.Name,
		cat:   ddiscord.LogServer,
		entry: logEntry{Title: "Server renamed", Footer: idFooter("Server", ev.ID)},
	})
	return nil
}

func inviteLog(title string) logHandler[decode.InviteEvent] {
	return func(_ context.Context, c *module.Context, emit module.Emit, ev decode.InviteEvent) error {
		body := "Invite " + ev.Code + " for " + channelMention(ev.ChannelID)
		if ev.Inviter.ID != "" {
			body += " by " + decode.Mention(decode.UserRef{ID: ev.Inviter.ID})
		}
		logTo(c, emit, ddiscord.LogServer, logEntry{Title: title, Body: body, Footer: idFooter("Invite", ev.Code)})
		return nil
	}
}

func logVoiceMove(c *module.Context, emit module.Emit, userID string, move discordstore.VoiceMove) {
	move, show := hideHub(c.Config.VoiceHubID, move)
	if !show {
		return
	}
	title, body, source := voiceMoveText(userID, move)
	logTo(c, emit, ddiscord.LogVoice, logEntry{
		Title: title, Body: body, Footer: idFooter("User", userID), SourceChannelID: source,
	})
}

func hideHub(hub string, move discordstore.VoiceMove) (discordstore.VoiceMove, bool) {
	switch {
	case move.From == move.To:
		return move, false
	case hub == "":
		return move, true
	case move.To == hub, move.From == hub && move.To == "":
		return move, false
	case move.From == hub:
		return discordstore.VoiceMove{To: move.To}, true
	default:
		return move, true
	}
}

func voiceMoveText(userID string, move discordstore.VoiceMove) (title, body, source string) {
	user := decode.Mention(decode.UserRef{ID: userID})
	switch {
	case move.From == "":
		return "Joined voice", user + " joined " + channelMention(move.To), move.To
	case move.To == "":
		return "Left voice", user + " left " + channelMention(move.From), move.From
	default:
		return "Moved voice", user + " moved from " + channelMention(move.From) + " to " + channelMention(move.To), move.To
	}
}
