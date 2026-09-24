// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"

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
	Tier   string
	Banned bool
}

type statusRPC struct {
	valkey     *projection.Store
	views      *cache.Cache[statusEntry]
	nc         *nats.Conn
	usersTopic string
	log        *zap.Logger
}

func SubscribeStatus(nc *nats.Conn, valkey *projection.Store, subject, usersTopic, invalidateSubject, queueGroup string, app *newrelic.Application, log *zap.Logger) error {
	s := &statusRPC{
		valkey:     valkey,
		views:      cache.New[statusEntry](statusCacheCapacity, statusCacheTTL),
		nc:         nc,
		usersTopic: usersTopic,
		log:        log,
	}

	// No queue group: every pod must drop its cached decision on a user change.
	if invalidateSubject != "" {
		if _, err := nc.Subscribe(invalidateSubject, func(msg *nats.Msg) {
			id, err := strconv.ParseUint(string(msg.Data), 10, 64)
			if err != nil {
				return
			}
			s.Invalidate(id)
		}); err != nil {
			return err
		}
	}

	return bus.QueueSubscribeJSON[projectorrpc.StatusRequest, projectorrpc.StatusReply](nc, subject, queueGroup, 1500*time.Millisecond, app, log, s.handleGet)
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

	entry := s.tierOf(ctx, id)

	return projectorrpc.StatusReply{
		BroadcasterID: req.BroadcasterID,
		Tier:          entry.Tier,
		Banned:        entry.Banned,
	}
}

func tierFromStatus(status string) string {
	if status == "premium" || status == "vip" || status == "paid" {
		return "premium"
	}
	return "standard"
}

func (s *statusRPC) tierFromValkey(ctx context.Context, id uint64) (statusEntry, bool) {
	statusStr, active, banned, _, _, err := s.valkey.GetUser(ctx, id)
	if err == nil && statusStr != "" {
		if !active {
			return statusEntry{Tier: "standard", Banned: banned}, true
		}
		return statusEntry{Tier: tierFromStatus(statusStr), Banned: banned}, true
	}
	return statusEntry{}, false
}

func (s *statusRPC) fetchUserFallback(ctx context.Context, id uint64) statusEntry {
	reply, err := bus.RequestJSON[struct {
		Status             string `json:"status"`
		IsActive           bool   `json:"is_active"`
		Banned             bool   `json:"banned"`
		Locale             string `json:"locale"`
		CommandsPageHidden bool   `json:"commands_page_hidden"`
	}](ctx, s.nc, s.usersTopic, map[string]string{"user_id": fmt.Sprint(id)})
	if err != nil {
		return statusEntry{Tier: "standard"}
	}

	_ = s.valkey.SetUser(ctx, id, projection.UserProjection{
		Status:             reply.Status,
		IsActive:           reply.IsActive,
		Banned:             reply.Banned,
		Locale:             reply.Locale,
		CommandsPageHidden: reply.CommandsPageHidden,
	})

	if !reply.IsActive {
		return statusEntry{Tier: "standard", Banned: reply.Banned}
	}
	return statusEntry{Tier: tierFromStatus(reply.Status), Banned: reply.Banned}
}

func (s *statusRPC) tierOf(ctx context.Context, id uint64) statusEntry {
	entry, err := s.views.GetOrLoad(ctx, tierKey(id), func(ctx context.Context) (statusEntry, error) {
		if entry, found := s.tierFromValkey(ctx, id); found {
			return entry, nil
		}
		return s.fetchUserFallback(ctx, id), nil
	})

	if err != nil {
		return statusEntry{Tier: "standard"}
	}
	return entry
}
