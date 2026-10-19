// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package relay

import (
	"context"
	"fmt"
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
	"GUILD_DELETE":                 ddiscord.SubjectEventGuild,
	"MESSAGE_DELETE_BULK":          ddiscord.SubjectEventMessage,
	"GUILD_MEMBER_UPDATE":          ddiscord.SubjectEventMember,
	"GUILD_BAN_ADD":                ddiscord.SubjectEventMember,
	"GUILD_BAN_REMOVE":             ddiscord.SubjectEventMember,
	"GUILD_UPDATE":                 ddiscord.SubjectEventGuild,
	"CHANNEL_CREATE":               ddiscord.SubjectEventGuild,
	"CHANNEL_UPDATE":               ddiscord.SubjectEventGuild,
	"CHANNEL_DELETE":               ddiscord.SubjectEventGuild,
	"THREAD_CREATE":                ddiscord.SubjectEventGuild,
	"THREAD_UPDATE":                ddiscord.SubjectEventGuild,
	"THREAD_DELETE":                ddiscord.SubjectEventGuild,
	"GUILD_ROLE_CREATE":            ddiscord.SubjectEventGuild,
	"GUILD_ROLE_UPDATE":            ddiscord.SubjectEventGuild,
	"GUILD_ROLE_DELETE":            ddiscord.SubjectEventGuild,
	"INVITE_CREATE":                ddiscord.SubjectEventGuild,
	"INVITE_DELETE":                ddiscord.SubjectEventGuild,
	"GUILD_EMOJIS_UPDATE":          ddiscord.SubjectEventGuild,
	"GUILD_STICKERS_UPDATE":        ddiscord.SubjectEventGuild,
}

var guildIsID = map[string]bool{"GUILD_CREATE": true, "GUILD_UPDATE": true, "GUILD_DELETE": true}

var channelIsID = map[string]bool{
	"CHANNEL_CREATE": true,
	"CHANNEL_UPDATE": true,
	"CHANNEL_DELETE": true,
	"THREAD_CREATE":  true,
	"THREAD_UPDATE":  true,
	"THREAD_DELETE":  true,
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
	id := ""
	if ev.SessionID != "" {
		id = fmt.Sprintf("%s-%d", ev.SessionID, ev.Seq)
	}
	return r.publish(ctx, outgoing{subject: subject, eventType: ev.Type, raw: ev.Raw, id: id})
}

type outgoing struct {
	subject   string
	eventType string
	raw       []byte
	id        string
}

func (r *Relay) publish(ctx context.Context, out outgoing) error {
	guildID, channelID, userID := routeFields(out.eventType, out.raw)
	event := ddiscord.Event{
		Type:             out.eventType,
		GuildID:          guildID,
		ChannelID:        channelID,
		UserID:           userID,
		Raw:              out.raw,
		ReceivedAtUnixMs: time.Now().UnixMilli(),
	}
	if r.Pub == nil {
		return nil
	}
	if out.id == "" {
		return bus.PublishJSON(ctx, r.Pub, out.subject, event)
	}
	body, err := codec.Marshal(event)
	if err != nil {
		return err
	}
	return bus.PublishConfirmed(ctx, r.Pub, bus.Publication{Subject: out.subject, ID: out.id, Payload: body})
}

func (r *Relay) PublishInteraction(ctx context.Context, raw []byte, id string) error {
	return r.publish(ctx, outgoing{
		subject:   ddiscord.SubjectEventInteraction,
		eventType: "INTERACTION_CREATE",
		raw:       raw,
		id:        "interaction-" + id,
	})
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
	if guildIsID[eventType] {
		guildID = p.ID
	}
	channelID = p.Channel
	if channelIsID[eventType] {
		channelID = p.ID
	}
	return guildID, channelID, firstNonEmpty(p.UserID, p.Author.ID, p.Member.User.ID, p.User.ID)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
