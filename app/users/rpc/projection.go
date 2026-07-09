package rpc

import (
	"context"
	"strconv"
	"time"

	"go.uber.org/zap"

	"ItsBagelBot/app/users/repository"
	"ItsBagelBot/internal/domain/rpc/projection"
	"ItsBagelBot/pkg/bus"
)

type projectionRPC struct {
	repo *repository.Users
	log  *zap.Logger
}

func SubscribeProjection(w Wiring, subject string) error {
	nc, repo, app, log, queueGroup := w.NC, w.Repo, w.App, w.Log, w.Queue
	p := &projectionRPC{
		repo: repo,
		log:  log,
	}

	return bus.QueueSubscribeJSON[projection.Request, projection.UserReply](nc, subject, queueGroup, 2*time.Second, app, log, p.handleGet)
}

func (p *projectionRPC) handleGet(ctx context.Context, req projection.Request) projection.UserReply {
	if req.UserID == "" {
		return projection.UserReply{Error: "bad request"}
	}

	id, err := strconv.ParseUint(req.UserID, 10, 64)
	if err != nil {
		return projection.UserReply{Error: "invalid user_id"}
	}

	view, err := p.repo.Get(ctx, id)
	if err != nil {
		return projection.UserReply{Error: err.Error()}
	}

	return projection.UserReply{
		UserID:   req.UserID,
		Status:   view.Status,
		IsActive: view.IsActive,
		Banned:   view.Banned,
		Locale:   view.Locale,
	}
}
