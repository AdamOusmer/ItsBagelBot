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
	"ItsBagelBot/pkg/bus"

	"github.com/nats-io/nats.go"

	"go.uber.org/zap"
)

const handleTimeout = 1500 * time.Millisecond

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
//	<prefix>.system.status   {}                                -> {paused, token health}
//	<prefix>.system.pause    {paused}                          -> {paused}
func SubscribeManage(nc *nats.Conn, registry *channels.Registry, tw *twitch.Client, prefix, queueGroup string, log *zap.Logger) error {

	m := &Manage{registry: registry, twitch: tw, log: log}

	if err := bus.QueueSubscribeJSON[channelRequest, channelReply](nc, prefix+".channel.get", queueGroup, handleTimeout, log, m.handleChannelGet); err != nil {
		return err
	}
	if err := bus.QueueSubscribeJSON[channelRequest, channelReply](nc, prefix+".channel.set", queueGroup, handleTimeout, log, m.handleChannelSet); err != nil {
		return err
	}
	if err := bus.QueueSubscribeJSON[struct{}, channelListReply](nc, prefix+".channel.list", queueGroup, handleTimeout, log, m.handleChannelList); err != nil {
		return err
	}
	if err := bus.QueueSubscribeJSON[struct{}, systemStatusReply](nc, prefix+".system.status", queueGroup, handleTimeout, log, m.handleSystemStatus); err != nil {
		return err
	}
	if err := bus.QueueSubscribeJSON[systemPauseRequest, systemPauseReply](nc, prefix+".system.pause", queueGroup, handleTimeout, log, m.handleSystemPause); err != nil {
		return err
	}

	return nil
}

type channelRequest struct {
	BroadcasterID string `json:"broadcaster_id"`
	Enabled       *bool  `json:"enabled,omitempty"`
	IsMod         *bool  `json:"is_mod,omitempty"`
}

type channelReply struct {
	Channel *channels.Channel `json:"channel,omitempty"`
	Found   bool              `json:"found"`
	Error   string            `json:"error,omitempty"`
}

func (m *Manage) handleChannelGet(ctx context.Context, req channelRequest) channelReply {
	if req.BroadcasterID == "" {
		return channelReply{Error: "bad request"}
	}

	ch, found, err := m.registry.Get(ctx, req.BroadcasterID)
	if err != nil {
		m.log.Error("channel get failed", zap.Error(err))
		return channelReply{Error: "lookup failed"}
	}

	reply := channelReply{Found: found}
	if found {
		reply.Channel = &ch
	}
	return reply
}

func (m *Manage) handleChannelSet(ctx context.Context, req channelRequest) channelReply {
	if req.BroadcasterID == "" {
		return channelReply{Error: "bad request"}
	}

	ch, found, err := m.registry.Get(ctx, req.BroadcasterID)
	if err != nil {
		m.log.Error("channel set lookup failed", zap.Error(err))
		return channelReply{Error: "lookup failed"}
	}

	if !found {
		ch = channels.Channel{BroadcasterID: req.BroadcasterID, Enabled: true}
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
		return channelReply{Error: "save failed"}
	}

	return channelReply{Channel: &ch, Found: true}
}

type channelListReply struct {
	Channels []channels.Channel `json:"channels"`
	Error    string             `json:"error,omitempty"`
}

func (m *Manage) handleChannelList(ctx context.Context, _ struct{}) channelListReply {
	list, err := m.registry.List(ctx)
	if err != nil {
		m.log.Error("channel list failed", zap.Error(err))
		return channelListReply{Error: "list failed"}
	}

	return channelListReply{Channels: list}
}

type systemStatusReply struct {
	Paused                   bool   `json:"paused"`
	AppTokenExpiresInSeconds int64  `json:"app_token_expires_in_seconds"`
	HasUserToken             bool   `json:"has_user_token"`
	Error                    string `json:"error,omitempty"`
}

func (m *Manage) handleSystemStatus(ctx context.Context, _ struct{}) systemStatusReply {
	paused, err := m.registry.Paused(ctx)
	if err != nil {
		m.log.Error("system status failed", zap.Error(err))
		return systemStatusReply{Error: "status failed"}
	}

	return systemStatusReply{
		Paused:                   paused,
		AppTokenExpiresInSeconds: int64(m.twitch.AppTokenExpiresIn().Seconds()),
		HasUserToken:             m.twitch.HasUserToken(),
	}
}

type systemPauseRequest struct {
	Paused bool `json:"paused"`
}

type systemPauseReply struct {
	Paused bool   `json:"paused"`
	Error  string `json:"error,omitempty"`
}

func (m *Manage) handleSystemPause(ctx context.Context, req systemPauseRequest) systemPauseReply {
	if err := m.registry.SetPaused(ctx, req.Paused); err != nil {
		m.log.Error("system pause failed", zap.Error(err))
		return systemPauseReply{Error: "pause failed"}
	}

	m.log.Info("outgress pause state changed", zap.Bool("paused", req.Paused))
	return systemPauseReply{Paused: req.Paused}
}
