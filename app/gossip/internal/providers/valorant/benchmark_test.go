// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valorant

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/app/gossip/internal/providertest"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
)

// A cold auto-region rank lookup followed by the same player's account lookup.
// Every upstream call takes 50ms; each operation gets a new cache store.
func BenchmarkValorantRankThenAccount(b *testing.B) {
	var requests atomic.Int64
	url := providertest.Upstream(b, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		time.Sleep(50 * time.Millisecond)
		switch {
		case strings.HasPrefix(r.URL.Path, "/valorant/v2/account/"):
			_, _ = w.Write([]byte(accountBody))
		case strings.HasPrefix(r.URL.Path, "/valorant/v3/mmr/"):
			_, _ = w.Write([]byte(mmrBody))
		default:
			b.Errorf("unexpected upstream path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		p := New(Config{BaseURL: url, ContentBaseURL: url, APIKey: "benchmark-key"}, providertest.Deps(providertest.NewMemStore()))
		rank, account := providertest.Endpoint(b, p, "rank"), providertest.Endpoint(b, p, "account")
		req := gossiprpc.Request{Account: "Frosty#EUW1"}
		b.StartTimer()
		if reply := providertest.Decode[rankReply](b, rank(context.Background(), req)); reply.Error != "" {
			b.Fatal(reply.Error)
		}
		if reply := providertest.Decode[accountReply](b, account(context.Background(), req)); reply.Error != "" {
			b.Fatal(reply.Error)
		}
		b.StopTimer()
	}
	b.ReportMetric(float64(requests.Load())/float64(b.N), "http/op")
}
