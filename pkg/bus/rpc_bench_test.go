// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"go.uber.org/zap"

	"ItsBagelBot/pkg/codec"
)

// These benchmarks exist because the 1ms-p99 question cannot be answered by
// arguing about allocations: it needs the four terms of an RPC round trip
// priced separately. Each sub-benchmark isolates one term, so
//
//	json_serial ≈ codec_only + core_request + server pipeline (+ handoff)
//
// and whichever term actually owns the tail says which fix matters:
//
//   - codec_only    — client-side encode/decode, no wire;
//   - core_request  — raw nats.go request machinery plus transport RTT, no
//     JSON, no New Relic, no pool handoff;
//   - json_serial   — the production pipeline end to end, handler inline on
//     the delivery goroutine;
//   - json_pooled   — the same over QueueSubscribeJSONConcurrent; the delta
//     against json_serial is the pool handoff price.
//
// The embedded broker is single-process loopback, so ABSOLUTE numbers are a
// lower bound on the fleet's — there is no network between caller and
// responder. What transfers to production is the deltas between rows, which is
// exactly the decomposition the p99 work needs. For fleet-absolute numbers run
// deploy/k8s/bus-bench across nodes.

type rpcBenchRequest struct {
	ChannelID string `json:"channel_id"`
	Limit     int    `json:"limit"`
}

type rpcBenchReply struct {
	Names []string `json:"names"`
	Total int      `json:"total"`
}

const rpcBenchReplyBody = `{"names":["alpha","beta","gamma","delta","epsilon"],"total":42}`

func BenchmarkRPCRoundTrip(b *testing.B) {
	nc := startRPCBenchBroker(b)

	b.Run("codec_only", func(b *testing.B) {
		replyBody := []byte(rpcBenchReplyBody)
		request := rpcBenchRequest{ChannelID: strings.Repeat("c", 16), Limit: 25}

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			body, err := codec.Marshal(request)
			if err != nil {
				b.Fatal(err)
			}
			var reply rpcBenchReply
			if err := codec.Unmarshal(replyBody, &reply); err != nil {
				b.Fatal(err)
			}
			_ = body
		}
	})

	b.Run("core_request", func(b *testing.B) {
		subject := fmt.Sprintf("bagel.rpc.bench.core.%d", time.Now().UnixNano())
		replyBody := []byte(rpcBenchReplyBody)
		_, err := nc.Subscribe(subject+".server", func(msg *nats.Msg) {
			_ = msg.Respond(replyBody)
		})
		if err != nil {
			b.Fatal(err)
		}
		if err := nc.Flush(); err != nil {
			b.Fatal(err)
		}

		ctx := context.Background()
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, err := nc.RequestWithContext(ctx, subject+".server", []byte(`{}`)); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("json_serial", func(b *testing.B) {
		subject := fmt.Sprintf("bagel.rpc.bench.serial.%d", time.Now().UnixNano())
		registerRPCBenchEndpoint(b, nc, subject, false)
		runRPCBenchRequests(b, nc, subject)
	})

	b.Run("json_pooled", func(b *testing.B) {
		subject := fmt.Sprintf("bagel.rpc.bench.pooled.%d", time.Now().UnixNano())
		registerRPCBenchEndpoint(b, nc, subject, true)
		runRPCBenchRequests(b, nc, subject)
	})
}

// startRPCBenchBroker boots an in-process NATS server on a random port and
// hands back a connection to it. NODE_NAME must be set before any endpoint
// registers, so the node-local half of every subject pair exists and the
// local-first route resolves on its first attempt instead of silently
// measuring the generic-subject fallback.
func startRPCBenchBroker(b *testing.B) *nats.Conn {
	b.Helper()
	b.Setenv("NODE_NAME", "bench-node")

	server, err := natsserver.NewServer(&natsserver.Options{
		Port:   -1,
		NoLog:  true,
		NoSigs: true,
	})
	if err != nil {
		b.Fatal(err)
	}
	server.Start()
	if !server.ReadyForConnections(5 * time.Second) {
		b.Fatal("embedded broker never became ready")
	}
	b.Cleanup(server.Shutdown)

	nc, err := nats.Connect(server.ClientURL())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(nc.Close)
	return nc
}

// registerRPCBenchEndpoint wires one bench endpoint through the production
// registration paths. app stays nil deliberately: every New Relic call in the
// pipeline tolerates a nil application (the same bargain the consume-lane
// telemetry makes), so the bench prices the pipeline shape, not the agent.
func registerRPCBenchEndpoint(b *testing.B, nc *nats.Conn, subject string, pooled bool) {
	b.Helper()

	handle := func(_ context.Context, req rpcBenchRequest) rpcBenchReply {
		return rpcBenchReply{Names: []string{req.ChannelID}, Total: req.Limit}
	}

	if !pooled {
		err := QueueSubscribeJSON(nc, subject, "bench-rpc", 5*time.Second, nil, zap.NewNop(), handle)
		if err != nil {
			b.Fatal(err)
		}
		return
	}

	pool, err := QueueSubscribeJSONConcurrent(nc,
		RPCSubscription{Subject: subject, QueueGroup: "bench-rpc"},
		5*time.Second, nil, zap.NewNop(), handle)
	if err != nil {
		b.Fatal(err)
	}
	// LIFO cleanup: this must run before the connection close registered by
	// startRPCBenchBroker, so no reply is attempted against a closed conn.
	b.Cleanup(func() { _ = pool.Drain(context.Background()) })
}

func runRPCBenchRequests(b *testing.B, nc *nats.Conn, subject string) {
	b.Helper()

	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := RequestJSONTimeout[rpcBenchReply](ctx, nc, subject,
			rpcBenchRequest{ChannelID: strings.Repeat("c", 16), Limit: 25}, 5*time.Second); err != nil {
			b.Fatal(err)
		}
	}
}
