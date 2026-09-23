// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"

	"ItsBagelBot/app/deployer/internal/ports"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	"ItsBagelBot/internal/domain/rpc/deploy"
	"ItsBagelBot/pkg/bus"
)

const (
	ownerID    = "1001"
	ownerLogin = "adam"
	viewerID   = "2002"
)

// allVerbs is every verb the wire contract declares; the guard tests walk it
// so a verb added to deploy.go without a guarded binding fails here.
var allVerbs = []string{
	deploy.VerbPlan, deploy.VerbStart, deploy.VerbList,
	deploy.VerbGet, deploy.VerbResume, deploy.VerbCancel, deploy.VerbApprove,
}

// fakeAuth admits ownerID only, the way the users service admits one staff
// row with the owner role.
type fakeAuth struct{}

func (fakeAuth) RequireOwner(_ context.Context, actorID string) (deploy.Actor, error) {
	if actorID != ownerID {
		return deploy.Actor{}, ports.ErrForbidden
	}
	return deploy.Actor{ID: actorID, Login: ownerLogin}, nil
}

// call is one engine invocation as the fake saw it.
type call struct {
	Verb  string
	Actor deploy.Actor
}

// fakeEngine answers every verb with a payload and err, recording who asked.
type fakeEngine struct {
	err   error
	calls []call
}

func (f *fakeEngine) record(c call) error {
	f.calls = append(f.calls, c)
	return f.err
}

func (f *fakeEngine) Plan(_ context.Context, a deploy.Actor, _ deploy.PlanRequest) (deploy.Plan, error) {
	return deploy.Plan{MainSHA: "abc"}, f.record(call{deploy.VerbPlan, a})
}

func (f *fakeEngine) Start(_ context.Context, a deploy.Actor, r deploy.StartRequest) (deploy.Run, error) {
	return deploy.Run{ID: "run-1", Kind: r.Kind, Actor: a}, f.record(call{deploy.VerbStart, a})
}

func (f *fakeEngine) List(_ context.Context, a deploy.Actor, _ deploy.ListRequest) ([]deploy.RunSummary, deploy.RunID, error) {
	return []deploy.RunSummary{{ID: "run-1"}}, "run-1", f.record(call{deploy.VerbList, a})
}

func (f *fakeEngine) Get(_ context.Context, a deploy.Actor, r deploy.RunRequest) (deploy.Run, error) {
	return deploy.Run{ID: r.RunID}, f.record(call{deploy.VerbGet, a})
}

func (f *fakeEngine) Resume(_ context.Context, a deploy.Actor, r deploy.RunRequest) (deploy.Run, error) {
	return deploy.Run{ID: r.RunID}, f.record(call{deploy.VerbResume, a})
}

func (f *fakeEngine) Cancel(_ context.Context, a deploy.Actor, r deploy.RunRequest) (deploy.Run, error) {
	return deploy.Run{ID: r.RunID}, f.record(call{deploy.VerbCancel, a})
}

func (f *fakeEngine) Approve(_ context.Context, a deploy.Actor, r deploy.RunRequest) (deploy.Run, error) {
	return deploy.Run{ID: r.RunID}, f.record(call{deploy.VerbApprove, a})
}

// request is the union of the fields the verbs read, so one table row can
// address any verb.
type request struct {
	verb  string
	actor string
	kind  deploy.RunKind
	runID deploy.RunID
}

func wellFormed(verb, actor string) request {
	return request{verb: verb, actor: actor, kind: deploy.KindRelease, runID: "run-1"}
}

// outcome is everything a test asserts about one verb call, compared whole.
type outcome struct {
	Refusal domainrpc.Refusal
	Payload bool
	Calls   []call
}

// send drives r through the same bound handlers Serve registers.
func send(e *fakeEngine, r request) outcome {
	refusal, payload := dispatch(newVerbs(e, fakeAuth{}), r)
	return outcome{Refusal: refusal, Payload: payload, Calls: e.calls}
}

func dispatch(v verbs, r request) (domainrpc.Refusal, bool) {
	ctx := context.Background()
	switch r.verb {
	case deploy.VerbPlan:
		rep := v.plan.Handle(ctx, deploy.PlanRequest{ActorID: r.actor, Kind: r.kind})
		return rep.Refusal, rep.Plan != nil
	case deploy.VerbStart:
		rep := v.start.Handle(ctx, deploy.StartRequest{ActorID: r.actor, Kind: r.kind})
		return rep.Refusal, rep.Run != nil
	case deploy.VerbList:
		rep := v.list.Handle(ctx, deploy.ListRequest{ActorID: r.actor})
		return rep.Refusal, rep.Runs != nil
	}
	i := slices.IndexFunc(v.runs, func(rv bus.Verb[deploy.RunRequest, deploy.RunReply]) bool { return rv.Name == r.verb })
	rep := v.runs[i].Handle(ctx, deploy.RunRequest{ActorID: r.actor, RunID: r.runID})
	return rep.Refusal, rep.Run != nil
}

