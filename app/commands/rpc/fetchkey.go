// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"

	"ItsBagelBot/app/commands/repository"
	fetchkeyrpc "ItsBagelBot/internal/domain/rpc/fetchkey"
	"ItsBagelBot/pkg/bus"
)

// SubscribeFetchKey serves the one internal verb, prefix+".get": gossip's
// just-in-time decrypt of a broadcaster's sealed API key before a user-defined
// fetch dials upstream. Same posture as the modules govee key RPC: 2s budget
// (the service answers from its own row plus one Unpack — no upstream hop),
// plaintext used for the single call and never cached by anyone, and an
// Unpack failure is terminal and logged with user_id+label only. A no-op when
// key custody is disabled (nil packer): the reply would always be an error,
// so the subject simply does not exist.
// FetchKeySubscription bundles everything the fetch-key RPC subscription
// needs: the transport, the custody-backed repo answering it, and the wire
// coordinates. One struct beats six positional arguments that only ever
// travel together.
type FetchKeySubscription struct {
	NC         *nats.Conn
	Repo       *repository.Fetches
	Subject    string
	QueueGroup string
	App        *newrelic.Application
	Log        *zap.Logger
}

// SubscribeFetchKey serves the internal decrypt RPC gossip calls just before
// each keyed fetch. Custody-disabled is a quiet no-op: without a keyset the
// subject simply does not exist.
func SubscribeFetchKey(s FetchKeySubscription) error {
	if s.Repo == nil || !s.Repo.CustodyEnabled() {
		s.Log.Warn("fetch key rpc disabled: key custody unavailable")
		return nil
	}

	k := &fetchKeyRPC{repo: s.Repo}
	return bus.QueueSubscribeJSON[fetchkeyrpc.KeyGetRequest, fetchkeyrpc.KeyGetReply](
		s.NC, s.Subject, s.QueueGroup, 2*time.Second, s.App, s.Log, k.handleGet)
}

type fetchKeyRPC struct {
	repo *repository.Fetches
}

func (k *fetchKeyRPC) handleGet(ctx context.Context, req fetchkeyrpc.KeyGetRequest) fetchkeyrpc.KeyGetReply {
	id, err := strconv.ParseUint(req.UserID, 10, 64)
	if err != nil {
		return fetchkeyrpc.KeyGetReply{Error: "user_id must be numeric"}
	}
	key, err := k.repo.Key(ctx, id, req.Label)
	switch {
	case errors.Is(err, repository.ErrNoFetchKey):
		// Empty + empty means "none on file", not a failure.
		return fetchkeyrpc.KeyGetReply{}
	case err != nil:
		return fetchkeyrpc.KeyGetReply{Error: err.Error()}
	}
	return fetchkeyrpc.KeyGetReply{Key: key}
}
