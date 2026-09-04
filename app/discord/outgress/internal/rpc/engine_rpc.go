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
	"ItsBagelBot/pkg/bus"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"
)

// engineHandleTimeout bounds one channel-management or go-live call: a
// handful of REST calls at most, so this is generous, matching
// rpcclient.Client's own client-side timeout on the engine side.
const engineHandleTimeout = 8 * time.Second

// engineREST is the REST slice engine_rpc.go's handlers need.
type engineREST interface {
	CreateChannel(ctx context.Context, ch discapi.GuildChannel) (discapi.Snowflake, error)
	DeleteChannel(ctx context.Context, ch discapi.Snowflake) error
	ModifyChannel(ctx context.Context, patch discapi.ChannelPatch) error
	MoveMember(ctx context.Context, move discapi.VoiceMove) error
	ListMessages(ctx context.Context, q discapi.MessageQuery) ([]discapi.Snowflake, error)
	BulkDeleteMessages(ctx context.Context, p discapi.Purge) error
	SendEmbed(ctx context.Context, post discapi.EmbedPost) (discapi.Message, error)
	EditMessage(ctx context.Context, m discapi.Message, patch discapi.MessagePatch) error
}

// EngineWiring is what SubscribeEngine needs from main.
type EngineWiring struct {
	NC     *nats.Conn
	Prefix string
	Queue  string
	App    *newrelic.Application
	Log    *zap.Logger
}

// SubscribeEngine wires the internal channel-management and go-live RPC
// engine calls for the operations internal/domain/rpc/discordoutgress
// exists to cover (see that package's doc).
func SubscribeEngine(rest engineREST, live kv.LiveStore, wire EngineWiring) error {
	h := &engineRPC{rest: rest, live: live, log: wire.Log}
	if err := bus.QueueSubscribeJSON[discordoutgress.ChannelCreateRequest, discordoutgress.ChannelCreateReply](
		wire.NC, wire.Prefix+".channel.create", wire.Queue, engineHandleTimeout, wire.App, wire.Log, h.handleCreate); err != nil {
		return err
	}
	if err := bus.QueueSubscribeJSON[discordoutgress.ChannelDeleteRequest, discordoutgress.ChannelDeleteReply](
		wire.NC, wire.Prefix+".channel.delete", wire.Queue, engineHandleTimeout, wire.App, wire.Log, h.handleDelete); err != nil {
		return err
	}
	if err := bus.QueueSubscribeJSON[discordoutgress.ChannelModifyRequest, discordoutgress.ChannelModifyReply](
		wire.NC, wire.Prefix+".channel.modify", wire.Queue, engineHandleTimeout, wire.App, wire.Log, h.handleModify); err != nil {
		return err
	}
	if err := bus.QueueSubscribeJSON[discordoutgress.MemberMoveRequest, discordoutgress.MemberMoveReply](
		wire.NC, wire.Prefix+".member.move", wire.Queue, engineHandleTimeout, wire.App, wire.Log, h.handleMove); err != nil {
		return err
	}
	if err := bus.QueueSubscribeJSON[discordoutgress.PurgeRequest, discordoutgress.PurgeReply](
		wire.NC, wire.Prefix+".channel.purge", wire.Queue, engineHandleTimeout, wire.App, wire.Log, h.handlePurge); err != nil {
		return err
	}
	if err := bus.QueueSubscribeJSON[discordoutgress.LiveOnlineRequest, discordoutgress.LiveOnlineReply](
		wire.NC, wire.Prefix+".live.online", wire.Queue, engineHandleTimeout, wire.App, wire.Log, h.handleLiveOnline); err != nil {
		return err
	}
	return bus.QueueSubscribeJSON[discordoutgress.LiveOfflineRequest, discordoutgress.LiveOfflineReply](
		wire.NC, wire.Prefix+".live.offline", wire.Queue, engineHandleTimeout, wire.App, wire.Log, h.handleLiveOffline)
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

// handlePurge lists then bulk-deletes: the two REST calls a Command cannot
// bundle into one (see internal/domain/rpc/discordoutgress's doc). Deleted
// reports how many ids the list call found and were sent to bulk-delete,
// even below Discord's 2-message minimum -- the caller (engine's /purge
// slash handler) is the one that decides whether that count means anything
// worth telling the moderator.
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

// handleLiveOnline is discordOnline moved onto the RPC boundary unchanged:
// a stream already announced (the live-message key still resolves) is a
// no-op, so a repeat call for the same stream never double-posts.
func (h *engineRPC) handleLiveOnline(ctx context.Context, req discordoutgress.LiveOnlineRequest) discordoutgress.LiveOnlineReply {
	if h.live == nil {
		return discordoutgress.LiveOnlineReply{}
	}
	if _, known := h.live.GetLiveMessage(ctx, req.GuildID); known {
		return discordoutgress.LiveOnlineReply{}
	}
	msg, err := h.rest.SendEmbed(ctx, discapi.EmbedPost{ChannelID: req.ChannelID, Embed: req.Embed})
	if err != nil {
		return discordoutgress.LiveOnlineReply{Error: err.Error()}
	}
	if err := h.live.PutLiveMessage(ctx, req.GuildID, msg); err != nil {
		h.log.Warn("go-live message tracking failed", zap.String("guild_id", req.GuildID), zap.Error(err))
	}
	return discordoutgress.LiveOnlineReply{}
}

// handleLiveOffline is discordOffline moved onto the RPC boundary
// unchanged: a message the streamer deleted (404) is forgotten too, so the
// stale key is not re-edited on every later offline event.
func (h *engineRPC) handleLiveOffline(ctx context.Context, req discordoutgress.LiveOfflineRequest) discordoutgress.LiveOfflineReply {
	if h.live == nil {
		return discordoutgress.LiveOfflineReply{}
	}
	msg, known := h.live.GetLiveMessage(ctx, req.GuildID)
	if !known {
		return discordoutgress.LiveOfflineReply{}
	}
	err := h.rest.EditMessage(ctx, msg, discapi.MessagePatch{Content: ddiscord.OfflineContent, Embeds: []ddiscord.Embed{}})
	if keepLiveMessage(err) {
		return discordoutgress.LiveOfflineReply{Error: err.Error()}
	}
	_ = h.live.DeleteLiveMessage(ctx, req.GuildID)
	return discordoutgress.LiveOfflineReply{}
}

func keepLiveMessage(err error) bool {
	if err == nil {
		return false
	}
	return !errors.Is(err, discapi.ErrChannelNotFound)
}
