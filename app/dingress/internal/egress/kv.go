// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package egress

import (
	"context"
	"strings"
	"time"

	discapi "ItsBagelBot/internal/discordapi"

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

// liveStore remembers the go-live message so stream.offline can edit it,
// and the guild->Twitch reverse index the guild-setup RPC needs. Production
// uses Valkey; tests use a map. Ported from outgress's discordLiveStore
// (app/outgress/internal/worker/discord_kv.go) unchanged in shape.
type liveStore interface {
	PutLiveMessage(ctx context.Context, key liveMsgKey, m discapi.Message) error
	GetLiveMessage(ctx context.Context, key liveMsgKey) (discapi.Message, bool)
	DeleteLiveMessage(ctx context.Context, key liveMsgKey) error
	PutGuild(ctx context.Context, req GuildSetupRequest) error
	GetGuild(ctx context.Context, req GuildSetupRequest) (broadcasterID string, ok bool)
	DeleteGuild(ctx context.Context, req GuildSetupRequest) error
	PutTicketDesk(ctx context.Context, guild discapi.Guild) error
}

type valkeyLiveStore struct {
	client valkey.Client
}

// NewLiveStore builds the Valkey-backed liveStore. A nil client (Valkey
// unreachable at boot) returns a nil store rather than panicking later --
// callers already nil-check w.discordKV before every use, matching
// outgress's newValkeyDiscordLive.
func NewLiveStore(client valkey.Client) liveStore {
	if client == nil {
		return nil
	}
	return valkeyLiveStore{client: client}
}

func discordLiveKey(key liveMsgKey) string { return "discord:live-msg:" + key.BroadcasterID }

func discordGuildKey(req GuildSetupRequest) string { return "discord:guild:" + req.GuildID }

func discordTicketDeskKey(guild discapi.Guild) string { return "discord:ticketdesk:" + guild.ID }

func (s valkeyLiveStore) PutLiveMessage(ctx context.Context, key liveMsgKey, m discapi.Message) error {
	return s.client.Do(ctx, s.client.B().Set().Key(discordLiveKey(key)).
		Value(m.ChannelID+"|"+m.ID).Ex(liveMessageTTL).Build()).Error()
}

func (s valkeyLiveStore) GetLiveMessage(ctx context.Context, key liveMsgKey) (discapi.Message, bool) {
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

func (s valkeyLiveStore) DeleteLiveMessage(ctx context.Context, key liveMsgKey) error {
	return s.client.Do(ctx, s.client.B().Del().Key(discordLiveKey(key)).Build()).Error()
}

func (s valkeyLiveStore) PutGuild(ctx context.Context, req GuildSetupRequest) error {
	if req.GuildID == "" {
		return nil
	}
	if req.BroadcasterID == "" {
		return nil
	}
	return s.client.Do(ctx, s.client.B().Set().Key(discordGuildKey(req)).
		Value(req.BroadcasterID).Build()).Error()
}

func (s valkeyLiveStore) GetGuild(ctx context.Context, req GuildSetupRequest) (string, bool) {
	raw, err := s.client.Do(ctx, s.client.B().Get().Key(discordGuildKey(req)).Build()).ToString()
	if err != nil {
		return "", false
	}
	if raw == "" {
		return "", false
	}
	return raw, true
}

func (s valkeyLiveStore) DeleteGuild(ctx context.Context, req GuildSetupRequest) error {
	return s.client.Do(ctx, s.client.B().Del().Key(discordGuildKey(req)).Build()).Error()
}

func (s valkeyLiveStore) PutTicketDesk(ctx context.Context, guild discapi.Guild) error {
	if guild.ID == "" {
		return nil
	}
	return s.client.Do(ctx, s.client.B().Set().Key(discordTicketDeskKey(guild)).Value("1").Build()).Error()
}
