// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"
	"time"

	"ItsBagelBot/app/discord/outgress/internal/kv"
	discapi "ItsBagelBot/internal/discordapi"
	ddiscord "ItsBagelBot/internal/domain/discord"
	discordoutgress "ItsBagelBot/internal/domain/rpc/discordoutgress"

	"go.uber.org/zap"
)

const engineHandleTimeout = 8 * time.Second

type engineREST interface {
	CreateChannel(ctx context.Context, ch discapi.GuildChannel) (discapi.Snowflake, error)
	DeleteChannel(ctx context.Context, ch discapi.Snowflake) error
	ModifyChannel(ctx context.Context, patch discapi.ChannelPatch) error
	MoveMember(ctx context.Context, move discapi.VoiceMove) error
	ListMessages(ctx context.Context, q discapi.MessageQuery) ([]discapi.Snowflake, error)
	BulkDeleteMessages(ctx context.Context, p discapi.Purge) error
	SendEmbed(ctx context.Context, post discapi.EmbedPost) (discapi.Message, error)
	EditMessage(ctx context.Context, m discapi.Message, patch discapi.MessagePatch) error
	GetInvite(ctx context.Context, code string) (discapi.Invite, error)
}

func SubscribeEngine(rest engineREST, live kv.LiveStore, wire Wiring) error {
	h := &engineRPC{rest: rest, live: live, log: wire.Log}
	at := func(name string) verb { return verb{Name: name, Timeout: engineHandleTimeout} }
	return errors.Join(
		register[discordoutgress.ChannelCreateRequest, discordoutgress.ChannelCreateReply](
			wire, at("channel.create"), h.handleCreate),
		register[discordoutgress.ChannelDeleteRequest, discordoutgress.ChannelDeleteReply](
			wire, at("channel.delete"), h.handleDelete),
		register[discordoutgress.ChannelModifyRequest, discordoutgress.ChannelModifyReply](
			wire, at("channel.modify"), h.handleModify),
		register[discordoutgress.MemberMoveRequest, discordoutgress.MemberMoveReply](
			wire, at("member.move"), h.handleMove),
		register[discordoutgress.PurgeRequest, discordoutgress.PurgeReply](
			wire, at("channel.purge"), h.handlePurge),
		register[discordoutgress.LiveOnlineRequest, discordoutgress.LiveOnlineReply](
			wire, at("live.online"), h.handleLiveOnline),
		register[discordoutgress.LiveOfflineRequest, discordoutgress.LiveOfflineReply](
			wire, at("live.offline"), h.handleLiveOffline),
		register[discordoutgress.InviteResolveRequest, discordoutgress.InviteResolveReply](
			wire, at("invite.resolve"), h.handleInviteResolve),
	)
}

type engineRPC struct {
	rest engineREST
	live kv.LiveStore
	log  *zap.Logger
}

func (h *engineRPC) handleCreate(ctx context.Context, req discordoutgress.ChannelCreateRequest) discordoutgress.ChannelCreateReply {
	got, err := h.rest.CreateChannel(ctx, discapi.GuildChannel{
		Guild: discapi.Guild{ID: req.GuildID},
		Spec: discapi.ChannelCreate{
			Name: req.Name, Type: req.Type, ParentID: req.ParentID, Topic: req.Topic,
			PermissionOverwrites: req.Overwrites,
		},
	})
	if err != nil {
		return discordoutgress.ChannelCreateReply{Error: err.Error()}
	}
	return discordoutgress.ChannelCreateReply{ChannelID: got.ID}
}

func (h *engineRPC) handleDelete(ctx context.Context, req discordoutgress.ChannelDeleteRequest) discordoutgress.ChannelDeleteReply {
	if err := h.rest.DeleteChannel(ctx, discapi.Snowflake{ID: req.ChannelID}); err != nil {
		return discordoutgress.ChannelDeleteReply{Error: err.Error()}
	}
	return discordoutgress.ChannelDeleteReply{}
}

func (h *engineRPC) handleModify(ctx context.Context, req discordoutgress.ChannelModifyRequest) discordoutgress.ChannelModifyReply {
	err := h.rest.ModifyChannel(ctx, discapi.ChannelPatch{
		ID: req.ChannelID, Name: req.Name, UserLimit: req.UserLimit, PermissionOverwrites: req.Overwrites,
	})
	if err != nil {
		return discordoutgress.ChannelModifyReply{Error: err.Error()}
	}
	return discordoutgress.ChannelModifyReply{}
}

