// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"

	"ItsBagelBot/app/commands/repository"
	fetchkeyrpc "ItsBagelBot/internal/domain/rpc/fetchkey"
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

// SubscribeFetchProjection serves the tier-3 fallback the worker's
// projection.Client falls through to when a user's settings hash has no
// projected fetch section yet — the exact role the commands get verb plays
// for command:<name> fields. The reply shape mirrors CommandsReply with
// fetch views (fetchkey.FetchView, whose tags match the Valkey field JSON).
func SubscribeFetchProjection(nc *nats.Conn, repo *repository.Fetches, subject, queueGroup string, app *newrelic.Application, log *zap.Logger) error {
	p := &fetchProjectionRPC{repo: repo}
	return bus.QueueSubscribeJSON[fetchkeyrpc.FetchListRequest, fetchkeyrpc.FetchListReply](nc, subject, queueGroup, 2*time.Second, app, log, p.handleGet)
}

type fetchProjectionRPC struct {
	repo *repository.Fetches
}

func (p *fetchProjectionRPC) handleGet(ctx context.Context, req fetchkeyrpc.FetchListRequest) fetchkeyrpc.FetchListReply {
	if req.UserID == "" {
		return fetchkeyrpc.FetchListReply{Error: "bad request"}
	}

	id, err := strconv.ParseUint(req.UserID, 10, 64)
	if err != nil {
		return fetchkeyrpc.FetchListReply{Error: "invalid user_id"}
	}

	views, err := p.repo.List(ctx, id)
	if err != nil {
		return fetchkeyrpc.FetchListReply{Error: err.Error()}
	}
	return fetchkeyrpc.FetchListReply{UserID: req.UserID, Fetches: views}
}
