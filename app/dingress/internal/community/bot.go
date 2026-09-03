// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package community

import (
	"context"
	"strconv"
	"sync"

	"ItsBagelBot/app/dingress/internal/gateway"
	"ItsBagelBot/app/dingress/internal/store"
	"ItsBagelBot/internal/discordapi"
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/codec"

	"go.uber.org/zap"
)

const (
	permAdmin    uint64 = 8
	permKick     uint64 = 2
	permBan      uint64 = 4
	permModerate uint64 = 1 << 40
	permConnect  int64  = 1048576
	permView     int64  = 1024
	permSend     int64  = 2048

	customTicketOpen  = "bagel:ticket:open"
	customTicketClose = "bagel:ticket:close"
)

// REST is the Discord write surface community ops need.
type REST interface {
	SendMessage(ctx context.Context, channelID, content string, tts bool) error
	SendEmbed(ctx context.Context, post discordapi.EmbedPost) (discordapi.Message, error)
	SendPanel(ctx context.Context, post discordapi.EmbedPost, buttons []discordapi.Button) (discordapi.Message, error)
	CreateChannel(ctx context.Context, ch discordapi.GuildChannel) (discordapi.Snowflake, error)
	DeleteChannel(ctx context.Context, ch discordapi.Snowflake) error
	AddMemberRole(ctx context.Context, r discordapi.MemberRole) error
	MoveMember(ctx context.Context, move discordapi.VoiceMove) error
	ModifyChannel(ctx context.Context, patch discordapi.ChannelPatch) error
	TimeoutMember(ctx context.Context, t discordapi.MemberTimeout) error
	KickMember(ctx context.Context, m discordapi.GuildMember) error
	BanMember(ctx context.Context, m discordapi.GuildMember) error
	BulkDeleteMessages(ctx context.Context, p discordapi.Purge) error
	ListMessages(ctx context.Context, channelID string, limit int) ([]discordapi.Snowflake, error)
	InteractionCallback(ctx context.Context, cb discordapi.Callback) error
	BulkOverwriteCommands(ctx context.Context, appID string, cmds []discordapi.AppCommand) error
}

// Modules loads the Discord module blob for a bound guild.
type Modules interface {
	Config(ctx context.Context, broadcasterID string) (ddiscord.Config, bool)
}

// Bot handles Discord gateway events for one fleet bot token.
type Bot struct {
	REST    REST
	Store   store.Store
	Modules Modules
	Log     *zap.Logger
	appID   string
	botUser string
	occ     occupancy
}

// ProjectionModules reads the Discord row outgress and the dashboard write.
type ProjectionModules struct {
	Src *projection.Store
}

// Config loads the enabled Discord module for a Twitch broadcaster id.
func (p ProjectionModules) Config(ctx context.Context, broadcasterID string) (ddiscord.Config, bool) {
	id, err := strconv.ParseUint(broadcasterID, 10, 64)
	if err != nil {
		return ddiscord.Config{}, false
	}
	mod, found, err := p.Src.GetModule(ctx, id, ddiscord.ModuleName)
	if err != nil || !found || !mod.IsEnabled {
		return ddiscord.Config{}, false
	}
	cfg := ddiscord.Parse(mod.Configs)
	if !cfg.Connected() {
		return ddiscord.Config{}, false
	}
	return cfg, true
}

func (b *Bot) log() *zap.Logger {
	if b.Log != nil {
		return b.Log
	}
	return zap.NewNop()
}

// Ready records the application id and registers slash commands once.
func (b *Bot) Ready(ctx context.Context, applicationID, botUserID string) error {
	b.appID = applicationID
	b.botUser = botUserID
	if applicationID == "" {
		return nil
	}
	if b.REST == nil {
		return nil
	}
	return b.REST.BulkOverwriteCommands(ctx, applicationID, slashCatalog())
}

// Dispatch routes one gateway event onto the matching community op.
func (b *Bot) Dispatch(ctx context.Context, eventType string, raw []byte) error {
	switch eventType {
	case "GUILD_MEMBER_ADD":
		return b.onMemberAdd(ctx, raw)
	case "GUILD_MEMBER_REMOVE":
		return b.onMemberRemove(ctx, raw)
	case "VOICE_STATE_UPDATE":
		return b.onVoice(ctx, raw)
	case "MESSAGE_CREATE":
		return b.onMessage(ctx, raw)
	case "MESSAGE_DELETE":
		return b.onMessageDelete(ctx, raw)
	case "MESSAGE_UPDATE":
		return b.onMessageUpdate(ctx, raw)
	case "INTERACTION_CREATE":
		return b.onInteraction(ctx, raw)
	case "GUILD_CREATE":
		return b.onGuildCreate(ctx, raw)
	default:
		return nil
	}
}

