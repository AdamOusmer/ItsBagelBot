// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"bytes"
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/rpc"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type echoReply struct {
	From  string `json:"from,omitempty"`
	Error string `json:"error,omitempty"`
}

func rpcSubjectName() string { return "bagel.rpc.test." + nuid.Next() }

func respondFrom(name string, calls *atomic.Int64) nats.MsgHandler {
	return func(msg *nats.Msg) {
		calls.Add(1)
		_ = Respond(msg, echoReply{From: name})
	}
}

type rpcCaller struct {
	node    string
	subject string
}

func requestFrom(t *testing.T, nc *nats.Conn, caller rpcCaller, timeout time.Duration) (echoReply, error) {
	t.Helper()
	t.Setenv("NODE_NAME", caller.node)
	return RequestJSONTimeout[echoReply](context.Background(), nc, caller.subject, map[string]string{}, timeout)
}

func TestFastMarshalErrorEnvelopeHitsProbe(t *testing.T) {
	type errorEnvelope struct {
		Error string `json:"error"`
	}

	for name, body := range map[string]any{
		"struct field": errorEnvelope{Error: "service draining"},
		"map key":      map[string]string{"error": "internal error"},
		"nested payload": struct {
			Result string            `json:"result"`
			Error  string            `json:"error"`
			Detail map[string]string `json:"detail,omitempty"`
		}{Result: "", Error: "bad request", Detail: map[string]string{"code": "E400"}},
	} {
		t.Run(name, func(t *testing.T) {
			fast, err := codec.FastMarshal(body)
			require.NoError(t, err)
			std, err := codec.Marshal(body)
			require.NoError(t, err)

			assert.True(t, bytes.Contains(fast, errorFieldProbe), "FastMarshal output %s misses probe %q", fast, errorFieldProbe)
			require.NotEmpty(t, ReplyErrorMessage(std), "the std encoding is also missed by ReplyErrorMessage")
			assert.Equal(t, ReplyErrorMessage(std), ReplyErrorMessage(fast))
		})
	}
}

func TestRPCErrorMessageValueFalsePositiveStaysSuccess(t *testing.T) {
	data, err := codec.FastMarshal(map[string]map[string]string{"meta": {"error": "ignored"}})
	require.NoError(t, err)

	assert.True(t, bytes.Contains(data, errorFieldProbe), "the fixture must contain the probe bytes")
	assert.Empty(t, ReplyErrorMessage(data))
}

func TestServeWithinSetsTheHandlerDeadline(t *testing.T) {
	nc := testnats.Connect(t)
	base := RPCWiring{NC: nc, Queue: "svc-rpc"}
	remaining := func(ctx context.Context, _ map[string]string) map[string]int64 {
		deadline, _ := ctx.Deadline()
		return map[string]int64{"seconds": int64(time.Until(deadline).Round(time.Second) / time.Second)}
	}
	defaultSubject, slowSubject := rpcSubjectName(), rpcSubjectName()
	require.NoError(t, Serve(base, defaultSubject, remaining))
	require.NoError(t, Serve(base.Within(30*time.Second), slowSubject, remaining))

	fast, err := RequestJSON[map[string]int64](context.Background(), nc, defaultSubject, map[string]string{})
	require.NoError(t, err)
	slow, err := RequestJSON[map[string]int64](context.Background(), nc, slowSubject, map[string]string{})
	require.NoError(t, err)

	assert.Equal(t, []int64{int64(DefaultRPCTimeout / time.Second), 30}, []int64{fast["seconds"], slow["seconds"]})
}

func TestRequestsPreferTheNodeLocalSubscriberAndFallBackWithoutResponders(t *testing.T) {
	nc := testnats.Connect(t)
	subject := rpcSubjectName()
	var onNode1, onNode2 atomic.Int64
	t.Setenv("NODE_NAME", "node1")
	require.NoError(t, QueueSubscribeRPC(nc, subject, "svc", respondFrom("node1", &onNode1)))
	t.Setenv("NODE_NAME", "node2")
	require.NoError(t, QueueSubscribeRPC(nc, subject, "svc", respondFrom("node2", &onNode2)))
	require.NoError(t, nc.Flush())

	local1, err1 := requestFrom(t, nc, rpcCaller{"node1", subject}, time.Second)
	local2, err2 := requestFrom(t, nc, rpcCaller{"node2", subject}, time.Second)
	fallback, err3 := requestFrom(t, nc, rpcCaller{"node3", subject}, time.Second)

	require.NoError(t, errors.Join(err1, err2, err3))
	assert.Equal(t, []string{"node1", "node2"}, []string{local1.From, local2.From})
	assert.Contains(t, []string{"node1", "node2"}, fallback.From, "a node without a local subscriber must fall back to the generic subject")
}

