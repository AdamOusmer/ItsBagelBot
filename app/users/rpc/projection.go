package rpc

import (
	"context"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"

	"ItsBagelBot/app/users/repository"
	"ItsBagelBot/pkg/bus"
)

type projectionRPC struct {
	repo *repository.Users
	log  *zap.Logger
}

func SubscribeProjection(nc *nats.Conn, repo *repository.Users, subject, queueGroup string, log *zap.Logger) error {
	p := &projectionRPC{
		repo: repo,
		log:  log,
	}

	return bus.QueueSubscribeJSON[projectionRequest, projectionReply](nc, subject, queueGroup, 2*time.Second, log, p.handleGet)
}

type projectionRequest struct {
	UserID string `json:"user_id"`
}

type projectionReply struct {
	UserID   string `json:"user_id"`
	Status   string `json:"status"`
	IsActive bool   `json:"is_active"`
	Banned   bool   `json:"banned"`
	Error    string `json:"error,omitempty"`
}

func (p *projectionRPC) handleGet(ctx context.Context, req projectionRequest) projectionReply {
	if req.UserID == "" {
		return projectionReply{Error: "bad request"}
	}

	id, err := strconv.ParseUint(req.UserID, 10, 64)
	if err != nil {
		return projectionReply{Error: "invalid user_id"}
	}

	view, err := p.repo.Get(ctx, id)
	if err != nil {
		return projectionReply{Error: err.Error()}
	}

	return projectionReply{
		UserID:   req.UserID,
		Status:   view.Status,
		IsActive: view.IsActive,
		Banned:   view.Banned,
	}
}
