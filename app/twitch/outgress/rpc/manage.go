// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"time"

	"ItsBagelBot/app/twitch/outgress/internal/channels"
	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	"ItsBagelBot/internal/domain/rpc/manage"
	outgressrpc "ItsBagelBot/internal/domain/rpc/outgress"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/monitor"

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

func SubscribeManage(nc *nats.Conn, registry *channels.Registry, tw *twitch.Client, prefix, queueGroup string, app *newrelic.Application, log *zap.Logger) error {

	m := &Manage{registry: registry, twitch: tw, log: log}

	return subscribeAll(
		func() error {
			return bus.QueueSubscribeJSON[manage.ChannelRequest, manage.ChannelReply](nc, prefix+".channel.get", queueGroup, handleTimeout, app, log, m.handleChannelGet)
		},
		func() error {
			return bus.QueueSubscribeJSON[manage.ChannelRequest, manage.ChannelReply](nc, prefix+".channel.set", queueGroup, handleTimeout, app, log, m.handleChannelSet)
		},
		func() error {
			return bus.QueueSubscribeJSON[struct{}, manage.ChannelListReply](nc, prefix+".channel.list", queueGroup, handleTimeout, app, log, m.handleChannelList)
		},
		func() error {
			return bus.QueueSubscribeJSON[struct{}, manage.SystemStatusReply](nc, prefix+".system.status", queueGroup, handleTimeout, app, log, m.handleSystemStatus)
		},
		func() error {
			return bus.QueueSubscribeJSON[outgressrpc.FollowageRequest, outgressrpc.FollowageReply](nc, prefix+".followage.get", queueGroup, followageHandleTimeout, app, log, m.handleFollowage)
		},
		func() error {
			return bus.QueueSubscribeJSON[outgressrpc.AccountAgeRequest, outgressrpc.AccountAgeReply](nc, prefix+".accountage.get", queueGroup, handleTimeout, app, log, m.handleAccountAge)
		},
		func() error {
			return bus.QueueSubscribeJSON[outgressrpc.UptimeRequest, outgressrpc.UptimeReply](nc, prefix+".uptime.get", queueGroup, followageHandleTimeout, app, log, m.handleUptime)
		},
		func() error {
			return bus.QueueSubscribeJSON[manage.SystemPauseRequest, manage.SystemPauseReply](nc, prefix+".system.pause", queueGroup, handleTimeout, app, log, m.handleSystemPause)
		},
		func() error {
			return bus.QueueSubscribeJSON[outgressrpc.StreamInfoRequest, outgressrpc.StreamInfoReply](nc, prefix+".streaminfo.get", queueGroup, followageHandleTimeout, app, log, m.handleStreamInfo)
		},
		func() error {
			return bus.QueueSubscribeJSON[outgressrpc.ChannelCountsRequest, outgressrpc.ChannelCountsReply](nc, prefix+".channelcounts.get", queueGroup, followageHandleTimeout, app, log, m.handleChannelCounts)
		},
	)
}

func subscribeAll(register ...func() error) error {
	for _, r := range register {
		if err := r(); err != nil {
			return err
		}
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
		m.log.Warn("followage lookup failed", zap.String("broadcaster_id", broadcasterID), zap.Error(err))
		return outgressrpc.FollowageReply{TargetID: targetID, UserFound: true, Error: "lookup failed"}
	}
	return outgressrpc.FollowageReply{
		TargetID: targetID, UserFound: true, Following: following, FollowedAt: followedAt,
	}
}

func (m *Manage) handleAccountAge(ctx context.Context, req outgressrpc.AccountAgeRequest) outgressrpc.AccountAgeReply {
	log := monitor.TxnLogger(ctx, m.log)
	if req.TargetID == "" && req.TargetLogin == "" {
		return outgressrpc.AccountAgeReply{Error: "bad request"}
	}
	id, createdAt, found, err := m.twitch.UserCreatedAt(ctx, req.TargetID, req.TargetLogin)
	if err != nil {
		log.Warn("accountage lookup failed", zap.Error(err))
		return outgressrpc.AccountAgeReply{Error: "lookup failed"}
	}
	if !found {
		return outgressrpc.AccountAgeReply{UserFound: false}
	}
	return outgressrpc.AccountAgeReply{TargetID: id, UserFound: true, CreatedAt: createdAt}
}

