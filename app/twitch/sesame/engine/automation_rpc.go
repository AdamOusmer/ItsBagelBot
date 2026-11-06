// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"

	"ItsBagelBot/internal/domain/rpc"
	automationrpc "ItsBagelBot/internal/domain/rpc/automation"
	automationsrpc "ItsBagelBot/internal/domain/rpc/automations"
	"ItsBagelBot/pkg/bus"
	pkg_valkey "ItsBagelBot/pkg/valkey"
)

const (
	automationRPCTimeout = 10 * time.Second
	runRequestTTL        = 10 * time.Minute
	runRequestPrefix     = "automation:req:"
)

type AutomationRPCDeps struct {
	Pipeline *Pipeline
	Client   valkey.Client
	App      *newrelic.Application
	Log      *zap.Logger
}

type automationRPC struct {
	deps AutomationRPCDeps
}

func SubscribeAutomationRPC(nc *nats.Conn, queue string, deps AutomationRPCDeps) error {
	if deps.Client != nil {
		deps.Client = pkg_valkey.Primary(deps.Client)
	}
	h := &automationRPC{deps: deps}
	w := bus.RPCWiring{NC: nc, App: deps.App, Queue: queue, Log: deps.Log}.Within(automationRPCTimeout)
	if err := bus.Serve(w, automationsrpc.SubjectRun, h.run); err != nil {
		return err
	}
	return bus.Serve(w, automationrpc.RunsSubject, h.runs)
}

func (h *automationRPC) run(ctx context.Context, req automationsrpc.RunRequest) automationsrpc.RunReply {
	broadcasterID, reason := validRunRequest(&req)
	if reason != "" {
		return runRefused(rpc.CodeInvalid, reason)
	}
	if !h.deps.Pipeline.automationsEnabled(ctx, broadcasterID) {
		return runRefused(rpc.CodeForbidden, "automations are not enabled for this channel")
	}
	if prior, dup := h.claimRequest(ctx, broadcasterID, req.RequestID); dup {
		return automationsrpc.RunReply{Accepted: true, RunID: prior}
	}
	reply := h.deps.Pipeline.RunNamed(ctx, req)
	h.settleRequest(ctx, broadcasterID, req.RequestID, reply)
	return reply
}

func validRunRequest(req *automationsrpc.RunRequest) (uint64, string) {
	if req.Source != automationsrpc.SourceDashboard {
		return 0, "invalid source"
	}
	if req.RequestID == "" {
		return 0, "request_id is required"
	}
	id, err := strconv.ParseUint(req.BroadcasterID, 10, 64)
	if err != nil || id == 0 {
		return 0, "broadcaster_id must be numeric"
	}
	return id, ""
}

func runRequestKey(broadcasterID uint64, requestID string) string {
	return runRequestPrefix + "{" + strconv.FormatUint(broadcasterID, 10) + "}:" + requestID
}

func (h *automationRPC) claimRequest(ctx context.Context, broadcasterID uint64, requestID string) (string, bool) {
	client := h.deps.Client
	if client == nil {
		return "", false
	}
	key := runRequestKey(broadcasterID, requestID)
	won, err := pkg_valkey.ClaimOnce(ctx, client, key, runRequestTTL)
	if err != nil {
		h.deps.Log.Warn("automation request dedup unavailable", zap.Error(err))
		return "", false
	}
	if won {
		return "", false
	}
	prior, ok := pkg_valkey.NewKV(client).GetString(ctx, key)
	if !ok || prior == pkg_valkey.ClaimValue {
		return "", true
	}
	return prior, true
}

func (h *automationRPC) settleRequest(ctx context.Context, broadcasterID uint64, requestID string, reply automationsrpc.RunReply) {
	client := h.deps.Client
	if client == nil {
		return
	}
	key := runRequestKey(broadcasterID, requestID)
	kv := pkg_valkey.NewKV(client)
	err := kv.Del(ctx, key)
	if reply.Accepted {
		err = kv.Set(ctx, pkg_valkey.Key{Name: key, TTL: runRequestTTL}, reply.RunID)
	}
	if err != nil {
		h.deps.Log.Warn("automation request dedup settle failed", zap.Error(err))
	}
}

func (h *automationRPC) runs(ctx context.Context, req automationrpc.RunsRequest) automationrpc.RunsReply {
	id, err := strconv.ParseUint(req.BroadcasterID, 10, 64)
	if err != nil || id == 0 {
		return automationrpc.RunsReply{Refusal: rpc.Refused(rpc.CodeInvalid, "broadcaster_id must be numeric")}
	}
	recs, err := h.deps.Pipeline.automations.Runs.Recent(ctx, id, req.AutomationID, req.Limit)
	if err != nil {
		return automationrpc.RunsReply{Refusal: rpc.Refused(rpc.CodeUnavailable, "run log unavailable")}
	}
	if recs == nil {
		recs = []automationrpc.RunRecord{}
	}
	return automationrpc.RunsReply{Runs: recs}
}

func runRefused(code rpc.Code, message string) automationsrpc.RunReply {
	return automationsrpc.RunReply{Refusal: rpc.Refused(code, message)}
}
