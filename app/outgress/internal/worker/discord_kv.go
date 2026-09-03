// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"strings"
	"time"

	discapi "ItsBagelBot/internal/discordapi"
	"ItsBagelBot/internal/projection"

	"github.com/valkey-io/valkey-go"
)

// liveMessageTTL bounds how long the go-live message id is remembered. It
// is refreshed on every go-live touch, so it only has to outlast one
// stream: 7 days covers a subathon, where the first cut's 36 h left the
// post stuck on LIVE.
const liveMessageTTL = 7 * 24 * time.Hour

// liveMsgKey is the Valkey key for one broadcaster's go-live message.
type liveMsgKey struct {
	BroadcasterID string
}

// discordUser is one Twitch user id the Discord module is keyed by.
type discordUser struct {
	ID uint64
}

// discordModuleReader is the GetModule slice live/clip announcers need.
// *projection.Store implements it; tests inject a map. The signature
// matches projection.Store so production can assign streamInfo directly.
type discordModuleReader interface {
	GetModule(ctx context.Context, userID uint64, name string) (projection.ModuleView, bool, error)
}

// discordLiveStore remembers the go-live message so stream.offline can
// edit it, and the guild→Twitch reverse index dingress needs. Production
// uses Valkey; tests use a map.
type discordLiveStore interface {
	PutLiveMessage(ctx context.Context, key liveMsgKey, m discapi.Message) error
	GetLiveMessage(ctx context.Context, key liveMsgKey) (discapi.Message, bool)
	DeleteLiveMessage(ctx context.Context, key liveMsgKey) error
	PutGuild(ctx context.Context, req GuildSetupRequest) error
	GetGuild(ctx context.Context, req GuildSetupRequest) (broadcasterID string, ok bool)
	DeleteGuild(ctx context.Context, req GuildSetupRequest) error
}

type valkeyDiscordLive struct {
	client valkey.Client
}

func NewDiscordLiveStore(client valkey.Client) discordLiveStore {
	return newValkeyDiscordLive(client)
}

func newValkeyDiscordLive(client valkey.Client) discordLiveStore {
	if client == nil {
		return nil
	}
	return valkeyDiscordLive{client: client}
}

func discordLiveKey(key liveMsgKey) string { return "discord:live-msg:" + key.BroadcasterID }

func discordGuildKey(req GuildSetupRequest) string { return "discord:guild:" + req.GuildID }

func (s valkeyDiscordLive) PutLiveMessage(ctx context.Context, key liveMsgKey, m discapi.Message) error {
	return s.client.Do(ctx, s.client.B().Set().Key(discordLiveKey(key)).
		Value(m.ChannelID+"|"+m.ID).Ex(liveMessageTTL).Build()).Error()
}

func (s valkeyDiscordLive) GetLiveMessage(ctx context.Context, key liveMsgKey) (discapi.Message, bool) {
	raw, err := s.client.Do(ctx, s.client.B().Get().Key(discordLiveKey(key)).Build()).ToString()
	if err != nil {
		return discapi.Message{}, false
	}
	ch, id, ok := strings.Cut(raw, "|")
	if !ok {
		return discapi.Message{}, false
	}
	if ch == "" {
		return discapi.Message{}, false
	}
	if id == "" {
		return discapi.Message{}, false
	}
	return discapi.Message{ChannelID: ch, ID: id}, true
}

func (s valkeyDiscordLive) DeleteLiveMessage(ctx context.Context, key liveMsgKey) error {
	return s.client.Do(ctx, s.client.B().Del().Key(discordLiveKey(key)).Build()).Error()
}

func (s valkeyDiscordLive) PutGuild(ctx context.Context, req GuildSetupRequest) error {
	if req.GuildID == "" {
		return nil
	}
	if req.BroadcasterID == "" {
		return nil
	}
	return s.client.Do(ctx, s.client.B().Set().Key(discordGuildKey(req)).
		Value(req.BroadcasterID).Build()).Error()
}

func (s valkeyDiscordLive) GetGuild(ctx context.Context, req GuildSetupRequest) (string, bool) {
	raw, err := s.client.Do(ctx, s.client.B().Get().Key(discordGuildKey(req)).Build()).ToString()
	if err != nil {
		return "", false
	}
	if raw == "" {
		return "", false
	}
	return raw, true
}

func (s valkeyDiscordLive) DeleteGuild(ctx context.Context, req GuildSetupRequest) error {
	return s.client.Do(ctx, s.client.B().Del().Key(discordGuildKey(req)).Build()).Error()
}
