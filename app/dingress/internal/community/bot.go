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
	SendChat(ctx context.Context, post discordapi.ChatPost) error
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
	ListMessages(ctx context.Context, q discordapi.MessageQuery) ([]discordapi.Snowflake, error)
	InteractionCallback(ctx context.Context, cb discordapi.Callback) error
	BulkOverwriteCommands(ctx context.Context, cat discordapi.CommandCatalog) error
}

// Modules loads the Discord module blob for a bound guild.
type Modules interface {
	Config(ctx context.Context, b store.Broadcaster) (ddiscord.Config, bool)
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
func (p ProjectionModules) Config(ctx context.Context, b store.Broadcaster) (ddiscord.Config, bool) {
	id, err := strconv.ParseUint(b.ID, 10, 64)
	if err != nil {
		return ddiscord.Config{}, false
	}
	mod, found, err := p.Src.GetModule(ctx, id, ddiscord.ModuleName)
	if err != nil {
		return ddiscord.Config{}, false
	}
	if !found {
		return ddiscord.Config{}, false
	}
	if !mod.IsEnabled {
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
func (b *Bot) Ready(ctx context.Context, ident gateway.Identity) error {
	b.appID = ident.ApplicationID
	b.botUser = ident.BotUserID
	if ident.ApplicationID == "" {
		return nil
	}
	if b.REST == nil {
		return nil
	}
	return b.REST.BulkOverwriteCommands(ctx, discordapi.CommandCatalog{
		ApplicationID: ident.ApplicationID,
		Commands:      slashCatalog(),
	})
}

type eventHandler func(*Bot, context.Context, []byte) error

var communityEvents = map[string]eventHandler{
	"GUILD_MEMBER_ADD":    (*Bot).onMemberAdd,
	"GUILD_MEMBER_REMOVE": (*Bot).onMemberRemove,
	"VOICE_STATE_UPDATE":  (*Bot).onVoice,
	"MESSAGE_CREATE":      (*Bot).onMessage,
	"MESSAGE_DELETE":      (*Bot).onMessageDelete,
	"MESSAGE_UPDATE":      (*Bot).onMessageUpdate,
	"INTERACTION_CREATE":  (*Bot).onInteraction,
	"GUILD_CREATE":        (*Bot).onGuildCreate,
}

// Dispatch routes one gateway event onto the matching community op.
func (b *Bot) Dispatch(ctx context.Context, ev gateway.Event) error {
	h, ok := communityEvents[ev.Type]
	if !ok {
		return nil
	}
	return h(b, ctx, ev.Raw)
}

func (b *Bot) bound(ctx context.Context, g store.Guild) (ddiscord.Config, bool) {
	if b.Store == nil {
		return ddiscord.Config{}, false
	}
	broadcaster, ok := b.Store.Broadcaster(ctx, g)
	if !ok {
		return ddiscord.Config{}, false
	}
	if b.Modules == nil {
		return ddiscord.Config{}, false
	}
	return b.Modules.Config(ctx, broadcaster)
}

type display struct {
	User userRef
	Nick string
}

func displayName(d display) string {
	if d.Nick != "" {
		return d.Nick
	}
	if d.User.GlobalName != "" {
		return d.User.GlobalName
	}
	if d.User.Username != "" {
		return d.User.Username
	}
	return d.User.ID
}

func avatarURL(user userRef) string {
	if user.ID == "" {
		return ""
	}
	if user.Avatar == "" {
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

type permBits struct{ Raw string }

func canMod(perm permBits) bool {
	n, err := strconv.ParseUint(perm.Raw, 10, 64)
	if err != nil {
		return false
	}
	return n&(permAdmin|permKick|permBan|permModerate) != 0
}

func mention(user userRef) string { return "<@" + user.ID + ">" }

type overwriteSpec struct {
	TargetID string
	Kind     int
	Bits     int64
}

func overwriteAllow(spec overwriteSpec) discordapi.PermissionOverwrite {
	return discordapi.PermissionOverwrite{ID: spec.TargetID, Type: spec.Kind, Allow: strconv.FormatInt(spec.Bits, 10), Deny: "0"}
}

func overwriteDeny(spec overwriteSpec) discordapi.PermissionOverwrite {
	return discordapi.PermissionOverwrite{ID: spec.TargetID, Type: spec.Kind, Allow: "0", Deny: strconv.FormatInt(spec.Bits, 10)}
}

// occupancy tracks who is in which voice channel so empty clones can die.
type occupancy struct {
	mu    sync.Mutex
	in    map[string]map[string]struct{} // channelID -> userIDs
	where map[string]string              // guildID/userID -> channelID
}

type voiceSeat struct {
	GuildID   string
	UserID    string
	ChannelID string
}

func (o *occupancy) update(seat voiceSeat) (left string, leftEmpty bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.in == nil {
		o.in = map[string]map[string]struct{}{}
		o.where = map[string]string{}
	}
	key := seat.GuildID + "/" + seat.UserID
	prev := o.where[key]
	if prev != "" {
		leftEmpty = o.leave(store.Channel{ID: prev}, store.Member{UserID: seat.UserID})
		left = prev
	}
	if seat.ChannelID == "" {
		delete(o.where, key)
		return left, leftEmpty
	}
	if o.in[seat.ChannelID] == nil {
		o.in[seat.ChannelID] = map[string]struct{}{}
	}
	o.in[seat.ChannelID][seat.UserID] = struct{}{}
	o.where[key] = seat.ChannelID
	return left, leftEmpty && prev != seat.ChannelID
}

func (o *occupancy) leave(ch store.Channel, m store.Member) bool {
	delete(o.in[ch.ID], m.UserID)
	if len(o.in[ch.ID]) != 0 {
		return false
	}
	delete(o.in, ch.ID)
	return true
}

var _ gateway.Handler = (*Bot)(nil)
