// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

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

type REST interface {
	InteractionCallback(ctx context.Context, cb discordapi.Callback) error
}

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

type Relay struct {
	REST REST
	Pub  bus.Publisher
	Log  *zap.Logger
}

var _ gateway.Handler = (*Relay)(nil)

func (r *Relay) Ready(_ context.Context, ident gateway.Identity) error {
	r.log().Info("discord gateway ready", zap.String("application_id", ident.ApplicationID))
	return nil
}

func (r *Relay) Dispatch(ctx context.Context, ev gateway.Event) error {
	subject, ok := subjectByType[ev.Type]
	if !ok {
		return nil
	}
	if ev.Type == "INTERACTION_CREATE" {
		if err := r.deferInteraction(ctx, ev.Raw); err != nil {
			r.log().Warn("interaction defer failed", zap.Error(err))
			return nil
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
