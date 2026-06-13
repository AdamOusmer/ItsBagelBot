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
	Error         string `json:"error,omitempty"`
}

type statusRPC struct {
	valkey     *store.Valkey
	views      *cache.Cache[string] // caching the tier string
	nc         *nats.Conn
	usersTopic string
	log        *zap.Logger
}

func SubscribeStatus(nc *nats.Conn, valkey *store.Valkey, subject, usersTopic, queueGroup string, log *zap.Logger) error {
	s := &statusRPC{
		valkey:     valkey,
		views:      cache.New[string](30 * time.Second), // short lived in-process cache
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

	tier := s.tierOf(ctx, id)

	respondStatus(msg, statusReply{
		BroadcasterID: req.BroadcasterID,
		Tier:          tier,
	})
}

func (s *statusRPC) tierOf(ctx context.Context, id uint64) string {
	// 1. In-process cache check
	tier, err := s.views.GetOrLoad(ctx, fmt.Sprintf("tier:%d", id), func(ctx context.Context) (string, error) {
		// 2. Valkey check
		statusStr, active, err := s.valkey.GetUser(ctx, id)
		if err == nil && statusStr != "" {
			if !active {
				return "standard", nil
			}
			if statusStr == "premium" || statusStr == "vip" || statusStr == "paid" {
				return "premium", nil
			}
			return "standard", nil
		}

		// 3. NATS RPC Lazy Load Fallback
		reqPayload, _ := json.Marshal(map[string]string{"user_id": fmt.Sprint(id)})
		msg, err := s.nc.RequestWithContext(ctx, s.usersTopic, reqPayload)
		if err != nil {
			return "standard", nil
		}

		var reply struct {
			Status   string `json:"status"`
			IsActive bool   `json:"is_active"`
		}
		if err := json.Unmarshal(msg.Data, &reply); err != nil {
			return "standard", nil
		}

		// Populate cache
		_ = s.valkey.SetUser(ctx, id, reply.Status, reply.IsActive)

		if !reply.IsActive {
			return "standard", nil
		}
		if reply.Status == "premium" || reply.Status == "vip" || reply.Status == "paid" {
			return "premium", nil
		}
		return "standard", nil
	})

	if err != nil {
		return "standard"
	}
	return tier
}

func respondStatus(msg *nats.Msg, reply statusReply) {
	body, _ := json.Marshal(reply)
	_ = msg.Respond(body)
}
