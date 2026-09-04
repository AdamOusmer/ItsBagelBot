// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package relay is app/discord/ingress's gateway.Handler: it wraps every
// dispatched event in a discord.Event and publishes it, and does nothing
// else. See internal/domain/discord's Event doc for why ingress never acts
// on what it reads -- the gateway is a singleton connection, and any REST
// call made inline here would block event reception behind Discord's REST
// latency during exactly the traffic spike (a raid) the rest of the system
// exists to react to.
//
// The one exception, INTERACTION_CREATE, is handled in ack.go.
package relay

import (
	"context"
	"time"

	"ItsBagelBot/app/discord/ingress/internal/gateway"
	"ItsBagelBot/internal/discordapi"
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"go.uber.org/zap"
)

// REST is the one Discord call relay is allowed to make: acknowledging an
// interaction within Discord's 3s deadline. Everything else in
// internal/discordapi is deliberately absent from this interface.
type REST interface {
	InteractionCallback(ctx context.Context, cb discordapi.Callback) error
}

// subjectByType maps a gateway dispatch type to the ingress subject it rides.
// An event type absent here is one the gateway session never asks for (see
// gateway.Intents) or one this bot does not yet act on; Dispatch drops it
// rather than guessing a subject for it.
var subjectByType = map[string]string{
	"GUILD_MEMBER_ADD":             ddiscord.SubjectEventMember,
	"GUILD_MEMBER_REMOVE":          ddiscord.SubjectEventMember,
	"VOICE_STATE_UPDATE":           ddiscord.SubjectEventVoice,
	"MESSAGE_CREATE":               ddiscord.SubjectEventMessage,
	"MESSAGE_UPDATE":               ddiscord.SubjectEventMessage,
	"MESSAGE_DELETE":               ddiscord.SubjectEventMessage,
	"INTERACTION_CREATE":           ddiscord.SubjectEventInteraction,
	"GUILD_AUDIT_LOG_ENTRY_CREATE": ddiscord.SubjectEventAudit,
	"GUILD_CREATE":                 ddiscord.SubjectEventGuild,
}

// Relay implements gateway.Handler.
type Relay struct {
	REST REST
	Pub  bus.Publisher
	Log  *zap.Logger
}

var _ gateway.Handler = (*Relay)(nil)

// Ready records nothing REST-shaped: the application id used to matter here
// (slash-command registration), but that REST call moved to outgress, which
// learns its own application id directly from Discord (GET
// /oauth2/applications/@me) rather than waiting on this event to reach it
// through engine. Ready exists only to satisfy gateway.Handler.
func (r *Relay) Ready(_ context.Context, ident gateway.Identity) error {
	r.log().Info("discord gateway ready", zap.String("application_id", ident.ApplicationID))
	return nil
}

// Dispatch wraps ev in a discord.Event and publishes it on the subject its
// type maps to. INTERACTION_CREATE is deferred inline first (see ack.go);
// every other type is a pure wrap-and-publish.
func (r *Relay) Dispatch(ctx context.Context, ev gateway.Event) error {
	subject, ok := subjectByType[ev.Type]
	if !ok {
		return nil
	}
	if ev.Type == "INTERACTION_CREATE" {
		if err := r.deferInteraction(ctx, ev.Raw); err != nil {
			r.log().Warn("interaction defer failed", zap.Error(err))
			return nil // do not publish work ingress never acknowledged
		}
	}
	return r.publish(ctx, subject, ev)
}

func (r *Relay) publish(ctx context.Context, subject string, ev gateway.Event) error {
	guildID, channelID, userID := routeFields(ev.Type, ev.Raw)
	event := ddiscord.Event{
		Type:             ev.Type,
		GuildID:          guildID,
		ChannelID:        channelID,
		UserID:           userID,
		Raw:              ev.Raw,
		ReceivedAtUnixMs: time.Now().UnixMilli(),
	}
	if r.Pub == nil {
		return nil
	}
	return bus.PublishJSON(ctx, r.Pub, subject, event)
}

func (r *Relay) log() *zap.Logger {
	if r.Log != nil {
		return r.Log
	}
	return zap.NewNop()
}

// idPayload lifts the routing ids (guild/channel/user) out of a gateway
// event without decoding anything business-shaped -- ingress has no
// business logic to decode for. Every field is best-effort: a shape it does
// not match just leaves that id empty, which the engine's own full decode
// (see app/discord/engine's modules) is unaffected by, since Event.Raw
// carries the untouched payload regardless.
type idPayload struct {
	ID      string `json:"id"`
	GuildID string `json:"guild_id"`
	Channel string `json:"channel_id"`
	UserID  string `json:"user_id"`
	Author  struct {
		ID string `json:"id"`
	} `json:"author"`
	Member struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	} `json:"member"`
	User struct {
		ID string `json:"id"`
	} `json:"user"`
}

// routeFields extracts GuildID/ChannelID/UserID for the given event type.
func routeFields(eventType string, raw []byte) (guildID, channelID, userID string) {
	var p idPayload
	_ = codec.Unmarshal(raw, &p)
	guildID = p.GuildID
	if eventType == "GUILD_CREATE" {
		guildID = p.ID
	}
	return guildID, p.Channel, firstNonEmpty(p.UserID, p.Author.ID, p.Member.User.ID, p.User.ID)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