func boundNames(v verbs) []string {
	names := []string{v.plan.Name, v.start.Name, v.list.Name}
	for _, rv := range v.runs {
		names = append(names, rv.Name)
	}
	return names
}

func TestEveryDeployVerbIsBound(t *testing.T) {
	assert.ElementsMatch(t, allVerbs, boundNames(newVerbs(&fakeEngine{}, fakeAuth{})))
}

func TestGuard(t *testing.T) {
	for _, verb := range allVerbs {
		t.Run(verb, func(t *testing.T) { checkGuard(t, verb) })
	}
}

// checkGuard: a non-owner and an actor-less request never reach the engine,
// and the owner reaches it as the actor the authorizer resolved.
func checkGuard(t *testing.T, verb string) {
	owner := deploy.Actor{ID: ownerID, Login: ownerLogin}
	cases := []struct {
		name  string
		actor string
		want  outcome
	}{
		{"non-owner refused", viewerID, outcome{Refusal: domainrpc.Refused(domainrpc.CodeForbidden, ports.ErrForbidden.Error())}},
		{"no actor is malformed", "", outcome{Refusal: domainrpc.Refused(domainrpc.CodeInvalid, "invalid request: actor_id is required")}},
		{"owner allowed", ownerID, outcome{Payload: true, Calls: []call{{verb, owner}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, send(&fakeEngine{}, wellFormed(verb, tc.actor)))
		})
	}
}

func TestMalformedRequestRefusedBeforeEngine(t *testing.T) {
	cases := []struct {
		name string
		req  request
		want string
	}{
		{"start without kind", request{verb: deploy.VerbStart, actor: ownerID}, `unknown kind ""`},
		{"start unknown kind", request{verb: deploy.VerbStart, actor: ownerID, kind: "yolo"}, `unknown kind "yolo"`},
		{"plan unknown kind", request{verb: deploy.VerbPlan, actor: ownerID, kind: "yolo"}, `unknown kind "yolo"`},
		{"get without run", request{verb: deploy.VerbGet, actor: ownerID}, "run_id is required"},
		{"resume without run", request{verb: deploy.VerbResume, actor: ownerID}, "run_id is required"},
		{"cancel without run", request{verb: deploy.VerbCancel, actor: ownerID}, "run_id is required"},
		{"approve without run", request{verb: deploy.VerbApprove, actor: ownerID}, "run_id is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := outcome{Refusal: domainrpc.Refused(domainrpc.CodeInvalid, "invalid request: "+tc.want)}
			assert.Equal(t, want, send(&fakeEngine{}, tc.req))
		})
	}
}

// A plan with no kind is the page's overview, not a malformed request.
func TestPlanWithoutKindIsTheOverview(t *testing.T) {
	req := request{verb: deploy.VerbPlan, actor: ownerID}
	want := outcome{Payload: true, Calls: []call{{deploy.VerbPlan, deploy.Actor{ID: ownerID, Login: ownerLogin}}}}
	assert.Equal(t, want, send(&fakeEngine{}, req))
}

func TestEngineErrorMapsToCode(t *testing.T) {
	cases := []struct {
		err  error
		code domainrpc.Code
	}{
		{ports.ErrInvalid, domainrpc.CodeInvalid},
		{fmt.Errorf("%w: run r9", ports.ErrNotFound), domainrpc.CodeNotFound},
		{ports.ErrForbidden, domainrpc.CodeForbidden},
		{ports.ErrConflict, domainrpc.CodeConflict},
		{ports.ErrLockHeld, domainrpc.CodeConflict},
		{ports.ErrNotResumable, domainrpc.CodeConflict},
		{fmt.Errorf("store: %w", context.DeadlineExceeded), domainrpc.CodeUnavailable},
		{ports.ErrRefused, domainrpc.CodeInternal},
		{errors.New("boom"), domainrpc.CodeInternal},
	}
	for _, tc := range cases {
		t.Run(tc.err.Error(), func(t *testing.T) {
			for _, verb := range allVerbs {
				got := send(&fakeEngine{err: tc.err}, wellFormed(verb, ownerID))
				assert.Equal(t, domainrpc.Refused(tc.code, tc.err.Error()), got.Refusal, verb)
				assert.False(t, got.Payload, "%s: refused reply must not carry a payload", verb)
			}
		})
	}
}
