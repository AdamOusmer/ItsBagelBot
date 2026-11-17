// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"

	"ItsBagelBot/internal/domain/invalidate"
	rpcprojection "ItsBagelBot/internal/domain/rpc/projection"
	projectorrpc "ItsBagelBot/internal/domain/rpc/projector"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/cache"
)

const (
	statusCacheCapacity int64 = 4096
	statusCacheTTL            = 30 * time.Second
)

type statusEntry struct {
	RuntimeAllowed *bool
	Tier           string
	Banned         bool
}

type statusRPC struct {
	valkey     *projection.Store
	views      *cache.Cache[statusEntry]
	nc         *nats.Conn
	usersTopic string
	log        *zap.Logger
}

type StatusConfig struct {
	Store             *projection.Store
	Subject           string
	UsersTopic        string
	InvalidateSubject string
	Invalidations     cache.Feed
}

func SubscribeStatus(w bus.RPCWiring, cfg StatusConfig) error {
	s := &statusRPC{
		valkey:     cfg.Store,
		views:      cache.New[statusEntry](statusCacheCapacity, statusCacheTTL).InvalidateOn(cfg.Invalidations, cache.UserKeys("tier:"), invalidate.ScopeUserProjection, invalidate.ScopeLive),
		nc:         w.NC,
		usersTopic: cfg.UsersTopic,
		log:        w.Log,
	}

	if err := s.subscribeInvalidation(cfg.InvalidateSubject); err != nil {
		return err
	}

	return bus.QueueSubscribeJSON[projectorrpc.StatusRequest, projectorrpc.StatusReply](w.NC, cfg.Subject, w.Queue, 1500*time.Millisecond, w.App, w.Log, s.handleGet)
}

func (s *statusRPC) subscribeInvalidation(subject string) error {
	if subject == "" {
		return nil
	}
	_, err := s.nc.Subscribe(subject, func(msg *nats.Msg) {
		id, err := strconv.ParseUint(string(msg.Data), 10, 64)
		if err == nil {
			s.Invalidate(id)
		}
	})
	return err
}

func tierKey(id uint64) string {
	return "tier:" + strconv.FormatUint(id, 10)
}

func (s *statusRPC) Invalidate(id uint64) {
	s.views.Invalidate(tierKey(id))
}

func (s *statusRPC) handleGet(ctx context.Context, req projectorrpc.StatusRequest) projectorrpc.StatusReply {
	if req.BroadcasterID == "" {
		return projectorrpc.StatusReply{Error: "bad request"}
	}

	id, err := strconv.ParseUint(req.BroadcasterID, 10, 64)
	if err != nil {
		return projectorrpc.StatusReply{BroadcasterID: req.BroadcasterID, Tier: "standard"}
	}

	entry, err := s.tierOf(ctx, id)
	if err != nil {
		return projectorrpc.StatusReply{BroadcasterID: req.BroadcasterID, Tier: "standard", Error: "status unavailable"}
	}

	return projectorrpc.StatusReply{
		RuntimeAllowed: entry.RuntimeAllowed,
		BroadcasterID:  req.BroadcasterID,
		Tier:           entry.Tier,
		Banned:         entry.Banned,
	}
}

func tierFromStatus(status string) string {
	switch strings.ToLower(status) {
	case "premium", "vip", "paid":
		return "premium"
	default:
		return "standard"
	}
}

func (s *statusRPC) tierFromValkey(ctx context.Context, id uint64) (statusEntry, bool) {
	u, found, err := s.valkey.GetUserRuntime(ctx, id)
	if err == nil && found {
		entry := statusEntry{Tier: tierFromStatus(u.Status), Banned: u.Banned}
		if !u.IsActive {
			entry.Tier = "standard"
		}
		if u.Lifecycle != nil {
			allowed := !u.Banned && u.Lifecycle.AllowsRuntime()
			entry.RuntimeAllowed = &allowed
		}
		return entry, true
	}
	return statusEntry{}, false
}

func (s *statusRPC) fetchUserFallback(ctx context.Context, id uint64) (statusEntry, error) {
	reply, err := bus.RequestJSON[rpcprojection.UserReply](ctx, s.nc, s.usersTopic, map[string]string{"user_id": fmt.Sprint(id)})
	if bus.NotFoundReply(err) {
		return statusEntry{Tier: "standard"}, nil
	}
	if err != nil {
		return statusEntry{}, err
	}

	_ = s.valkey.SetUser(ctx, id, projection.UserFromReply(reply))

	entry := statusEntry{Tier: tierFromStatus(reply.Status), Banned: reply.Banned}
	if !reply.IsActive {
		entry.Tier = "standard"
	}
	if reply.Lifecycle != nil {
		allowed := !reply.Banned && reply.Lifecycle.AllowsRuntime()
		entry.RuntimeAllowed = &allowed
	}
	return entry, nil
}

func (s *statusRPC) tierOf(ctx context.Context, id uint64) (statusEntry, error) {
	return s.views.GetOrLoad(ctx, tierKey(id), func(ctx context.Context) (statusEntry, error) {
		if entry, found := s.tierFromValkey(ctx, id); found {
			return entry, nil
		}
		return s.fetchUserFallback(ctx, id)
	})
}
