// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package rpc serves the deploy verbs and authorizes every call as owner.
package rpc

import (
	"context"
	"fmt"
	"slices"

	"ItsBagelBot/app/deployer/internal/ports"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	"ItsBagelBot/internal/domain/rpc/deploy"
	"ItsBagelBot/pkg/bus"
)

// Engine is the run engine as the verbs see it; *engine.Engine satisfies it.
type Engine interface {
	Plan(ctx context.Context, actor deploy.Actor, req deploy.PlanRequest) (deploy.Plan, error)
	Start(ctx context.Context, actor deploy.Actor, req deploy.StartRequest) (deploy.Run, error)
	Get(ctx context.Context, actor deploy.Actor, req deploy.RunRequest) (deploy.Run, error)
	List(ctx context.Context, actor deploy.Actor, req deploy.ListRequest) ([]deploy.RunSummary, deploy.RunID, error)
	Resume(ctx context.Context, actor deploy.Actor, req deploy.RunRequest) (deploy.Run, error)
	Cancel(ctx context.Context, actor deploy.Actor, req deploy.RunRequest) (deploy.Run, error)
	Approve(ctx context.Context, actor deploy.Actor, req deploy.RunRequest) (deploy.Run, error)
}

// Serve binds every verb under prefix (deploy.Prefix). Four ServeVerbs calls
// rather than one table because a table binds a single request/reply pair and
// the deploy verbs use four; the run-addressed verbs share one.
func Serve(w bus.RPCWiring, prefix string, e Engine, auth ports.Authorizer) error {
	v := newVerbs(e, auth)
	if err := bus.ServeVerbs(w, prefix, v.plan); err != nil {
		return err
	}
	if err := bus.ServeVerbs(w, prefix, v.start); err != nil {
		return err
	}
	if err := bus.ServeVerbs(w, prefix, v.list); err != nil {
		return err
	}
	return bus.ServeVerbs(w, prefix, v.runs...)
}

// verbs is the bound surface, split by request type. Serve binds it and the
// tests drive the same handlers, so a verb that lost its guard fails a test
// instead of reaching the cluster.
type verbs struct {
	plan  bus.Verb[deploy.PlanRequest, deploy.PlanReply]
	start bus.Verb[deploy.StartRequest, deploy.RunReply]
	list  bus.Verb[deploy.ListRequest, deploy.ListReply]
	runs  []bus.Verb[deploy.RunRequest, deploy.RunReply]
}

func newVerbs(e Engine, auth ports.Authorizer) verbs {
	s := server{engine: e}
	runActor := func(r deploy.RunRequest) string { return r.ActorID }
	return verbs{
		plan:  bus.At(deploy.VerbPlan, guarded(auth, func(r deploy.PlanRequest) string { return r.ActorID }, s.plan)),
		start: bus.At(deploy.VerbStart, guarded(auth, func(r deploy.StartRequest) string { return r.ActorID }, s.start)),
		list:  bus.At(deploy.VerbList, guarded(auth, func(r deploy.ListRequest) string { return r.ActorID }, s.list)),
		runs: []bus.Verb[deploy.RunRequest, deploy.RunReply]{
			bus.At(deploy.VerbGet, guarded(auth, runActor, addressed(e.Get))),
			bus.At(deploy.VerbResume, guarded(auth, runActor, addressed(e.Resume))),
			bus.At(deploy.VerbCancel, guarded(auth, runActor, addressed(e.Cancel))),
			bus.At(deploy.VerbApprove, guarded(auth, runActor, addressed(e.Approve))),
		},
	}
}

// handler is one verb's body, reached only with an actor the users service
// has confirmed as owner.
type handler[Req, Rep any] func(context.Context, deploy.Actor, Req) (Rep, error)

// refusing is a reply that embeds domainrpc.Refusal, so the guard can stamp a
// refusal onto any of the three reply types.
type refusing[Rep any] interface {
	*Rep
	domainrpc.Refusing
}

