// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package kv

import (
	"context"
	"strings"
	"time"

	discapi "ItsBagelBot/internal/discordapi"
	ddiscord "ItsBagelBot/internal/domain/discord"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/valkey-io/valkey-go"
)

const liveMessageTTL = 7 * 24 * time.Hour

type GuildID string

type LiveStore interface {
	PutLiveMessage(ctx context.Context, guildID GuildID, m discapi.Message) error
	GetLiveMessage(ctx context.Context, guildID GuildID) (discapi.Message, bool)
	DeleteLiveMessage(ctx context.Context, guildID GuildID) error
}

type valkeyLiveStore struct {
	kv pkg_valkey.KV
}

func New(client valkey.Client) LiveStore {
	if client == nil {
		return nil
	}
	return valkeyLiveStore{kv: pkg_valkey.NewKV(client)}
}

func liveKey(guildID GuildID) string { return "discord:live-msg:" + string(guildID) }

func (s valkeyLiveStore) PutLiveMessage(ctx context.Context, guildID GuildID, m discapi.Message) error {
	at := pkg_valkey.Key{Name: liveKey(guildID), TTL: liveMessageTTL}
	return s.kv.Set(ctx, at, m.ChannelID+"|"+m.ID)
}

func (s valkeyLiveStore) GetLiveMessage(ctx context.Context, guildID GuildID) (discapi.Message, bool) {
	raw, ok := s.kv.GetString(ctx, liveKey(guildID))
	if !ok {
		return discapi.Message{}, false
	}
	ch, id, ok := strings.Cut(raw, "|")
	if malformedLiveMessage(ch, id, ok) {
		return discapi.Message{}, false
	}
	return discapi.Message{ChannelID: ch, ID: id}, true
}

func malformedLiveMessage(ch, id string, ok bool) bool {
	return !ok || ch == "" || id == ""
}

func (s valkeyLiveStore) DeleteLiveMessage(ctx context.Context, guildID GuildID) error {
	return s.kv.Del(ctx, liveKey(guildID))
}

type BotStatusReader interface {
	BotStatus(ctx context.Context) (ddiscord.BotStatus, bool)
}

func NewBotStatusReader(client valkey.Client) BotStatusReader {
	if client == nil {
		return nil
	}
	return valkeyBotStatus{kv: pkg_valkey.NewKV(client)}
}

type valkeyBotStatus struct{ kv pkg_valkey.KV }

func (s valkeyBotStatus) BotStatus(ctx context.Context) (ddiscord.BotStatus, bool) {
	raw, ok := s.kv.GetString(ctx, ddiscord.BotStatusKey)
	if !ok {
		return ddiscord.BotStatus{}, false
	}
	got, err := ddiscord.DecodeBotStatus([]byte(raw))
	if err != nil {
		return ddiscord.BotStatus{}, false
	}
	return got, true
}

func reauthKey(guildID GuildID) string { return "discord:reauth:" + string(guildID) }

type ReauthStore interface {
	MarkNeedsReauth(ctx context.Context, guildID GuildID) error
	ClearNeedsReauth(ctx context.Context, guildID GuildID) error
	NeedsReauth(ctx context.Context, guildID GuildID) bool
}

func NewReauthStore(client valkey.Client) ReauthStore {
	return valkeyReauth{kv: pkg_valkey.NewKV(client)}
}

type valkeyReauth struct{ kv pkg_valkey.KV }

func (s valkeyReauth) MarkNeedsReauth(ctx context.Context, guildID GuildID) error {
	return s.kv.Set(ctx, pkg_valkey.Key{Name: reauthKey(guildID)}, "1")
}

func (s valkeyReauth) ClearNeedsReauth(ctx context.Context, guildID GuildID) error {
	return s.kv.Del(ctx, reauthKey(guildID))
}

func (s valkeyReauth) NeedsReauth(ctx context.Context, guildID GuildID) bool {
	_, marked := s.kv.GetString(ctx, reauthKey(guildID))
	return marked
}

const lockdownTTL = 7 * 24 * time.Hour

type LockdownChannel struct {
	ChannelID string `json:"channel_id"`
	Allow     string `json:"allow"`
	Deny      string `json:"deny"`
}

type LockdownState struct {
	VerificationLevel int               `json:"verification_level"`
	EveryoneRoleID    string            `json:"everyone_role_id,omitempty"`
	Channels          []LockdownChannel `json:"channels,omitempty"`
}

type LockdownStore interface {
	PutLockdown(ctx context.Context, guildID GuildID, state LockdownState) error
	GetLockdown(ctx context.Context, guildID GuildID) (LockdownState, bool)
	DeleteLockdown(ctx context.Context, guildID GuildID) error
}

func NewLockdownStore(client valkey.Client) LockdownStore {
	if client == nil {
		return nil
	}
	return valkeyLockdown{kv: pkg_valkey.NewKV(client)}
}

type valkeyLockdown struct{ kv pkg_valkey.KV }

func lockdownKey(guildID GuildID) string { return "discord:lockdown:" + string(guildID) }

func (s valkeyLockdown) PutLockdown(ctx context.Context, guildID GuildID, state LockdownState) error {
	at := pkg_valkey.Key{Name: lockdownKey(guildID), TTL: lockdownTTL}
	return pkg_valkey.SetJSON(ctx, s.kv, at, state)
}

func (s valkeyLockdown) GetLockdown(ctx context.Context, guildID GuildID) (LockdownState, bool) {
	return pkg_valkey.GetJSON[LockdownState](ctx, s.kv, lockdownKey(guildID))
}

func (s valkeyLockdown) DeleteLockdown(ctx context.Context, guildID GuildID) error {
	return s.kv.Del(ctx, lockdownKey(guildID))
}
