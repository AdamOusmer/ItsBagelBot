package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"

	"ItsBagelBot/app/users/repository"
)

type projectionRPC struct {
	repo *repository.Users
	nc   *nats.Conn
	log  *zap.Logger
}

func SubscribeProjection(nc *nats.Conn, repo *repository.Users, subject, queueGroup string, log *zap.Logger) error {
	p := &projectionRPC{
		repo: repo,
		nc:   nc,
		log:  log,
	}

	if _, err := nc.QueueSubscribe(subject, queueGroup, p.handleGet); err != nil {
		return fmt.Errorf("subscribe %s: %w", subject, err)
	}
	return nil
}

type projectionRequest struct {
	UserID string `json:"user_id"`
}

type projectionReply struct {
	UserID   string `json:"user_id"`
	Status   string `json:"status"`
	IsActive bool   `json:"is_active"`
	Error    string `json:"error,omitempty"`
}

func (p *projectionRPC) handleGet(msg *nats.Msg) {
	var req projectionRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil || req.UserID == "" {
		respondProj(msg, projectionReply{Error: "bad request"})
		return
	}

	id, err := strconv.ParseUint(req.UserID, 10, 64)
	if err != nil {
		respondProj(msg, projectionReply{Error: "invalid user_id"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	view, err := p.repo.Get(ctx, id)
	if err != nil {
		respondProj(msg, projectionReply{Error: err.Error()})
		return
	}

	respondProj(msg, projectionReply{
		UserID:   req.UserID,
		Status:   view.Status,
		IsActive: view.IsActive,
	})
}

func respondProj(msg *nats.Msg, reply projectionReply) {
	body, _ := json.Marshal(reply)
	_ = msg.Respond(body)
}