func TestRequestsNeverReplayAnAmbiguousFailure(t *testing.T) {
	nc := testnats.Connect(t)
	subject := rpcSubjectName()
	var calls atomic.Int64
	t.Setenv("NODE_NAME", "node1")
	require.NoError(t, QueueSubscribeRPC(nc, subject, "svc", func(*nats.Msg) { calls.Add(1) }))
	require.NoError(t, nc.Flush())

	_, err := requestFrom(t, nc, rpcCaller{"node1", subject}, 300*time.Millisecond)

	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.EqualValues(t, 1, calls.Load(), "a request that may have executed must not be sent again")
}

func TestAnInvalidNodeNameRegistersTheGenericSubjectOnly(t *testing.T) {
	nc := testnats.Connect(t)
	subject := rpcSubjectName()
	var calls atomic.Int64
	for _, node := range []string{"zone.node2", "node*", "node 2"} {
		t.Setenv("NODE_NAME", node)
		require.NoError(t, QueueSubscribeRPC(nc, subject, "svc", respondFrom("generic", &calls)))
	}
	require.NoError(t, nc.Flush())

	_, nodeLocal := nc.Request(subject+".node.zone.node2", nil, 200*time.Millisecond)
	generic, err := requestFrom(t, nc, rpcCaller{"", subject}, time.Second)

	assert.ErrorIs(t, nodeLocal, nats.ErrNoResponders, "an invalid node name must not register a node-local subject")
	require.NoError(t, err)
	assert.Equal(t, "generic", generic.From)
}

func TestQueueSubscribeJSONRepliesWithTheHandlerResultOrAStructuredError(t *testing.T) {
	nc := testnats.Connect(t)
	subject := rpcSubjectName()
	require.NoError(t, QueueSubscribeJSON(nc, subject, "svc", time.Second, nil, zap.NewNop(),
		func(_ context.Context, req map[string]string) echoReply {
			switch req["mode"] {
			case "panic":
				panic("boom")
			case "refuse":
				return echoReply{Error: "not allowed"}
			}
			return echoReply{From: "handler"}
		}))

	for _, tc := range []struct {
		name    string
		payload string
		want    echoReply
		wantErr string
	}{
		{"the handler result is the reply", `{}`, echoReply{From: "handler"}, ""},
		{"a handler refusal surfaces as an RPC reply error", `{"mode":"refuse"}`, echoReply{}, "not allowed"},
		{"a handler panic is answered as an internal error", `{"mode":"panic"}`, echoReply{}, "internal error"},
		{"an undecodable request is answered as a bad request", `{not json`, echoReply{}, "bad request"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reply, err := requestRaw(t, nc, subject, tc.payload)

			assert.Equal(t, tc.want, reply)
			if tc.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			var replyErr RPCReplyError
			require.ErrorAs(t, err, &replyErr)
			assert.Equal(t, tc.wantErr, replyErr.Message)
		})
	}
}

func requestRaw(t *testing.T, nc *nats.Conn, subject, payload string) (echoReply, error) {
	t.Helper()
	msg, err := nc.Request(subject, []byte(payload), time.Second)
	require.NoError(t, err)
	if message := ReplyErrorMessage(msg.Data); message != "" {
		return echoReply{}, RPCReplyError{Subject: subject, Message: message}
	}
	var reply echoReply
	require.NoError(t, codec.Unmarshal(msg.Data, &reply))
	return reply, nil
}

func TestRequestJSONSurfacesRPCReplyErrorsAndMarshalFailures(t *testing.T) {
	nc := testnats.Connect(t)
	subject := rpcSubjectName()
	require.NoError(t, Serve(RPCWiring{NC: nc, Queue: "svc", Log: zap.NewNop()}, subject,
		func(context.Context, map[string]string) echoReply { return echoReply{Error: "service draining"} }))

	_, replyErr := RequestJSON[echoReply](context.Background(), nc, subject, map[string]string{})
	_, marshalErr := RequestJSON[echoReply](context.Background(), nc, subject, make(chan int))

	var typed RPCReplyError
	require.ErrorAs(t, replyErr, &typed)
	assert.Equal(t, "service draining", typed.Message)
	assert.EqualError(t, typed, "rpc "+subject+": service draining")
	assert.ErrorContains(t, marshalErr, "marshal request")
}

func TestRequestJSONKeepsRefusalCode(t *testing.T) {
	nc := testnats.Connect(t)
	subject := rpcSubjectName()
	want := rpc.Refused(rpc.CodeNotFound, "user account not found")
	reply, err := codec.FastMarshal(struct {
		UserID string `json:"user_id"`
		rpc.Refusal
	}{UserID: "42", Refusal: want})
	require.NoError(t, err)
	_, err = nc.Subscribe(subject, func(msg *nats.Msg) { _ = msg.Respond(reply) })
	require.NoError(t, err)

	_, err = RequestJSONTimeout[struct{}](t.Context(), nc, subject, struct{}{}, time.Second)

	var got RPCReplyError
	require.ErrorAs(t, err, &got)
	assert.Equal(t, RPCReplyError{Subject: subject, Message: want.Error, Code: want.Code}, got)
}

func TestReplyErrorMessageToleratesNonStringCode(t *testing.T) {
	assert.Equal(t, "slow down", ReplyErrorMessage([]byte(`{"error":"slow down","code":429}`)))
}
