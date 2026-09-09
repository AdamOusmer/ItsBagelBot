// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"

	"ItsBagelBot/app/db/commands/repository"
	domainrpc "ItsBagelBot/internal/domain/rpc"
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
func SubscribeFetchDashboard(w Wiring, prefix string) error {
	d := &fetchDashboardRPC{repo: w.Fetches}

	// Four verbs, four different request and reply types, so they cannot share
	// one ServeVerbs table; ServeForUser binds each on the shared wiring and
	// puts the user-id guard in front of every one of them.
	return errors.Join(
		bus.ServeForUser[fetchkeyrpc.FetchListRequest, fetchkeyrpc.FetchListReply](w.RPCWiring, prefix+".fetch_list", d.handleList),
		bus.ServeForUser[fetchkeyrpc.FetchDefSetRequest, fetchkeyrpc.FetchMutateReply](w.RPCWiring, prefix+".fetch_set_def", d.handleSetDef),
		bus.ServeForUser[fetchkeyrpc.FetchKeySetRequest, fetchkeyrpc.FetchKeySetReply](w.RPCWiring, prefix+".fetch_set_key", d.handleSetKey),
		bus.ServeForUser[fetchkeyrpc.FetchDeleteRequest, fetchkeyrpc.FetchMutateReply](w.RPCWiring, prefix+".fetch_delete", d.handleDelete),
	)
}

type fetchDashboardRPC struct {
	repo *repository.Fetches
}

func (d *fetchDashboardRPC) handleList(ctx context.Context, _ fetchkeyrpc.FetchListRequest, id uint64) (fetchkeyrpc.FetchListReply, error) {
	views, err := d.repo.List(ctx, id)
	if err != nil {
		return fetchkeyrpc.FetchListReply{}, err
	}
	keys, err := d.repo.ListKeys(ctx, id)
	if err != nil {
		return fetchkeyrpc.FetchListReply{}, err
	}
	return fetchkeyrpc.FetchListReply{Fetches: views, Keys: keys}, nil
}

func (d *fetchDashboardRPC) handleSetDef(ctx context.Context, req fetchkeyrpc.FetchDefSetRequest, id uint64) (fetchkeyrpc.FetchMutateReply, error) {
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
	if req.OriginalName != "" && req.OriginalName != req.Name {
		return fetchkeyrpc.FetchMutateReply{}, d.repo.RenameDef(ctx, id, req.OriginalName, spec)
	}
	return fetchkeyrpc.FetchMutateReply{}, d.repo.UpsertDef(ctx, id, spec)
}

func (d *fetchDashboardRPC) handleSetKey(ctx context.Context, req fetchkeyrpc.FetchKeySetRequest, id uint64) (fetchkeyrpc.FetchKeySetReply, error) {
	last4, err := d.repo.SetKey(ctx, id, repository.KeyEntry{Label: req.Label, Value: req.Value})
	switch {
	case err == nil:
		return fetchkeyrpc.FetchKeySetReply{Last4: last4}, nil
	case isKeyValidationErr(err), errors.Is(err, repository.ErrCustodyUnavailable):
		return fetchkeyrpc.FetchKeySetReply{}, err
	default:
		// Seal/persist failure: reported without echoing any of the value.
		return fetchkeyrpc.FetchKeySetReply{Refusal: domainrpc.Refused(domainrpc.CodeInternal, "failed to store key")}, nil
	}
}

func (d *fetchDashboardRPC) handleDelete(ctx context.Context, req fetchkeyrpc.FetchDeleteRequest, id uint64) (fetchkeyrpc.FetchMutateReply, error) {
	switch req.Kind {
	case "def":
		return fetchkeyrpc.FetchMutateReply{}, d.repo.DeleteDef(ctx, id, repository.DefDelete{Name: req.Name, Force: req.Force})
	case "key":
		return fetchkeyrpc.FetchMutateReply{}, d.repo.DeleteKey(ctx, id, req.Label)
	default:
		return fetchkeyrpc.FetchMutateReply{Refusal: domainrpc.Refused(domainrpc.CodeInvalid, "kind must be def or key")}, nil
	}
}

// isKeyValidationErr reports whether err is one of the domain validation
// sentinels whose message is safe (and useful) to surface verbatim on the
// wire; anything else gets a generic refusal so internals never leak.
func isKeyValidationErr(err error) bool {
	return errors.Is(err, validate.ErrKeyLabel) ||
		errors.Is(err, validate.ErrKeyValue) ||
		errors.Is(err, validate.ErrUserIDZero)
}
