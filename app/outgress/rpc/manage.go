// Package rpc exposes the outgress management API over NATS request-reply,
// mirroring the bagel.rpc.* conventions of the other services. It covers the
// two things an operator manages here: channels (enable, disable, mod
// status) and the system itself (kill switch, token health).
package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"ItsBagelBot/app/outgress/internal/channels"
	"ItsBagelBot/app/outgress/internal/twitch"

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

	handlers := map[string]nats.MsgHandler{
		prefix + ".channel.get":   m.handleChannelGet,
		prefix + ".channel.set":   m.handleChannelSet,
		prefix + ".channel.list":  m.handleChannelList,
		prefix + ".system.status": m.handleSystemStatus,
		prefix + ".system.pause":  m.handleSystemPause,
	}

	for subject, handler := range handlers {
		if _, err := nc.QueueSubscribe(subject, queueGroup, handler); err != nil {
			return fmt.Errorf("subscribe %s: %w", subject, err)
		}
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

func (m *Manage) handleChannelGet(msg *nats.Msg) {

	var req channelRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil || req.BroadcasterID == "" {
		respond(msg, channelReply{Error: "bad request"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), handleTimeout)
	defer cancel()

	ch, found, err := m.registry.Get(ctx, req.BroadcasterID)
	if err != nil {
		m.log.Error("channel get failed", zap.Error(err))
		respond(msg, channelReply{Error: "lookup failed"})
		return
	}

	reply := channelReply{Found: found}
	if found {
		reply.Channel = &ch
	}
	respond(msg, reply)
}

func (m *Manage) handleChannelSet(msg *nats.Msg) {

	var req channelRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil || req.BroadcasterID == "" {
		respond(msg, channelReply{Error: "bad request"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), handleTimeout)
	defer cancel()

	ch, found, err := m.registry.Get(ctx, req.BroadcasterID)
	if err != nil {
		m.log.Error("channel set lookup failed", zap.Error(err))
		respond(msg, channelReply{Error: "lookup failed"})
		return
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
		respond(msg, channelReply{Error: "save failed"})
		return
	}

	respond(msg, channelReply{Channel: &ch, Found: true})
}

type channelListReply struct {
	Channels []channels.Channel `json:"channels"`
	Error    string             `json:"error,omitempty"`
}

func (m *Manage) handleChannelList(msg *nats.Msg) {

	ctx, cancel := context.WithTimeout(context.Background(), handleTimeout)
	defer cancel()

	list, err := m.registry.List(ctx)
	if err != nil {
		m.log.Error("channel list failed", zap.Error(err))
		respond(msg, channelListReply{Error: "list failed"})
		return
	}

	respond(msg, channelListReply{Channels: list})
}

type systemStatusReply struct {
	Paused                   bool   `json:"paused"`
	AppTokenExpiresInSeconds int64  `json:"app_token_expires_in_seconds"`
	HasUserToken             bool   `json:"has_user_token"`
	Error                    string `json:"error,omitempty"`
}

func (m *Manage) handleSystemStatus(msg *nats.Msg) {

	ctx, cancel := context.WithTimeout(context.Background(), handleTimeout)
	defer cancel()

	paused, err := m.registry.Paused(ctx)
	if err != nil {
		m.log.Error("system status failed", zap.Error(err))
		respond(msg, systemStatusReply{Error: "status failed"})
		return
	}

	respond(msg, systemStatusReply{
		Paused:                   paused,
		AppTokenExpiresInSeconds: int64(m.twitch.AppTokenExpiresIn().Seconds()),
		HasUserToken:             m.twitch.HasUserToken(),
	})
}

type systemPauseRequest struct {
	Paused bool `json:"paused"`
}

type systemPauseReply struct {
	Paused bool   `json:"paused"`
	Error  string `json:"error,omitempty"`
}

func (m *Manage) handleSystemPause(msg *nats.Msg) {

	var req systemPauseRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		respond(msg, systemPauseReply{Error: "bad request"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), handleTimeout)
	defer cancel()

	if err := m.registry.SetPaused(ctx, req.Paused); err != nil {
		m.log.Error("system pause failed", zap.Error(err))
		respond(msg, systemPauseReply{Error: "pause failed"})
		return
	}

	m.log.Info("outgress pause state changed", zap.Bool("paused", req.Paused))
	respond(msg, systemPauseReply{Paused: req.Paused})
}

func respond(msg *nats.Msg, reply any) {
	body, _ := json.Marshal(reply)
	_ = msg.Respond(body)
}