func (b *Bot) bound(ctx context.Context, guildID string) (ddiscord.Config, bool) {
	if b.Store == nil {
		return ddiscord.Config{}, false
	}
	broadcaster, ok := b.Store.Broadcaster(ctx, guildID)
	if !ok {
		return ddiscord.Config{}, false
	}
	if b.Modules == nil {
		return ddiscord.Config{}, false
	}
	return b.Modules.Config(ctx, broadcaster)
}

func displayName(user userRef, nick string) string {
	if nick != "" {
		return nick
	}
	if user.GlobalName != "" {
		return user.GlobalName
	}
	if user.Username != "" {
		return user.Username
	}
	return user.ID
}

func avatarURL(user userRef) string {
	if user.ID == "" || user.Avatar == "" {
		return ""
	}
	return "https://cdn.discordapp.com/avatars/" + user.ID + "/" + user.Avatar + ".png"
}

type userRef struct {
	ID         string `json:"id"`
	Username   string `json:"username"`
	GlobalName string `json:"global_name"`
	Avatar     string `json:"avatar"`
	Bot        bool   `json:"bot"`
}

type memberEvent struct {
	GuildID string  `json:"guild_id"`
	Nick    string  `json:"nick"`
	User    userRef `json:"user"`
}

type voiceEvent struct {
	GuildID   string `json:"guild_id"`
	ChannelID string `json:"channel_id"`
	UserID    string `json:"user_id"`
	Member    struct {
		User userRef `json:"user"`
	} `json:"member"`
}

type messageEvent struct {
	ID        string  `json:"id"`
	GuildID   string  `json:"guild_id"`
	ChannelID string  `json:"channel_id"`
	Content   string  `json:"content"`
	Author    userRef `json:"author"`
}

type interactionEvent struct {
	ID    string `json:"id"`
	Token string `json:"token"`
	Type  int    `json:"type"`
	Data  struct {
		Name     string              `json:"name"`
		CustomID string              `json:"custom_id"`
		Options  []interactionOption `json:"options"`
	} `json:"data"`
	GuildID   string `json:"guild_id"`
	ChannelID string `json:"channel_id"`
	Member    struct {
		User        userRef `json:"user"`
		Permissions string  `json:"permissions"`
		Nick        string  `json:"nick"`
	} `json:"member"`
}

type interactionOption struct {
	Name    string              `json:"name"`
	Type    int                 `json:"type"`
	Value   codec.RawMessage    `json:"value"`
	Options []interactionOption `json:"options"`
}

func decode[T any](raw []byte) (T, error) {
	var v T
	err := codec.Unmarshal(raw, &v)
	return v, err
}

func canMod(perm string) bool {
	n, err := strconv.ParseUint(perm, 10, 64)
	if err != nil {
		return false
	}
	return n&(permAdmin|permKick|permBan|permModerate) != 0
}

func mention(id string) string { return "<@" + id + ">" }

func overwriteAllow(id string, kind int, allow int64) discordapi.PermissionOverwrite {
	return discordapi.PermissionOverwrite{ID: id, Type: kind, Allow: strconv.FormatInt(allow, 10), Deny: "0"}
}

func overwriteDeny(id string, kind int, deny int64) discordapi.PermissionOverwrite {
	return discordapi.PermissionOverwrite{ID: id, Type: kind, Allow: "0", Deny: strconv.FormatInt(deny, 10)}
}

// occupancy tracks who is in which voice channel so empty clones can die.
type occupancy struct {
	mu    sync.Mutex
	in    map[string]map[string]struct{} // channelID -> userIDs
	where map[string]string              // guildID/userID -> channelID
}

func (o *occupancy) update(guildID, userID, channelID string) (left string, leftEmpty bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.in == nil {
		o.in = map[string]map[string]struct{}{}
		o.where = map[string]string{}
	}
	key := guildID + "/" + userID
	prev := o.where[key]
	if prev != "" {
		delete(o.in[prev], userID)
		if len(o.in[prev]) == 0 {
			delete(o.in, prev)
			leftEmpty = true
		}
		left = prev
	}
	if channelID == "" {
		delete(o.where, key)
		return left, leftEmpty
	}
	if o.in[channelID] == nil {
		o.in[channelID] = map[string]struct{}{}
	}
	o.in[channelID][userID] = struct{}{}
	o.where[key] = channelID
	return left, leftEmpty && prev != channelID
}

var _ gateway.Handler = (*Bot)(nil)
