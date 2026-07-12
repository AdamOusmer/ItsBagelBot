// Package rpc exposes the outgress management API over NATS request-reply,
// mirroring the bagel.rpc.* conventions of the other services. It covers the
// two things an operator manages here: channels (enable, disable, mod
// status) and the system itself (kill switch, token health).
package rpc

import (
	"context"
	"time"

	"ItsBagelBot/app/outgress/internal/channels"
	"ItsBagelBot/app/outgress/internal/twitch"
	"ItsBagelBot/internal/domain/rpc/manage"
	outgressrpc "ItsBagelBot/internal/domain/rpc/outgress"
	"ItsBagelBot/pkg/bus"

	"github.com/newrelic/go-agent/v3/newrelic"

	"github.com/nats-io/nats.go"

	"go.uber.org/zap"
)

const handleTimeout = 1500 * time.Millisecond
const followageHandleTimeout = 4 * time.Second

type Manage struct {
	registry *channels.Registry
	twitch   *twitch.Client
	log      *zap.Logger
}

// SubscribeManage registers the management endpoints under prefix:
//
//	<prefix>.channel.get     {broadcaster_id}                  -> {channel, found}
//	<prefix>.channel.set     {broadcaster_id, enabled?, is_mod?} -> {channel, found}
//	<prefix>.channel.list    {}                                -> {channels}
//	<prefix>.accountage.get  {target_id?, target_login?}       -> {created_at, user_found}
//	<prefix>.system.status   {}                                -> {paused, token health}
//	<prefix>.system.pause    {paused}                          -> {paused}
func SubscribeManage(nc *nats.Conn, registry *channels.Registry, tw *twitch.Client, prefix, queueGroup string, app *newrelic.Application, log *zap.Logger) error {

	m := &Manage{registry: registry, twitch: tw, log: log}

	if err := bus.QueueSubscribeJSON[manage.ChannelRequest, manage.ChannelReply](nc, prefix+".channel.get", queueGroup, handleTimeout, app, log, m.handleChannelGet); err != nil {
		return err
	}
	if err := bus.QueueSubscribeJSON[manage.ChannelRequest, manage.ChannelReply](nc, prefix+".channel.set", queueGroup, handleTimeout, app, log, m.handleChannelSet); err != nil {
		return err
	}
	if err := bus.QueueSubscribeJSON[struct{}, manage.ChannelListReply](nc, prefix+".channel.list", queueGroup, handleTimeout, app, log, m.handleChannelList); err != nil {
		return err
	}
	if err := bus.QueueSubscribeJSON[struct{}, manage.SystemStatusReply](nc, prefix+".system.status", queueGroup, handleTimeout, app, log, m.handleSystemStatus); err != nil {
		return err
	}
	if err := bus.QueueSubscribeJSON[outgressrpc.FollowageRequest, outgressrpc.FollowageReply](nc, prefix+".followage.get", queueGroup, followageHandleTimeout, app, log, m.handleFollowage); err != nil {
		return err
	}
	if err := bus.QueueSubscribeJSON[outgressrpc.AccountAgeRequest, outgressrpc.AccountAgeReply](nc, prefix+".accountage.get", queueGroup, handleTimeout, app, log, m.handleAccountAge); err != nil {
		return err
	}
	if err := bus.QueueSubscribeJSON[manage.SystemPauseRequest, manage.SystemPauseReply](nc, prefix+".system.pause", queueGroup, handleTimeout, app, log, m.handleSystemPause); err != nil {
		return err
	}

	return nil
}

func (m *Manage) handleFollowage(ctx context.Context, req outgressrpc.FollowageRequest) outgressrpc.FollowageReply {
	if !validFollowageRequest(req) {
		return outgressrpc.FollowageReply{Error: "bad request"}
	}
	targetID, err := m.resolveFollowageTarget(ctx, req)
	if err != nil {
		return outgressrpc.FollowageReply{Error: "lookup failed"}
	}
	return m.readFollowage(ctx, req.BroadcasterID, targetID)
}

func validFollowageRequest(req outgressrpc.FollowageRequest) bool {
	if req.BroadcasterID == "" {
		return false
	}
	return req.TargetID != "" || req.TargetLogin != ""
}

func (m *Manage) resolveFollowageTarget(ctx context.Context, req outgressrpc.FollowageRequest) (string, error) {
	if req.TargetID != "" {
		return req.TargetID, nil
	}
	targetID, err := m.twitch.UserIDByLogin(ctx, req.TargetLogin)
	if err != nil {
		m.log.Warn("followage target resolve failed", zap.Error(err))
	}
	return targetID, err
}

