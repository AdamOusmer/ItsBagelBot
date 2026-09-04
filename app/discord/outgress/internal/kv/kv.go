// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package kv is outgress's go-live message tracker: it remembers the
// message id SendEmbed returns so a later stream.offline can edit it in
// place. Ported from app/dingress/internal/egress/kv.go, narrowed to just
// this one keyspace -- the guild->broadcaster reverse index that used to
// live alongside it (PutGuild/GetGuild/DeleteGuild) moved to
// internal/discordstore, which engine now also reads (see that package's
// doc for why one copy of that key matters more once two processes touch
// it). This package stays outgress-local because nothing outside the
// live/offline RPC handler (see ../rpc/engine_rpc.go) ever needs it: it is
// keyed on a message id only outgress's own SendEmbed call ever learns.
package kv

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

// LiveStore remembers the go-live message so stream.offline can edit it.
type LiveStore interface {
	PutLiveMessage(ctx context.Context, guildID string, m discapi.Message) error
	GetLiveMessage(ctx context.Context, guildID string) (discapi.Message, bool)
	DeleteLiveMessage(ctx context.Context, guildID string) error
}

type valkeyLiveStore struct {
	client valkey.Client
}

// New builds the Valkey-backed LiveStore. A nil client (Valkey unreachable
// at boot) returns a nil store rather than panicking later -- callers
// already nil-check before every use, matching outgress's original
// newValkeyDiscordLive.
func New(client valkey.Client) LiveStore {
	if client == nil {
		return nil
	}
	return valkeyLiveStore{client: client}
}

// liveKey is keyed by GUILD id, not the Twitch broadcaster id the original
// keyed on: outgress's RPC surface (see internal/domain/rpc/discordoutgress)
// only ever carries the guild id engine already resolved, and a guild binds
// to exactly one broadcaster, so this loses no information while dropping a
// field neither side otherwise needs to agree on.
func liveKey(guildID string) string { return "discord:live-msg:" + guildID }

func (s valkeyLiveStore) PutLiveMessage(ctx context.Context, guildID string, m discapi.Message) error {
	return s.client.Do(ctx, s.client.B().Set().Key(liveKey(guildID)).
		Value(m.ChannelID+"|"+m.ID).Ex(liveMessageTTL).Build()).Error()
}

func (s valkeyLiveStore) GetLiveMessage(ctx context.Context, guildID string) (discapi.Message, bool) {
	raw, err := s.client.Do(ctx, s.client.B().Get().Key(liveKey(guildID)).Build()).ToString()
	if err != nil || raw == "" {
		return discapi.Message{}, false
	}
	ch, id, ok := strings.Cut(raw, "|")
	if !ok || ch == "" || id == "" {
		return discapi.Message{}, false
	}
	return discapi.Message{ChannelID: ch, ID: id}, true
}

func (s valkeyLiveStore) DeleteLiveMessage(ctx context.Context, guildID string) error {
	return s.client.Do(ctx, s.client.B().Del().Key(liveKey(guildID)).Build()).Error()
}

// reauthKey marks a guild whose bot role predates CHANGE_NICKNAME.
func reauthKey(guildID string) string { return "discord:reauth:" + guildID }

// ReauthStore records guilds where the per-guild rename was refused, so the
// dashboard can ask that streamer to re-authorize.
//
// This is learned from Discord's own 403 rather than computed from role
// permissions. Computing it would mean fetching the guild's roles, the bot's
// member roles, and folding the permission bits ourselves, and being subtly
// wrong there means either nagging streamers who are fine or staying silent
// for the ones who are not. The refusal is unambiguous and free: we already
// made the call.
type ReauthStore interface {
	MarkNeedsReauth(ctx context.Context, guildID string) error
	ClearNeedsReauth(ctx context.Context, guildID string) error
	NeedsReauth(ctx context.Context, guildID string) bool
}

// NewReauthStore builds the Valkey-backed store.
func NewReauthStore(client valkey.Client) ReauthStore { return valkeyReauth{client: client} }

type valkeyReauth struct{ client valkey.Client }

// MarkNeedsReauth records the refusal. No TTL: the missing permission is
// frozen into the bot's role at install and does not lapse on its own, so an
// expiring flag would just make the prompt blink in and out until someone
// acts on it.
func (s valkeyReauth) MarkNeedsReauth(ctx context.Context, guildID string) error {
	return s.client.Do(ctx, s.client.B().Set().Key(reauthKey(guildID)).Value("1").Build()).Error()
}

// ClearNeedsReauth is called once a rename succeeds, which is the only proof
// the permission actually arrived.
func (s valkeyReauth) ClearNeedsReauth(ctx context.Context, guildID string) error {
	return s.client.Do(ctx, s.client.B().Del().Key(reauthKey(guildID)).Build()).Error()
}

func (s valkeyReauth) NeedsReauth(ctx context.Context, guildID string) bool {
	n, err := s.client.Do(ctx, s.client.B().Exists().Key(reauthKey(guildID)).Build()).AsInt64()
	return err == nil && n > 0
}
