package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
)

func TestLatencySampleRequirementUsesRateControlledDuration(t *testing.T) {
	cfg := config{
		messages: 42_000, targetRate: 42_000, latencySamples: 20,
		latencyInterval: 50 * time.Millisecond, maxP99: 50 * time.Millisecond,
	}
	if got := latencySampleRequirement(cfg); got != 8 {
		t.Fatalf("latency requirement = %d, want 8", got)
	}
	cfg.targetRate = 0
	if got := latencySampleRequirement(cfg); got != 1 {
		t.Fatalf("unpaced latency requirement = %d, want 1", got)
	}
	cfg.latencySamples = 0
	if got := latencySampleRequirement(cfg); got != 0 {
		t.Fatalf("disabled latency requirement = %d, want 0", got)
	}
}

func TestMissedPublisherStartBarrierFails(t *testing.T) {
	if _, err := waitForStart(time.Now().Add(-2 * time.Second).UnixMilli()); err == nil {
		t.Fatal("publisher accepted a missed shared start barrier")
	}
	started, err := waitForStart(0)
	if err != nil || started.IsZero() {
		t.Fatalf("unbarriered local start failed: started=%v err=%v", started, err)
	}
}

func TestPublishModesHonorCanceledProcessContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, mode := range []string{"async", "atomic", "fast"} {
		job := publishJob{
			ctx:      ctx,
			cfg:      config{mode: mode},
			counters: &benchmarkCounters{},
		}
		if err := publishByMode(job); !errors.Is(err, context.Canceled) {
			t.Fatalf("mode %s cancellation error = %v", mode, err)
		}
	}
}

func TestBenchmarkContextEnforcesRunTimeout(t *testing.T) {
	ctx, cancel := benchmarkContext(config{runTimeout: time.Millisecond})
	defer cancel()
	select {
	case <-ctx.Done():
		require.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)
	case <-time.After(time.Second):
		t.Fatal("benchmark context ignored its run timeout")
	}
}

func TestBenchmarkConnectionsDisableReconnectBuffering(t *testing.T) {
	url := runMinimalNATSServer(t)
	nc, err := nats.Connect(url, connectionOptions(config{}, nil, "test", &connectionStats{})...)
	require.NoError(t, err)
	t.Cleanup(nc.Close)
	// This must be asserted after Connect: nats.go replaces zero with its
	// default 8 MiB reconnect buffer while constructing the connection.
	require.Equal(t, -1, nc.Opts.ReconnectBufSize)
}

func runMinimalNATSServer(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })

	go serveMinimalNATS(listener)
	return "nats://" + listener.Addr().String()
}

func serveMinimalNATS(listener net.Listener) {
	conn, err := listener.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	if err := writeMinimalNATSInfo(conn, listener); err != nil {
		return
	}
	answerNATSPings(conn)
}

func writeMinimalNATSInfo(conn net.Conn, listener net.Listener) error {
	port := listener.Addr().(*net.TCPAddr).Port
	_, err := fmt.Fprintf(conn,
		"INFO {\"server_id\":\"test\",\"version\":\"2.14.0\",\"proto\":1,\"host\":\"127.0.0.1\",\"port\":%d,\"max_payload\":1048576}\r\n",
		port,
	)
	return err
}

func answerNATSPings(conn net.Conn) {
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		if scanner.Text() == "PING" {
			_, _ = conn.Write([]byte("PONG\r\n"))
		}
	}
}

func TestAcknowledgementGapTracksRecoveredStalls(t *testing.T) {
	counters := &benchmarkCounters{}
	counters.beginAckWindows(time.Now(), 2)
	counters.recordAcknowledgedAt(0, 10, 100*time.Millisecond)
	counters.recordAcknowledgedAt(0, 10, 250*time.Millisecond)
	counters.recordAcknowledgedAt(0, 10, 2*time.Second)
	counters.recordAcknowledgedAt(1, 10, 100*time.Millisecond)
	finished := counters.started.Add(2 * time.Second)
	counters.finishAckWindows(finished)

	require.Equal(t, int64(40), counters.acked.Load())
	require.Equal(t, 1900*time.Millisecond, counters.maximumAckGap())
	result := result{
		Acknowledged:            40,
		MaxPublishProgressGapMS: durationMS(counters.maximumAckGap()),
	}
	err := validateBenchmark(config{messages: 40, mode: "atomic", maxAckGap: time.Second}, result, nil)
	require.ErrorContains(t, err, "maximum publisher completion-progress gap")
}