func (m *Manage) readFollowage(ctx context.Context, broadcasterID, targetID string) outgressrpc.FollowageReply {
	if targetID == "" {
		return outgressrpc.FollowageReply{UserFound: false}
	}
	if targetID == broadcasterID {
		return outgressrpc.FollowageReply{TargetID: targetID, UserFound: true}
	}
	return m.fetchFollowage(ctx, broadcasterID, targetID)
}

func (m *Manage) fetchFollowage(ctx context.Context, broadcasterID, targetID string) outgressrpc.FollowageReply {
	followedAt, following, err := m.twitch.FollowedAt(ctx, broadcasterID, targetID)
	if err != nil {
		m.log.Warn("followage lookup failed", zap.Error(err))
		return outgressrpc.FollowageReply{TargetID: targetID, UserFound: true, Error: "lookup failed"}
	}
	return outgressrpc.FollowageReply{
		TargetID: targetID, UserFound: true, Following: following, FollowedAt: followedAt,
	}
}

func (m *Manage) handleAccountAge(ctx context.Context, req outgressrpc.AccountAgeRequest) outgressrpc.AccountAgeReply {
	if req.TargetID == "" && req.TargetLogin == "" {
		return outgressrpc.AccountAgeReply{Error: "bad request"}
	}
	id, createdAt, found, err := m.twitch.UserCreatedAt(ctx, req.TargetID, req.TargetLogin)
	if err != nil {
		m.log.Warn("accountage lookup failed", zap.Error(err))
		return outgressrpc.AccountAgeReply{Error: "lookup failed"}
	}
	if !found {
		return outgressrpc.AccountAgeReply{UserFound: false}
	}
	return outgressrpc.AccountAgeReply{TargetID: id, UserFound: true, CreatedAt: createdAt}
}

func (m *Manage) handleChannelGet(ctx context.Context, req manage.ChannelRequest) manage.ChannelReply {
	if req.BroadcasterID == "" {
		return manage.ChannelReply{Error: "bad request"}
	}

	ch, found, err := m.registry.Get(ctx, req.BroadcasterID)
	if err != nil {
		m.log.Error("channel get failed", zap.Error(err))
		return manage.ChannelReply{Error: "lookup failed"}
	}

	reply := manage.ChannelReply{Found: found}
	if found {
		reply.Channel = &ch
	}
	return reply
}

func (m *Manage) handleChannelSet(ctx context.Context, req manage.ChannelRequest) manage.ChannelReply {
	if req.BroadcasterID == "" {
		return manage.ChannelReply{Error: "bad request"}
	}

	ch, found, err := m.registry.Get(ctx, req.BroadcasterID)
	if err != nil {
		m.log.Error("channel set lookup failed", zap.Error(err))
		return manage.ChannelReply{Error: "lookup failed"}
	}

	if !found {
		ch = manage.Channel{BroadcasterID: req.BroadcasterID, Enabled: true}
	}
	if req.Enabled != nil {
		ch.Enabled = *req.Enabled
	}
	if req.IsMod != nil {
		ch.IsMod = *req.IsMod
		// An operator override counts as a verification, so the workers
		// trust it for the full TTL instead of re-checking immediately.
		ch.ModCheckedAt = time.Now()
	}

	if err := m.registry.Save(ctx, ch); err != nil {
		m.log.Error("channel set failed", zap.Error(err))
		return manage.ChannelReply{Error: "save failed"}
	}

	return manage.ChannelReply{Channel: &ch, Found: true}
}

func (m *Manage) handleChannelList(ctx context.Context, _ struct{}) manage.ChannelListReply {
	list, err := m.registry.List(ctx)
	if err != nil {
		m.log.Error("channel list failed", zap.Error(err))
		return manage.ChannelListReply{Error: "list failed"}
	}

	return manage.ChannelListReply{Channels: list}
}

func (m *Manage) handleSystemStatus(ctx context.Context, _ struct{}) manage.SystemStatusReply {
	paused, err := m.registry.Paused(ctx)
	if err != nil {
		m.log.Error("system status failed", zap.Error(err))
		return manage.SystemStatusReply{Error: "status failed"}
	}

	return manage.SystemStatusReply{
		Paused:                   paused,
		AppTokenExpiresInSeconds: int64(m.twitch.AppTokenExpiresIn().Seconds()),
		HasUserToken:             m.twitch.HasUserToken(),
	}
}

func (m *Manage) handleSystemPause(ctx context.Context, req manage.SystemPauseRequest) manage.SystemPauseReply {
	if err := m.registry.SetPaused(ctx, req.Paused); err != nil {
		m.log.Error("system pause failed", zap.Error(err))
		return manage.SystemPauseReply{Error: "pause failed"}
	}

	m.log.Info("outgress pause state changed", zap.Bool("paused", req.Paused))
	return manage.SystemPauseReply{Paused: req.Paused}
}
