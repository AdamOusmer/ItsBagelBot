// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/internal/domain/rpc"
	automationrpc "ItsBagelBot/internal/domain/rpc/automation"
	automationsrpc "ItsBagelBot/internal/domain/rpc/automations"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus/bustest"
	"ItsBagelBot/pkg/codec"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

type listSource []automation.Automation

func (s listSource) Automations(context.Context, uint64) ([]automation.Automation, error) {
	return s, nil
}

func (s listSource) AutomationIndex(context.Context, uint64) (*automation.TriggerIndex, error) {
	return automation.CompileIndex(s), nil
}

type rpcFixture struct {
	p     *Pipeline
	pub   *fakePublisher
	nc    *nats.Conn
	runs  *ValkeyRunLog
	valk  valkey.Client
	bid   uint64
	bidSt string
}

func automationOn() map[string]projection.ModuleView {
	return map[string]projection.ModuleView{AutomationsModuleName: {IsEnabled: true}}
}

func chatAutomation(id, name, trigger string, params string) automation.Automation {
	action, _ := json.Marshal(map[string]any{"text": "hello from " + name})
	return automation.Automation{
		ID: id, UserID: 1, Name: name, Enabled: true,
		Definition: automation.Definition{
			Version: 1,
			Trigger: automation.Trigger{Type: trigger, Params: json.RawMessage(params)},
			Actions: []automation.Action{{Type: "chat", Params: action}},
		},
	}
}

func newRPCFixture(t *testing.T, modules map[string]projection.ModuleView, autos ...automation.Automation) *rpcFixture {
	t.Helper()
	valk := newHotPathTestClient(t)
	fx := &rpcFixture{pub: &fakePublisher{}, valk: valk, runs: NewValkeyRunLog(valk, zap.NewNop()), bid: uniqueBroadcaster()}
	fx.bidSt = strconv.FormatUint(fx.bid, 10)
	d := Deps{
		Proj: fakeReader{modules: modules}, Live: liveAlways{}, Cooldown: NoopCooldown{}, Pub: fx.pub, Log: zap.NewNop(),
		Dedup:       NewEventDedup(newRecordingStore(), "t:", time.Minute, zap.NewNop()),
		Automations: AutomationDeps{Source: listSource(autos), Runs: fx.runs, Limiter: NewValkeyRunLimiter(valk)},
	}
	fx.p = NewPipeline(d, NewRegistry(zap.NewNop()), Config{OutgressPremium: premiumSubj, OutgressStandard: standardSubj})
	fx.nc = bustest.NATS(t)
	require.NoError(t, SubscribeAutomationRPC(fx.nc, "sesame-rpc", AutomationRPCDeps{Pipeline: fx.p, Client: valk, Log: zap.NewNop()}))
	return fx
}

func call[T any](t *testing.T, nc *nats.Conn, subject string, req any) T {
	t.Helper()
	body, err := codec.Marshal(req)
	require.NoError(t, err)
	msg, err := nc.Request(subject, body, 5*time.Second)
	require.NoError(t, err)
	var out T
	require.NoError(t, codec.Unmarshal(msg.Data, &out))
	return out
}

func (fx *rpcFixture) run(t *testing.T, name, source, requestID string) automationsrpc.RunReply {
	t.Helper()
	return call[automationsrpc.RunReply](t, fx.nc, automationsrpc.SubjectRun, automationsrpc.RunRequest{
		BroadcasterID: fx.bidSt, Name: name, Source: source, RequestID: requestID,
	})
}

func (fx *rpcFixture) chatCount() int { return len(fx.pub.snapshot()) }

func TestAutomationRunRPCAcceptsAndPublishesOutput(t *testing.T) {
	fx := newRPCFixture(t, automationOn(), chatAutomation("a1", "greet", "manual", `{}`))

	reply := fx.run(t, "GREET", automationsrpc.SourceDashboard, "req-1")

	require.True(t, reply.Accepted, reply.Error)
	require.NotEmpty(t, reply.RunID)
	require.Equal(t, 1, fx.chatCount())
	got := fx.pub.snapshot()[0]
	assert.Contains(t, []string{premiumSubj, standardSubj}, got.subject)
	assert.JSONEq(t, `{"broadcaster_id":"`+fx.bidSt+`","message":"hello from greet"}`, string(got.msg.Payload))
}

func TestAutomationRunRPCRefusals(t *testing.T) {
	cases := []struct {
		name    string
		modules map[string]projection.ModuleView
		runName string
		source  string
		reqID   string
		code    rpc.Code
	}{
		{"unknown name", automationOn(), "missing", automationsrpc.SourceDashboard, "r", rpc.CodeNotFound},
		{"invalid source", automationOn(), "greet", "web", "r", rpc.CodeInvalid},
		{"missing request id", automationOn(), "greet", automationsrpc.SourceDashboard, "", rpc.CodeInvalid},
		{"module off", nil, "greet", automationsrpc.SourceDashboard, "r", rpc.CodeForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fx := newRPCFixture(t, tc.modules, chatAutomation("a1", "greet", "manual", `{}`))

			reply := fx.run(t, tc.runName, tc.source, tc.reqID)

			assert.False(t, reply.Accepted)
			assert.Equal(t, tc.code, reply.Code)
			assert.Zero(t, fx.chatCount())
		})
	}
}

