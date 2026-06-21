package rpc

import (
	"context"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"

	"ItsBagelBot/app/commands/repository"
	"ItsBagelBot/internal/domain/rpc/projection"
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

	return bus.QueueSubscribeJSON[projection.Request, projection.CommandsReply](nc, subject, queueGroup, 2*time.Second, app, log, p.handleGet)
}

func (p *projectionRPC) handleGet(ctx context.Context, req projection.Request) projection.CommandsReply {
	if req.UserID == "" {
		return projection.CommandsReply{Error: "bad request"}
	}

	id, err := strconv.ParseUint(req.UserID, 10, 64)
	if err != nil {
		return projection.CommandsReply{Error: "invalid user_id"}
	}

	views, err := p.repo.List(ctx, id)
	if err != nil {
		return projection.CommandsReply{Error: err.Error()}
	}

	return projection.CommandsReply{
		UserID:   req.UserID,
		Commands: views,
	}
}
