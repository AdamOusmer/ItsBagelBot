package rpc

import (
	"context"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"

	"ItsBagelBot/app/commands/repository"
	"ItsBagelBot/pkg/bus"
)

type projectionRPC struct {
	repo *repository.Commands
	log  *zap.Logger
}

func SubscribeProjection(nc *nats.Conn, repo *repository.Commands, subject, queueGroup string, app *newrelic.Application, log *zap.Logger) error {
	p := &projectionRPC{
		repo: repo,
		log:  log,
	}

	return bus.QueueSubscribeJSON[projectionRequest, projectionReply](nc, subject, queueGroup, 2*time.Second, app, log, p.handleGet)
}

type projectionRequest struct {
	UserID string `json:"user_id"`
}

type projectionReply struct {
	UserID   string                   `json:"user_id"`
	Commands []repository.CommandView `json:"commands"`
	Error    string                   `json:"error,omitempty"`
}

func (p *projectionRPC) handleGet(ctx context.Context, req projectionRequest) projectionReply {
	if req.UserID == "" {
		return projectionReply{Error: "bad request"}
	}

	id, err := strconv.ParseUint(req.UserID, 10, 64)
	if err != nil {
		return projectionReply{Error: "invalid user_id"}
	}

	views, err := p.repo.List(ctx, id)
	if err != nil {
		return projectionReply{Error: err.Error()}
	}

	return projectionReply{
		UserID:   req.UserID,
		Commands: views,
	}
}