func TestAutomationRunRPCDuplicateRequestRunsOnce(t *testing.T) {
	fx := newRPCFixture(t, automationOn(), chatAutomation("a1", "greet", "manual", `{}`))

	first := fx.run(t, "greet", automationsrpc.SourceDashboard, "same")
	second := fx.run(t, "greet", automationsrpc.SourceDashboard, "same")

	require.True(t, first.Accepted)
	assert.True(t, second.Accepted)
	assert.Equal(t, first.RunID, second.RunID)
	assert.Equal(t, 1, fx.chatCount())
}

const probeRequestID = "probe"

func (fx *rpcFixture) holdClaim(t *testing.T) {
	t.Helper()
	cmd := fx.valk.B().Set().Key(runRequestKey(fx.bid, probeRequestID)).Value(pkg_valkey.ClaimValue).Build()
	require.NoError(t, fx.valk.Do(context.Background(), cmd).Error())
}

func (fx *rpcFixture) claimExists(t *testing.T) bool {
	t.Helper()
	n, err := fx.valk.Do(context.Background(), fx.valk.B().Exists().Key(runRequestKey(fx.bid, probeRequestID)).Build()).AsInt64()
	require.NoError(t, err)
	return n == 1
}

func TestAutomationRunRPCPendingClaimRepliesAcceptedWithoutRunID(t *testing.T) {
	fx := newRPCFixture(t, automationOn(), chatAutomation("a1", "greet", "manual", `{}`))
	fx.holdClaim(t)

	reply := fx.run(t, "greet", automationsrpc.SourceDashboard, probeRequestID)

	assert.True(t, reply.Accepted)
	assert.Empty(t, reply.RunID)
	assert.Zero(t, fx.chatCount())
}

func TestAutomationRunRPCRefusalReleasesClaim(t *testing.T) {
	fx := newRPCFixture(t, automationOn(), chatAutomation("a1", "greet", "manual", `{}`))

	reply := fx.run(t, "no-such-automation", automationsrpc.SourceDashboard, probeRequestID)

	require.False(t, reply.Accepted)
	assert.False(t, fx.claimExists(t))
}

func TestValkeyRunLogAndLimiterReadAndWriteThroughPrimary(t *testing.T) {
	client := newHotPathTestClient(t)

	assert.True(t, pkg_valkey.IsPrimary(NewValkeyRunLog(client, zap.NewNop()).client))
	assert.True(t, pkg_valkey.IsPrimary(NewValkeyRunLimiter(client).client))
}

func TestAutomationRunsRPCReturnsNewestFirstWithFilter(t *testing.T) {
	fx := newRPCFixture(t, automationOn(),
		chatAutomation("a1", "one", "manual", `{}`), chatAutomation("a2", "two", "manual", `{}`))
	var ids []string
	for i, name := range []string{"one", "two", "one"} {
		reply := fx.run(t, name, automationsrpc.SourceDashboard, "r"+strconv.Itoa(i))
		require.True(t, reply.Accepted)
		ids = append(ids, reply.RunID)
	}

	all := call[automationrpc.RunsReply](t, fx.nc, automationrpc.RunsSubject, automationrpc.RunsRequest{BroadcasterID: fx.bidSt})
	assert.Equal(t, []string{ids[2], ids[1], ids[0]}, runIDs(all.Runs))

	one := call[automationrpc.RunsReply](t, fx.nc, automationrpc.RunsSubject, automationrpc.RunsRequest{BroadcasterID: fx.bidSt, AutomationID: "a1", Limit: 1})
	assert.Equal(t, []string{ids[2]}, runIDs(one.Runs))

	bad := call[automationrpc.RunsReply](t, fx.nc, automationrpc.RunsSubject, automationrpc.RunsRequest{BroadcasterID: "x"})
	assert.Equal(t, rpc.CodeInvalid, bad.Code)
}

func TestRunNamedChannelRateCapRecordsLimited(t *testing.T) {
	fx := newRPCFixture(t, automationOn(), chatAutomation("a1", "greet", "manual", `{}`))
	for i := 0; i < automation.MaxRunsPerMinute; i++ {
		_, err := NewValkeyRunLimiter(fx.valk).AllowRun(context.Background(), fx.bid)
		require.NoError(t, err)
	}

	reply := fx.p.RunNamed(context.Background(), automationsrpc.RunRequest{BroadcasterID: fx.bidSt, Name: "greet", Source: automationsrpc.SourceDashboard, RequestID: "over"})

	require.True(t, reply.Accepted)
	recs, err := fx.runs.Recent(context.Background(), fx.bid, "", 1)
	require.NoError(t, err)
	require.Len(t, recs, 1)
	assert.Equal(t, automationrpc.OutcomeLimited, recs[0].Outcome)
	assert.Zero(t, fx.chatCount())
}
