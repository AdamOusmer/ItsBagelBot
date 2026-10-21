// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/internal/testnats"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func blockingVerb(probe *overlapProbe) func(context.Context, map[string]string) echoReply {
	return func(context.Context, map[string]string) echoReply {
		probe.handle(nil)
		return echoReply{From: "ok"}
	}
}

func requestConcurrently(nc *nats.Conn, subject string, n int, timeout time.Duration) *sync.WaitGroup {
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			_, _ = RequestJSONTimeout[echoReply](context.Background(), nc, subject, map[string]string{}, timeout)
		})
	}
	return &wg
}

func TestServeRunsHandlersConcurrently(t *testing.T) {
	nc := testnats.Connect(t)
	subject := rpcSubjectName()
	probe := newOverlapProbe(defaultRPCMaxWorkers)
	require.NoError(t, Serve(RPCWiring{NC: nc, Queue: "svc"}, subject, blockingVerb(probe)))

	calls := requestConcurrently(nc, subject, defaultRPCMaxWorkers, rpcPoolTestTimeout)
	probe.awaitArrivals(t, defaultRPCMaxWorkers)
	close(probe.release)
	calls.Wait()

	assert.EqualValues(t, defaultRPCMaxWorkers, probe.peak.Load())
}

func TestServeSkipsRequestsWhoseCallerAlreadyGaveUp(t *testing.T) {
	nc := testnats.Connect(t)
	subject := rpcSubjectName()
	probe := newOverlapProbe(4)
	single := RPCWiring{NC: nc, Queue: "svc", Pool: RPCPoolPolicy{MaxWorkers: 1}}
	require.NoError(t, Serve(single, subject, blockingVerb(probe)))

	head := requestConcurrently(nc, subject, 1, rpcPoolTestTimeout)
	probe.awaitArrivals(t, 1)
	_, err := RequestJSONTimeout[echoReply](context.Background(), nc, subject, map[string]string{}, 100*time.Millisecond)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	close(probe.release)
	head.Wait()

	reply, err := RequestJSONTimeout[echoReply](context.Background(), nc, subject, map[string]string{}, rpcPoolTestTimeout)
	require.NoError(t, err)
	assert.Equal(t, "ok", reply.From)
	assert.EqualValues(t, 2, probe.completed.Load(), "the request queued behind the head expired and must never run")
}

func TestServeCapsTheHandlerDeadlineAtTheRequesters(t *testing.T) {
	nc := testnats.Connect(t)
	subject := rpcSubjectName()
	var remaining atomic.Int64
	require.NoError(t, ServeMsg(RPCWiring{NC: nc, Queue: "svc"}.Within(30*time.Second), subject, func(ctx context.Context, msg *nats.Msg) {
		deadline, _ := ctx.Deadline()
		remaining.Store(int64(time.Until(deadline)))
		_ = Respond(msg, echoReply{From: "ok"})
	}))

	_, err := RequestJSONTimeout[echoReply](context.Background(), nc, subject, map[string]string{}, time.Second)

	require.NoError(t, err)
	assert.LessOrEqual(t, time.Duration(remaining.Load()), time.Second)
	assert.Positive(t, remaining.Load())
}
