// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"

	"ItsBagelBot/app/db/commands/repository"
	fetchkeyrpc "ItsBagelBot/internal/domain/rpc/fetchkey"
	"ItsBagelBot/pkg/bus"
)

func SubscribeFetchKey(w Wiring, subject string) error {
	if w.Fetches == nil || !w.Fetches.CustodyEnabled() {
		w.Log.Warn("fetch key rpc disabled: key custody unavailable")
		return nil
	}

	k := &fetchKeyRPC{repo: w.Fetches}
	return bus.ServeForUser[fetchkeyrpc.KeyGetRequest, fetchkeyrpc.KeyGetReply](w.RPCWiring, subject, k.handleGet)
}

type fetchKeyRPC struct {
	repo *repository.Fetches
}

func (k *fetchKeyRPC) handleGet(ctx context.Context, req fetchkeyrpc.KeyGetRequest, id uint64) (fetchkeyrpc.KeyGetReply, error) {
	key, err := k.repo.Key(ctx, id, req.Label)
	if errors.Is(err, repository.ErrNoFetchKey) {
		return fetchkeyrpc.KeyGetReply{}, nil
	}
	return fetchkeyrpc.KeyGetReply{Key: key}, err
}
