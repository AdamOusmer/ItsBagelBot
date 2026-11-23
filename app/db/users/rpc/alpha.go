// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"time"

	"ItsBagelBot/app/db/users/repository"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"
)

const alphaBudget = 1500 * time.Millisecond

func SubscribeAlpha(w Wiring, checkSubject string) error {
	return bus.ServeForUser[usersrpc.AlphaCheckRequest, usersrpc.AlphaCheckReply](w.Within(alphaBudget), checkSubject, alphaRPC{repo: w.Repo}.check)
}

type alphaRPC struct {
	repo *repository.Users
}

func (a alphaRPC) check(ctx context.Context, _ usersrpc.AlphaCheckRequest, id uint64) (usersrpc.AlphaCheckReply, error) {
	view, err := a.repo.Get(ctx, id)
	return usersrpc.AlphaCheckReply{Alpha: view.Alpha, Refusal: bus.Classify(err, entNotFound)}, nil
}
