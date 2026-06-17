package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"

	"ItsBagelBot/app/projector/store"
	"ItsBagelBot/pkg/cache"
)

type statusRequest struct {
	BroadcasterID string `json:"broadcaster_id"`
}

type statusReply struct {
	BroadcasterID string `json:"broadcaster_id"`
	Tier          string `json:"tier"`
	Banned        bool   `json:"banned"`
	Error         string `json:"error,omitempty"`
}

// statusEntry is the cached per-broadcaster decision: the resolved tier plus
// whether the broadcaster is banned from the service.
type statusEntry struct {
	Tier   string
	Banned bool
}

type statusRPC struct {
	valkey     *store.Valkey
	views      *cache.Cache[statusEntry] // caching the resolved tier + ban flag
	nc         *nats.Conn
	usersTopic string
	log        *zap.Logger
}

func SubscribeStatus(nc *nats.Conn, valkey *store.Valkey, subject, usersTopic, queueGroup string, log *zap.Logger) error {
	s := &statusRPC{
		valkey:     valkey,
		views:      cache.New[statusEntry](cache.DefaultCapacity, 30*time.Second), // short lived in-process cache
		nc:         nc,
		usersTopic: usersTopic,
		log:        log,
	}

	if _, err := nc.QueueSubscribe(subject, queueGroup, s.handleGet); err != nil {
		return fmt.Errorf("subscribe %s: %w", subject, err)
	}
	return nil
}

func (s *statusRPC) handleGet(msg *nats.Msg) {
	var req statusRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil || req.BroadcasterID == "" {
		respondStatus(msg, statusReply{Error: "bad request"})
		return
	}

	id, err := strconv.ParseUint(req.BroadcasterID, 10, 64)
	if err != nil {
		respondStatus(msg, statusReply{BroadcasterID: req.BroadcasterID, Tier: "standard"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()

	entry := s.tierOf(ctx, id)

	respondStatus(msg, statusReply{
		BroadcasterID: req.BroadcasterID,
		Tier:          entry.Tier,
		Banned:        entry.Banned,
	})
}

func tierFromStatus(status string) string {
	if status == "premium" || status == "vip" || status == "paid" {
		return "premium"
	}
	return "standard"
}

func (s *statusRPC) tierOf(ctx context.Context, id uint64) statusEntry {
	// 1. In-process cache check
	entry, err := s.views.GetOrLoad(ctx, fmt.Sprintf("tier:%d", id), func(ctx context.Context) (statusEntry, error) {
		// 2. Valkey check
		statusStr, active, banned, err := s.valkey.GetUser(ctx, id)
		if err == nil && statusStr != "" {
			if !active {
				return statusEntry{Tier: "standard", Banned: banned}, nil
			}
			return statusEntry{Tier: tierFromStatus(statusStr), Banned: banned}, nil
		}

		// 3. NATS RPC Lazy Load Fallback
		reqPayload, _ := json.Marshal(map[string]string{"user_id": fmt.Sprint(id)})
		msg, err := s.nc.RequestWithContext(ctx, s.usersTopic, reqPayload)
		if err != nil {
			return statusEntry{Tier: "standard"}, nil
		}

		var reply struct {
			Status   string `json:"status"`
			IsActive bool   `json:"is_active"`
			Banned   bool   `json:"banned"`
		}
		if err := json.Unmarshal(msg.Data, &reply); err != nil {
			return statusEntry{Tier: "standard"}, nil
		}

		// Populate cache
		_ = s.valkey.SetUser(ctx, id, reply.Status, reply.IsActive, reply.Banned)

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

func respondStatus(msg *nats.Msg, reply statusReply) {
	body, _ := json.Marshal(reply)
	_ = msg.Respond(body)
}
