// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package setup_test

import (
	"context"
	"strings"

	"ItsBagelBot/app/discord/outgress/internal/setup"
	discapi "ItsBagelBot/internal/discordapi"
	"ItsBagelBot/internal/discordstore"

	"go.uber.org/zap"
)

type panelPost struct {
	Post    discapi.EmbedPost
	Buttons []discapi.Button
}

type fakeDiscord struct {
	channelGuilds map[string]string
	channels      []discapi.Snowflake
	roles         []discapi.Snowflake
	everyonePerms string
	botRoles      []string
	deleteErr     error
	guildErr      error

	createdChannels map[string]discapi.ChannelCreate
	createdRoles    []discapi.RoleCreate
	panels          []panelPost
	deleted         []discapi.Message
	chats           []discapi.ChatPost
	guildLookups    int
	listCalls       int
}

func (f *fakeDiscord) GetChannel(_ context.Context, id string) (discapi.ChannelInfo, error) {
	guild, ok := f.channelGuilds[id]
	if !ok {
		guild = "guild-1"
	}
	return discapi.ChannelInfo{ID: id, GuildID: guild}, nil
}

func (f *fakeDiscord) SendChat(_ context.Context, post discapi.ChatPost) error {
	f.chats = append(f.chats, post)
	return nil
}

func (f *fakeDiscord) SendPanel(_ context.Context, post discapi.EmbedPost, buttons []discapi.Button) (discapi.Message, error) {
	f.panels = append(f.panels, panelPost{Post: post, Buttons: buttons})
	return discapi.Message{ChannelID: post.ChannelID, ID: "panel-in-" + post.ChannelID}, nil
}

func (f *fakeDiscord) DeleteMessage(_ context.Context, m discapi.Message) error {
	f.deleted = append(f.deleted, m)
	return f.deleteErr
}

func (f *fakeDiscord) CreateChannel(_ context.Context, ch discapi.GuildChannel) (discapi.Snowflake, error) {
	name := strings.ToLower(ch.Spec.Name)
	if f.createdChannels == nil {
		f.createdChannels = map[string]discapi.ChannelCreate{}
	}
	f.createdChannels[name] = ch.Spec
	created := discapi.Snowflake{ID: "ch-" + name, Name: ch.Spec.Name, Type: ch.Spec.Type}
	f.channels = append(f.channels, created)
	return created, nil
}

func (f *fakeDiscord) CreateRole(_ context.Context, role discapi.GuildRole) (discapi.Snowflake, error) {
	f.createdRoles = append(f.createdRoles, role.Spec)
	return discapi.Snowflake{ID: "role-" + strings.ToLower(role.Spec.Name), Name: role.Spec.Name}, nil
}

func (f *fakeDiscord) ListGuildChannels(context.Context, discapi.Guild) ([]discapi.Snowflake, error) {
	f.listCalls++
	return append([]discapi.Snowflake(nil), f.channels...), nil
}

func (f *fakeDiscord) ListGuildRoles(context.Context, discapi.Guild) ([]discapi.Snowflake, error) {
	everyone := discapi.Snowflake{ID: "guild-1", Name: "@everyone", Permissions: f.everyonePerms}
	return append([]discapi.Snowflake{everyone}, f.roles...), nil
}

func (f *fakeDiscord) GetGuildWithCounts(_ context.Context, g discapi.Guild) (discapi.GuildInfo, error) {
	f.guildLookups++
	if f.guildErr != nil {
		return discapi.GuildInfo{}, f.guildErr
	}
	return discapi.GuildInfo{ID: g.ID, Name: "server " + g.ID, Icon: "abc", ApproximateMemberCount: 42}, nil
}

func (f *fakeDiscord) GetGuildMember(context.Context, discapi.GuildMember) (discapi.GuildMemberInfo, error) {
	return discapi.GuildMemberInfo{Roles: f.botRoles}, nil
}

func (f *fakeDiscord) roleNames() []string {
	var out []string
	for _, r := range f.createdRoles {
		out = append(out, r.Name)
	}
	return out
}

type owners map[string]string

func boundStore(bindings owners) *discordstore.Mem {
	store := discordstore.NewMem()
	for guild, broadcaster := range bindings {
		store.PutGuild(discordstore.Guild{ID: guild}, discordstore.Broadcaster{ID: broadcaster})
	}
	return store
}

func newWorker(discord *fakeDiscord, store discordstore.Store) *setup.Worker {
	return setup.New(setup.Config{Discord: discord, Store: store, Log: zap.NewNop()})
}

type cacheOnlyStore struct{ discordstore.Store }

func (c cacheOnlyStore) BindingOf(ctx context.Context, g discordstore.Guild) (discordstore.Broadcaster, discordstore.BindingSource, bool) {
	b, ok := c.Store.Broadcaster(ctx, g)
	return b, discordstore.BindingFromCache, ok
}

type unavailableStore struct{ discordstore.Store }

func (unavailableStore) GuildsOf(context.Context, discordstore.Broadcaster) ([]discordstore.Binding, error) {
	return nil, discordstore.ErrStoreUnavailable
}