// guarded wraps one verb in the owner check, mirroring the users service's
// admin ladder (app/db/users/rpc/admin.go guarded): no handler carries an
// authorization branch of its own, and the actor a handler sees is the one
// the users service resolved from its staff table, never a role or login the
// request claimed.
func guarded[Req, Rep any, PR refusing[Rep]](auth ports.Authorizer, actorOf func(Req) string, h handler[Req, Rep]) func(context.Context, Req) Rep {
	return func(ctx context.Context, req Req) Rep {
		actor, err := owner(ctx, auth, actorOf(req))
		if err != nil {
			return refused[Rep, PR](err)
		}
		rep, err := h(ctx, actor, req)
		if err != nil {
			return refused[Rep, PR](err)
		}
		return rep
	}
}

// owner refuses a request that names no actor before spending a users hop on
// it. auth.check would refuse the empty id too, but through the transport
// and as forbidden; a missing actor_id is a malformed request, and the code
// says so.
func owner(ctx context.Context, auth ports.Authorizer, actorID string) (deploy.Actor, error) {
	if actorID == "" {
		return deploy.Actor{}, invalid("actor_id is required")
	}
	return auth.RequireOwner(ctx, actorID)
}

// refused is the zero reply carrying err's refusal, so a failed verb never
// ships a half-built payload next to its error.
func refused[Rep any, PR refusing[Rep]](err error) Rep {
	var rep Rep
	PR(&rep).Refuse(domainrpc.Fail(err, rules...))
	return rep
}

// rules maps the engine's sentinels onto the shared vocabulary. Lock held,
// not resumable and a lost CAS are all conflicts: the request was well formed
// and the run's current state said no, so the console re-reads the run
// instead of retrying. ErrRefused and anything unrecognised fall through to
// internal.
var rules = []domainrpc.Rule{
	domainrpc.Is(ports.ErrInvalid, domainrpc.CodeInvalid),
	domainrpc.Is(ports.ErrNotFound, domainrpc.CodeNotFound),
	domainrpc.Is(ports.ErrForbidden, domainrpc.CodeForbidden),
	domainrpc.Is(ports.ErrConflict, domainrpc.CodeConflict),
	domainrpc.Is(ports.ErrLockHeld, domainrpc.CodeConflict),
	domainrpc.Is(ports.ErrNotResumable, domainrpc.CodeConflict),
}

// server holds the engine the verb bodies call.
type server struct {
	engine Engine
}

// plan accepts no kind (the overview) or a known one.
func (s server) plan(ctx context.Context, actor deploy.Actor, req deploy.PlanRequest) (deploy.PlanReply, error) {
	if req.Kind != "" && !known(req.Kind) {
		return deploy.PlanReply{}, invalid("unknown kind %q", req.Kind)
	}
	plan, err := s.engine.Plan(ctx, actor, req)
	return deploy.PlanReply{Plan: &plan}, err
}

func (s server) start(ctx context.Context, actor deploy.Actor, req deploy.StartRequest) (deploy.RunReply, error) {
	if !known(req.Kind) {
		return deploy.RunReply{}, invalid("unknown kind %q", req.Kind)
	}
	run, err := s.engine.Start(ctx, actor, req)
	return deploy.RunReply{Run: &run}, err
}

func (s server) list(ctx context.Context, actor deploy.Actor, req deploy.ListRequest) (deploy.ListReply, error) {
	runs, active, err := s.engine.List(ctx, actor, req)
	return deploy.ListReply{Runs: runs, ActiveRunID: active}, err
}

// addressed is the body shared by the four verbs that name one run: get,
// resume, cancel and approve differ only in the engine call.
func addressed(call handler[deploy.RunRequest, deploy.Run]) handler[deploy.RunRequest, deploy.RunReply] {
	return func(ctx context.Context, actor deploy.Actor, req deploy.RunRequest) (deploy.RunReply, error) {
		if req.RunID == "" {
			return deploy.RunReply{}, invalid("run_id is required")
		}
		run, err := call(ctx, actor, req)
		return deploy.RunReply{Run: &run}, err
	}
}

func known(kind deploy.RunKind) bool { return slices.Contains(deploy.Kinds(), kind) }

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ports.ErrInvalid, fmt.Sprintf(format, args...))
}