func TestFastPublisherCountsFlowAckDeltas(t *testing.T) {
	counters := &benchmarkCounters{}
	counters.beginAckWindows(time.Now(), 1)
	session := fastPublishSession{
		job: publishJob{publisher: 0, count: 100, counters: counters},
	}

	session.recordAckSequenceAt(1, 10*time.Millisecond)
	session.recordAckSequenceAt(64, 20*time.Millisecond)
	session.recordAckSequenceAt(64, 30*time.Millisecond)
	session.recordAckSequenceAt(120, 40*time.Millisecond)
	counters.finishAckWindows(counters.started.Add(50 * time.Millisecond))

	require.Equal(t, uint64(100), session.acked)
	require.Equal(t, int64(100), counters.acked.Load())
	require.Equal(t, 20*time.Millisecond, counters.maximumAckGap())
}

func TestAsyncDrainObservesResolvedPubAcksBeforeWindowFills(t *testing.T) {
	counters := &benchmarkCounters{}
	counters.beginAckWindows(time.Now(), 1)
	job := publishJob{publisher: 0, counters: counters}
	ready := resolvedPubAckFuture()
	pending := unresolvedPubAckFuture()

	remaining, err := drainReadyAcknowledgements(job, []pendingPublish{
		{ok: ready.Ok(), err: ready.Err(), sequence: 0},
		{ok: pending.Ok(), err: pending.Err(), sequence: 1},
	})
	require.NoError(t, err)
	require.Len(t, remaining, 1)
	require.Equal(t, messageSequence(1), remaining[0].sequence)
	require.Equal(t, int64(1), counters.acked.Load())
}

func TestAsyncDrainPropagatesReadyPubAckFailure(t *testing.T) {
	counters := &benchmarkCounters{}
	counters.beginAckWindows(time.Now(), 1)
	failed := failedPubAckFuture(nats.ErrTimeout)

	remaining, err := drainReadyAcknowledgements(
		publishJob{publisher: 0, counters: counters},
		[]pendingPublish{{ok: failed.Ok(), err: failed.Err(), sequence: 7}},
	)
	require.Nil(t, remaining)
	require.ErrorContains(t, err, "PubAck 7")
	require.Equal(t, int64(1), counters.failures.Load())
	require.Equal(t, int64(1), counters.timeouts.Load())
}

func TestSampledPubAckMaxRejectsARecoveredLatencyStall(t *testing.T) {
	result := result{Acknowledged: 40, PubAckMaxMS: 2_001}
	err := validateBenchmark(config{messages: 40, maxAckGap: 2 * time.Second}, result, nil)
	require.ErrorContains(t, err, "maximum sampled PubAck RTT")
}

type stubPubAckFuture struct {
	ok  chan *nats.PubAck
	err chan error
}

func resolvedPubAckFuture() *stubPubAckFuture {
	future := unresolvedPubAckFuture()
	future.ok <- &nats.PubAck{}
	return future
}

func failedPubAckFuture(err error) *stubPubAckFuture {
	future := unresolvedPubAckFuture()
	future.err <- err
	return future
}

func unresolvedPubAckFuture() *stubPubAckFuture {
	return &stubPubAckFuture{
		ok:  make(chan *nats.PubAck, 1),
		err: make(chan error, 1),
	}
}

func (f *stubPubAckFuture) Ok() <-chan *nats.PubAck { return f.ok }
func (f *stubPubAckFuture) Err() <-chan error       { return f.err }
func (f *stubPubAckFuture) Msg() *nats.Msg          { return nil }

func TestVariedPayloadRingIsFixedSizeValidJSON(t *testing.T) {
	payloads := benchmarkPayloads(256, 8)
	for i, payload := range payloads {
		if len(payload) != 256 || !json.Valid(payload) {
			t.Fatalf("payload %d length=%d valid=%t", i, len(payload), json.Valid(payload))
		}
	}
	if bytes.Equal(payloads[0], payloads[1]) {
		t.Fatal("payload variants are identical")
	}
}
