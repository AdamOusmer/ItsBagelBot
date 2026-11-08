// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package paceman_test

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/app/gossip/internal/providers/paceman"
	"ItsBagelBot/app/gossip/internal/providertest"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
)

// Each independent upstream read takes 50ms. A new store per operation keeps
// both reads cold; provider and fixture setup are excluded from the timer.
func BenchmarkPacemanColdSession(b *testing.B) {
	var requests atomic.Int64
	url := providertest.Upstream(b, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		time.Sleep(50 * time.Millisecond)
		switch r.URL.Path {
		case "/getSessionStats/":
			_, _ = w.Write([]byte(sessionStatsBody))
		case "/getSessionNethers/":
			_, _ = w.Write([]byte(sessionNethersBody))
		default:
			b.Errorf("unexpected upstream path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		p := paceman.New(paceman.Config{BaseURL: url}, providertest.Deps(providertest.NewMemStore()))
		handle := providertest.Endpoint(b, p, "session")
		b.StartTimer()
		reply := providertest.Decode[gossiprpc.PacemanSessionReply](b, handle(context.Background(), gossiprpc.Request{Account: "Feinberg"}))
		if reply.Error != "" {
			b.Fatal(reply.Error)
		}
		b.StopTimer()
	}
	b.ReportMetric(float64(requests.Load())/float64(b.N), "http/op")
}
