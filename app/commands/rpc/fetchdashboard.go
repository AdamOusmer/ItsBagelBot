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
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/bus"
)

// SubscribeFetchDashboard serves the $(urlfetch) console verbs under the same
// prefix as the command verbs (NATS_COMMANDS_SUBJECT_PREFIX): fetch_list,
// fetch_set_def, fetch_set_key and fetch_delete. None of them ever returns key
// material — lists carry label+last4 metadata only, set replies the last4
// derived from the just-submitted value once. The audit zap line per mutate
// lives in the repository; values are never logged.
// FetchDashboardWiring bundles what SubscribeFetchDashboard needs beyond the
// handlers themselves: the RPC connection, the store, the subject prefix and
// queue group, and the New Relic app + logger — the spotifyWiring shape.
type FetchDashboardWiring struct {
	NC         *nats.Conn
	Repo       *repository.Fetches
	Prefix     string
	QueueGroup string
	App        *newrelic.Application
	Log        *zap.Logger
}

func SubscribeFetchDashboard(w FetchDashboardWiring) error {
	d := &fetchDashboardRPC{repo: w.Repo}

	if err := bus.QueueSubscribeJSON[fetchkeyrpc.FetchListRequest, fetchkeyrpc.FetchListReply](
		w.NC, w.Prefix+".fetch_list", w.QueueGroup, 2*time.Second, w.App, w.Log, d.handleList); err != nil {
		return err
	}
	if err := bus.QueueSubscribeJSON[fetchkeyrpc.FetchDefSetRequest, fetchkeyrpc.FetchMutateReply](
		w.NC, w.Prefix+".fetch_set_def", w.QueueGroup, 2*time.Second, w.App, w.Log, d.handleSetDef); err != nil {
		return err
	}
	if err := bus.QueueSubscribeJSON[fetchkeyrpc.FetchKeySetRequest, fetchkeyrpc.FetchKeySetReply](
		w.NC, w.Prefix+".fetch_set_key", w.QueueGroup, 2*time.Second, w.App, w.Log, d.handleSetKey); err != nil {
		return err
	}
	return bus.QueueSubscribeJSON[fetchkeyrpc.FetchDeleteRequest, fetchkeyrpc.FetchMutateReply](
		w.NC, w.Prefix+".fetch_delete", w.QueueGroup, 2*time.Second, w.App, w.Log, d.handleDelete)
}

type fetchDashboardRPC struct {
	repo *repository.Fetches
}

// parseUserID converts the wire user_id; a non-numeric id is a caller bug and
// gets the same refusal shape as the command dashboard verbs.
func parseUserID(raw string) (uint64, error) {
	return strconv.ParseUint(raw, 10, 64)
}

func (d *fetchDashboardRPC) handleList(ctx context.Context, req fetchkeyrpc.FetchListRequest) fetchkeyrpc.FetchListReply {
	id, err := parseUserID(req.UserID)
	if err != nil {
		return fetchkeyrpc.FetchListReply{Error: "invalid user_id"}
	}

	views, err := d.repo.List(ctx, id)
	if err != nil {
		return fetchkeyrpc.FetchListReply{Error: err.Error()}
	}
	keys, err := d.repo.ListKeys(ctx, id)
	if err != nil {
		return fetchkeyrpc.FetchListReply{Error: err.Error()}
	}
	return fetchkeyrpc.FetchListReply{Fetches: views, Keys: keys}
}

func (d *fetchDashboardRPC) handleSetDef(ctx context.Context, req fetchkeyrpc.FetchDefSetRequest) fetchkeyrpc.FetchMutateReply {
	id, err := parseUserID(req.UserID)
	if err != nil {
		return fetchkeyrpc.FetchMutateReply{Error: "invalid user_id"}
	}

	spec := repository.FetchSpec{
		Name:     req.Name,
		URL:      req.URL,
		Path:     req.JSONPath,
		KeyLabel: req.KeyLabel,
		IsActive: req.IsActive,
	}

	// A rename updates the existing row's name field in place; a plain edit
	// or create goes through the immediate validated upsert (never the
	// write-behind batcher — the quota count must see real rows).
	var opErr error
	if req.OriginalName != "" && req.OriginalName != req.Name {
		opErr = d.repo.RenameDef(ctx, id, req.OriginalName, spec)
	} else {
		opErr = d.repo.UpsertDef(ctx, id, spec)
	}
	if opErr != nil {
		return fetchkeyrpc.FetchMutateReply{Error: opErr.Error()}
	}
	return fetchkeyrpc.FetchMutateReply{}
}

func (d *fetchDashboardRPC) handleSetKey(ctx context.Context, req fetchkeyrpc.FetchKeySetRequest) fetchkeyrpc.FetchKeySetReply {
	id, err := parseUserID(req.UserID)
	if err != nil {
		return fetchkeyrpc.FetchKeySetReply{Error: "invalid user_id"}
	}

	last4, err := d.repo.SetKey(ctx, id, repository.KeyEntry{Label: req.Label, Value: req.Value})
	switch {
	case err == nil:
		return fetchkeyrpc.FetchKeySetReply{Last4: last4}
	case isKeyValidationErr(err):
		return fetchkeyrpc.FetchKeySetReply{Error: err.Error()}
	case errors.Is(err, repository.ErrCustodyUnavailable):
		return fetchkeyrpc.FetchKeySetReply{Error: err.Error()}
	default:
		// Seal/persist failure: reported without echoing any of the value.
		return fetchkeyrpc.FetchKeySetReply{Error: "failed to store key"}
	}
}

func (d *fetchDashboardRPC) handleDelete(ctx context.Context, req fetchkeyrpc.FetchDeleteRequest) fetchkeyrpc.FetchMutateReply {
	id, err := parseUserID(req.UserID)
	if err != nil {
		return fetchkeyrpc.FetchMutateReply{Error: "invalid user_id"}
	}

	switch req.Kind {
	case "def":
		err = d.repo.DeleteDef(ctx, id, req.Name, req.Force)
	case "key":
		err = d.repo.DeleteKey(ctx, id, req.Label)
	default:
		return fetchkeyrpc.FetchMutateReply{Error: "kind must be def or key"}
	}
	if err != nil {
		return fetchkeyrpc.FetchMutateReply{Error: err.Error()}
	}
	return fetchkeyrpc.FetchMutateReply{}
}

// isKeyValidationErr reports whether err is one of the domain validation
// sentinels whose message is safe (and useful) to surface verbatim on the
// wire; anything else gets a generic refusal so internals never leak.
func isKeyValidationErr(err error) bool {
	return errors.Is(err, validate.ErrKeyLabel) ||
		errors.Is(err, validate.ErrKeyValue) ||
		errors.Is(err, validate.ErrUserIDZero)
}
