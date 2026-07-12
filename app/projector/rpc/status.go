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

// statusCacheCapacity ceilings the resolved-status cache. It is keyed one entry
// per broadcaster with a 30s TTL, so a few thousand covers the distinct
// broadcasters a projector pod answers for within the window without holding the
// generic cache.DefaultCapacity ten thousand at rest.
const statusCacheCapacity int64 = 4096

// statusEntry is the cached per-broadcaster decision: the resolved tier plus
// whether the broadcaster is banned from the service.
type statusEntry struct {
	Tier   string
	Banned bool
}

type statusRPC struct {
	valkey     *projection.Store
	views      *cache.Cache[statusEntry] // caching the resolved tier + ban flag
	nc         *nats.Conn
	usersTopic string
	log        *zap.Logger
}

func SubscribeStatus(nc *nats.Conn, valkey *projection.Store, subject, usersTopic, invalidateSubject, queueGroup string, app *newrelic.Application, log *zap.Logger) error {
	s := &statusRPC{
		valkey:     valkey,
		views:      cache.New[statusEntry](statusCacheCapacity, 30*time.Second), // short lived in-process cache
		nc:         nc,
		usersTopic: usersTopic,
		log:        log,
	}

	// Core-NATS fan-out (no queue group): every projector pod drops its cached
	// tier/ban decision the moment a user changes, instead of waiting out the
	// 30s TTL. Without this a ban or tier change is stale per pod for up to 30s.
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

// tierKey is the in-process cache key for a user's resolved tier+ban entry.
func tierKey(id uint64) string {
	return "tier:" + strconv.FormatUint(id, 10)
}

// Invalidate drops the cached tier+ban decision for one user so the next status
// query re-resolves it from the freshly projected Valkey state.
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

func (s *statusRPC) tierOf(ctx context.Context, id uint64) statusEntry {
	// 1. In-process cache check
	entry, err := s.views.GetOrLoad(ctx, tierKey(id), func(ctx context.Context) (statusEntry, error) {
		// 2. Valkey check
		statusStr, active, banned, _, err := s.valkey.GetUser(ctx, id)
		if err == nil && statusStr != "" {
			if !active {
				return statusEntry{Tier: "standard", Banned: banned}, nil
			}
			return statusEntry{Tier: tierFromStatus(statusStr), Banned: banned}, nil
		}

		// 3. NATS RPC Lazy Load Fallback
		reply, err := bus.RequestJSON[struct {
			Status   string `json:"status"`
			IsActive bool   `json:"is_active"`
			Banned   bool   `json:"banned"`
			Locale   string `json:"locale"`
		}](ctx, s.nc, s.usersTopic, map[string]string{"user_id": fmt.Sprint(id)})
		if err != nil {
			return statusEntry{Tier: "standard"}, nil
		}

		// Populate cache (seeds locale too when the users service returned one).
		_ = s.valkey.SetUser(ctx, id, projection.UserProjection{
			Status:   reply.Status,
			IsActive: reply.IsActive,
			Banned:   reply.Banned,
			Locale:   reply.Locale,
		})

		if !reply.IsActive {
			return statusEntry{Tier: "standard", Banned: reply.Banned}, nil
		}
		return statusEntry{Tier: tierFromStatus(reply.Status), Banned: reply.Banned}, nil
	})

	if err != nil {
		return statusEntry{Tier: "standard"}
	}
	return entry
}
