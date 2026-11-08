// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package fortnite

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/app/gossip/internal/providertest"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
)

// Time the first stats command immediately after a fresh session snapshot.
// Snapshot/provider setup is excluded; every upstream call takes 50ms.
func BenchmarkFortniteStatsAfterSnapshot(b *testing.B) {
	var requests atomic.Int64
	url := providertest.Upstream(b, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		time.Sleep(50 * time.Millisecond)
		switch r.URL.Path {
		case displayNamePath + "Ninja":
			_, _ = w.Write([]byte(`{"id":"deadbeef","displayName":"Ninja"}`))
		case statsLookupPath:
			_, _ = w.Write([]byte(syntheticBlob))
		default:
			b.Errorf("unexpected upstream path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	var commandRequests int64
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		p := New(Config{StatsBaseURL: url, ShopBaseURL: url, APIKey: "benchmark-key"}, providertest.Deps(providertest.NewMemStore()))
		req := gossiprpc.Request{Account: "Ninja", ChannelID: "42"}
		start := providertest.Call[gossiprpc.FortniteSnapshotReply](b, p, "session_start", req)
		if start.Error != "" {
			b.Fatal(start.Error)
		}
		handle := providertest.Endpoint(b, p, "stats")
		before := requests.Load()
		b.StartTimer()
		reply := providertest.Decode[gossiprpc.FortniteStatsReply](b, handle(context.Background(), req))
		if reply.Error != "" {
			b.Fatal(reply.Error)
		}
		b.StopTimer()
		commandRequests += requests.Load() - before
	}
	b.ReportMetric(float64(commandRequests)/float64(b.N), "command_http/op")
}