func (h *engineRPC) handleMove(ctx context.Context, req discordoutgress.MemberMoveRequest) discordoutgress.MemberMoveReply {
	err := h.rest.MoveMember(ctx, discapi.VoiceMove{GuildID: req.GuildID, UserID: req.UserID, ChannelID: req.ChannelID})
	if err != nil {
		return discordoutgress.MemberMoveReply{Error: err.Error()}
	}
	return discordoutgress.MemberMoveReply{}
}

func (h *engineRPC) handlePurge(ctx context.Context, req discordoutgress.PurgeRequest) discordoutgress.PurgeReply {
	msgs, err := h.rest.ListMessages(ctx, discapi.MessageQuery{ChannelID: req.ChannelID, Limit: req.Count})
	if err != nil {
		return discordoutgress.PurgeReply{Error: err.Error()}
	}
	ids := make([]string, 0, len(msgs))
	for _, m := range msgs {
		ids = append(ids, m.ID)
	}
	if len(ids) < 2 {
		return discordoutgress.PurgeReply{Deleted: len(ids)}
	}
	if err := h.rest.BulkDeleteMessages(ctx, discapi.Purge{ChannelID: req.ChannelID, MessageIDs: ids}); err != nil {
		return discordoutgress.PurgeReply{Error: err.Error()}
	}
	return discordoutgress.PurgeReply{Deleted: len(ids)}
}

func (h *engineRPC) handleLiveOnline(ctx context.Context, req discordoutgress.LiveOnlineRequest) discordoutgress.LiveOnlineReply {
	if h.live == nil {
		return discordoutgress.LiveOnlineReply{}
	}
	if _, known := h.live.GetLiveMessage(ctx, kv.GuildID(req.GuildID)); known {
		return discordoutgress.LiveOnlineReply{}
	}
	msg, err := h.rest.SendEmbed(ctx, discapi.EmbedPost{ChannelID: req.ChannelID, Embed: req.Embed})
	if err != nil {
		return discordoutgress.LiveOnlineReply{Error: err.Error()}
	}
	if err := h.live.PutLiveMessage(ctx, kv.GuildID(req.GuildID), msg); err != nil {
		h.log.Warn("go-live message tracking failed", zap.String("guild_id", req.GuildID), zap.Error(err))
	}
	return discordoutgress.LiveOnlineReply{}
}

func (h *engineRPC) handleLiveOffline(ctx context.Context, req discordoutgress.LiveOfflineRequest) discordoutgress.LiveOfflineReply {
	if h.live == nil {
		return discordoutgress.LiveOfflineReply{}
	}
	msg, known := h.live.GetLiveMessage(ctx, kv.GuildID(req.GuildID))
	if !known {
		return discordoutgress.LiveOfflineReply{}
	}
	err := h.rest.EditMessage(ctx, msg, discapi.MessagePatch{Content: ddiscord.OfflineContent, Embeds: []ddiscord.Embed{}})
	if keepLiveMessage(err) {
		return discordoutgress.LiveOfflineReply{Error: err.Error()}
	}
	_ = h.live.DeleteLiveMessage(ctx, kv.GuildID(req.GuildID))
	return discordoutgress.LiveOfflineReply{}
}

func (h *engineRPC) handleInviteResolve(ctx context.Context, req discordoutgress.InviteResolveRequest) discordoutgress.InviteResolveReply {
	inv, err := h.rest.GetInvite(ctx, req.Code)
	if err != nil {
		if errors.Is(err, discapi.ErrChannelNotFound) {
			return discordoutgress.InviteResolveReply{NotFound: true}
		}
		return discordoutgress.InviteResolveReply{Error: err.Error()}
	}
	if inv.Guild == nil {
		return discordoutgress.InviteResolveReply{NotFound: true}
	}
	return discordoutgress.InviteResolveReply{GuildID: inv.Guild.ID}
}

func keepLiveMessage(err error) bool {
	if err == nil {
		return false
	}
	return !errors.Is(err, discapi.ErrChannelNotFound)
}