func (m *Manage) handleUptime(ctx context.Context, req outgressrpc.UptimeRequest) outgressrpc.UptimeReply {
	if req.BroadcasterID == "" {
		return outgressrpc.UptimeReply{Error: "bad request"}
	}
	startedAt, live, err := m.twitch.StreamStartedAt(ctx, req.BroadcasterID)
	if err != nil {
		m.log.Warn("uptime lookup failed", zap.Error(err))
		return outgressrpc.UptimeReply{Error: "lookup failed"}
	}
	return outgressrpc.UptimeReply{Live: live, StartedAt: startedAt}
}

func (m *Manage) handleStreamInfo(ctx context.Context, req outgressrpc.StreamInfoRequest) outgressrpc.StreamInfoReply {
	return readStreamInfo(ctx, m.twitch, m.log, req)
}

func (m *Manage) handleChannelCounts(ctx context.Context, req outgressrpc.ChannelCountsRequest) outgressrpc.ChannelCountsReply {
	return readChannelCounts(ctx, m.twitch, m.log, req)
}

func (m *Manage) handleChannelGet(ctx context.Context, req manage.ChannelRequest) manage.ChannelReply {
	log := monitor.TxnLogger(ctx, m.log)
	if req.BroadcasterID == "" {
		return manage.ChannelReply{Error: "bad request"}
	}

	ch, found, err := m.registry.Get(ctx, req.BroadcasterID)
	if err != nil {
		log.Error("channel get failed", zap.Error(err))
		return manage.ChannelReply{Error: "lookup failed"}
	}

	reply := manage.ChannelReply{Found: found}
	if found {
		reply.Channel = &ch
	}
	return reply
}

func (m *Manage) handleChannelSet(ctx context.Context, req manage.ChannelRequest) manage.ChannelReply {
	log := monitor.TxnLogger(ctx, m.log)
	if req.BroadcasterID == "" {
		return manage.ChannelReply{Error: "bad request"}
	}

	ch, found, err := m.registry.Get(ctx, req.BroadcasterID)
	if err != nil {
		log.Error("channel set lookup failed", zap.Error(err))
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
		ch.ModCheckedAt = time.Now()
	}

	if err := m.registry.Save(ctx, ch); err != nil {
		log.Error("channel set failed", zap.Error(err))
		return manage.ChannelReply{Error: "save failed"}
	}

	return manage.ChannelReply{Channel: &ch, Found: true}
}

func (m *Manage) handleChannelList(ctx context.Context, _ struct{}) manage.ChannelListReply {
	log := monitor.TxnLogger(ctx, m.log)
	list, err := m.registry.List(ctx)
	if err != nil {
		log.Error("channel list failed", zap.Error(err))
		return manage.ChannelListReply{Error: "list failed"}
	}

	return manage.ChannelListReply{Channels: list}
}

func (m *Manage) handleSystemStatus(ctx context.Context, _ struct{}) manage.SystemStatusReply {
	log := monitor.TxnLogger(ctx, m.log)
	paused, err := m.registry.Paused(ctx)
	if err != nil {
		log.Error("system status failed", zap.Error(err))
		return manage.SystemStatusReply{Error: "status failed"}
	}

	return manage.SystemStatusReply{
		Paused:                   paused,
		AppTokenExpiresInSeconds: int64(m.twitch.AppTokenExpiresIn().Seconds()),
		HasUserToken:             m.twitch.HasUserToken(),
	}
}

func (m *Manage) handleSystemPause(ctx context.Context, req manage.SystemPauseRequest) manage.SystemPauseReply {
	log := monitor.TxnLogger(ctx, m.log)
	if err := m.registry.SetPaused(ctx, req.Paused); err != nil {
		log.Error("system pause failed", zap.Error(err))
		return manage.SystemPauseReply{Error: "pause failed"}
	}

	log.Info("outgress pause state changed", zap.Bool("paused", req.Paused))
	return manage.SystemPauseReply{Paused: req.Paused}
}
