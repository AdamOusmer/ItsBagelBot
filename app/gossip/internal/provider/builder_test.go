// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package provider_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"ItsBagelBot/app/gossip/internal/core"
	"ItsBagelBot/app/gossip/internal/provider"
	"ItsBagelBot/app/gossip/internal/providertest"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type testReply struct {
	Player string `json:"player"`
	Value  int    `json:"value"`
	Error  string `json:"error,omitempty"`
}

func testErrReply(id, msg string) any { return testReply{Player: id, Error: msg} }

func noop(context.Context, gossiprpc.Request) any { return nil }

func memDeps() provider.Deps { return providertest.Deps(providertest.NewMemStore()) }

type fetchRecorder struct{ calls int }

func (r *fetchRecorder) fetch(value testReply, err error) provider.FetchFunc {
	return func(context.Context, gossiprpc.Request, provider.ID) (any, error) {
		r.calls++
		return value, err
	}
}

func TestBuildIndexesEndpointsInOrder(t *testing.T) {
	b := provider.NewProvider("demo", memDeps())
	b.Endpoint("one").Timeout(3 * time.Second).Handle(func(context.Context, gossiprpc.Request) any { return "1" })
	b.Endpoint("two").Handle(func(context.Context, gossiprpc.Request) any { return "2" })
	p := b.Build()

	assert.Equal(t, "demo", p.Name())
	require.Len(t, p.Endpoints(), 2)
	assert.Equal(t, "one", p.Endpoints()[0].Name)
	assert.Equal(t, 3*time.Second, p.Endpoints()[0].Timeout)
	assert.Equal(t, "two", p.Endpoints()[1].Name)
	assert.Equal(t, "1", p.Endpoints()[0].Handle(context.Background(), gossiprpc.Request{}))
}

func TestValidateRejectsMisassembly(t *testing.T) {
	cached := func(b *provider.Builder) *provider.FlowBuilder {
		return b.Endpoint("x").Cached(time.Minute, time.Minute)
	}
	for _, tc := range []struct {
		name     string
		provider string
		deps     provider.Deps
		setup    func(b *provider.Builder)
		want     string
	}{
		{"empty provider name", "", memDeps(), func(b *provider.Builder) { b.Endpoint("x").Handle(noop) }, "non-empty name"},
		{"no endpoints", "demo", memDeps(), func(*provider.Builder) {}, "no endpoints"},
		{"empty endpoint name", "demo", memDeps(), func(b *provider.Builder) { b.Endpoint("").Handle(noop) }, "empty name"},
		{"duplicate endpoint", "demo", memDeps(), func(b *provider.Builder) {
			b.Endpoint("x").Handle(noop)
			b.Endpoint("x").Handle(noop)
		}, "twice"},
		{"no terminal", "demo", memDeps(), func(b *provider.Builder) { b.Endpoint("x") }, "no terminal"},
		{"flow without Fetch", "demo", memDeps(), func(b *provider.Builder) { cached(b).Reply(testErrReply) }, "no Fetch"},
		{"flow without Reply", "demo", memDeps(), func(b *provider.Builder) {
			cached(b).Fetch(func(context.Context, gossiprpc.Request, provider.ID) (any, error) { return nil, nil })
		}, "no Reply"},
		{"flow without cache", "demo", provider.Deps{}, func(b *provider.Builder) {
			cached(b).Reply(testErrReply).Fetch(func(context.Context, gossiprpc.Request, provider.ID) (any, error) { return nil, nil })
		}, "Deps.Cache is nil"},
		{"dead trusted flag", "dead", memDeps(), func(b *provider.Builder) {
			b.Trusted()
			b.Endpoint("x").Handle(noop)
		}, ".Trusted()"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := provider.NewProvider(tc.provider, tc.deps)
			tc.setup(b)
			assert.ErrorContains(t, b.Validate(), tc.want)
		})
	}
}

func TestBuilderPanicsOnProgrammerError(t *testing.T) {
	t.Run("building an endpoint without a terminal", func(t *testing.T) {
		b := provider.NewProvider("demo", memDeps())
		b.Endpoint("x")
		assert.Panics(t, func() { b.Build() })
	})
	t.Run("declaring trust after constructing a client", func(t *testing.T) {
		b := provider.NewProvider("late", memDeps())
		b.Client("https://a.invalid", nil, time.Second)
		assert.Panics(t, func() { b.Trusted() })
	})
}

func TestFlowServesAndCaches(t *testing.T) {
	fetches := 0
	b := provider.NewProvider("demo", memDeps())
	b.Endpoint("stats").
		Cached(time.Minute, time.Minute).
		Reply(testErrReply).
		Fallback("stats lookup failed").
		Fetch(func(_ context.Context, _ gossiprpc.Request, id provider.ID) (any, error) {
			fetches++
			return testReply{Player: id.Display, Value: 7}, nil
		})
	h := b.Build().Endpoints()[0].Handle

	first := providertest.Decode[testReply](t, h(context.Background(), gossiprpc.Request{Account: "Techno"}))
	assert.Equal(t, testReply{Player: "Techno", Value: 7}, first)

	res := h(context.Background(), gossiprpc.Request{Account: "techno"})
	assert.IsType(t, codec.RawMessage{}, res, "a cache hit must answer stored wire bytes")
	assert.Equal(t, 1, fetches)
}

func TestCachedUntilBoundsTheCachedWindowByTheDeadline(t *testing.T) {
	shop := func(store *providertest.MemStore, deadline provider.DeadlineFunc, rec *fetchRecorder) provider.HandlerFunc {
		b := provider.NewProvider("demo", providertest.Deps(store))
		b.Endpoint("shop").
			CachedUntil(deadline, time.Minute).
			ID(provider.StaticID("current")).
			Reply(testErrReply).
			Fetch(rec.fetch(testReply{Value: 7}, nil))
		return b.Build().Endpoints()[0].Handle
	}

	t.Run("retains the entry only until the deadline", func(t *testing.T) {
		store := providertest.NewMemStore()
		deadline := time.Now().Add(6 * time.Hour)
		h := shop(store, func(time.Time) time.Time { return deadline }, &fetchRecorder{})

		require.Empty(t, providertest.Decode[testReply](t, h(context.Background(), gossiprpc.Request{})).Error)

		retention := store.Retention(core.Key("demo", "shop", "current"))
		assert.WithinDuration(t, deadline, time.Now().Add(retention), time.Second,
			"the entry must fall out of the store as the deadline passes, not after it")
	})
	t.Run("never serves a reply that was stale on arrival", func(t *testing.T) {
		var rec fetchRecorder
		h := shop(providertest.NewMemStore(), func(now time.Time) time.Time { return now.Add(-time.Hour) }, &rec)

		require.Empty(t, providertest.Decode[testReply](t, h(context.Background(), gossiprpc.Request{})).Error)
		require.Empty(t, providertest.Decode[testReply](t, h(context.Background(), gossiprpc.Request{})).Error)
		assert.Equal(t, 2, rec.calls)
	})
}

func TestFlowAnswersFailuresInChat(t *testing.T) {
	notFound := &core.UpstreamError{Status: 404, Message: "player not found"}
	for _, tc := range []struct {
		name        string
		req         gossiprpc.Request
		fetchErr    error
		want        testReply
		wantFetches int
	}{
		{"rejects a request without an account before fetching", gossiprpc.Request{}, nil,
			testReply{Error: "missing account"}, 0},
		{"answers an absent player from the negative cache", gossiprpc.Request{Account: "ghost"}, notFound,
			testReply{Player: "ghost", Error: "player not found"}, 1},
		{"answers an infrastructure failure with the fallback and never caches it", gossiprpc.Request{Account: "Techno"}, errors.New("upstream unreachable"),
			testReply{Player: "Techno", Error: "stats lookup failed"}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var rec fetchRecorder
			b := provider.NewProvider("demo", memDeps())
			b.Endpoint("stats").
				Cached(time.Minute, time.Minute).
				Reply(testErrReply).
				Fallback("stats lookup failed").
				Fetch(rec.fetch(testReply{}, tc.fetchErr))
			h := b.Build().Endpoints()[0].Handle

			for range 2 {
				assert.Equal(t, tc.want, providertest.Decode[testReply](t, h(context.Background(), tc.req)))
			}
			assert.Equal(t, tc.wantFetches, rec.calls)
		})
	}
}

func TestIDExtractors(t *testing.T) {
	for _, tc := range []struct {
		name       string
		extract    provider.IDFunc
		req        gossiprpc.Request
		want       provider.ID
		wantReject string
	}{
		{"Account trims and folds the key", provider.Account, gossiprpc.Request{Account: "  Techno "}, provider.ID{Display: "Techno", Key: "techno"}, ""},
		{"Account rejects a missing account", provider.Account, gossiprpc.Request{}, provider.ID{}, "missing account"},
		{"Channel trims and keeps the key", provider.Channel, gossiprpc.Request{ChannelID: " 42 "}, provider.ID{Display: "42", Key: "42"}, ""},
		{"Channel rejects a missing channel", provider.Channel, gossiprpc.Request{}, provider.ID{}, "missing channel"},
		{"StaticID ignores the request", provider.StaticID("current"), gossiprpc.Request{Account: "ignored"}, provider.ID{Key: "current"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id, reject := tc.extract(tc.req)
			assert.Equal(t, tc.want, id)
			assert.Equal(t, tc.wantReject, reject)
		})
	}
}

func TestClientLaneFollowsTrustDeclaration(t *testing.T) {
	quiet := provider.Deps{Log: zap.NewNop()}
	trusted := provider.NewProvider("t", quiet).Trusted()
	assert.Equal(t, core.LaneDirect, trusted.Client("https://a.invalid", nil, time.Second).Lane())

	unmarked := provider.NewProvider("u", quiet)
	assert.Equal(t, core.LaneWARP, unmarked.Client("https://b.invalid", nil, time.Second).Lane(),
		"the default must be the untrusted lane; inversion is the point")
}

func TestBuildLogsClientTally(t *testing.T) {
	for _, tc := range []struct {
		name    string
		trusted bool
		clients []string
		want    string
	}{
		{"govee", true, []string{"https://a.invalid", "https://m.invalid"}, "govee: 2 clients (trusted)"},
		{"custom", false, []string{"https://c.invalid"}, "custom: 1 client (warp)"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			observed, logs := observer.New(zap.InfoLevel)
			b := provider.NewProvider(tc.name, provider.Deps{Log: zap.New(observed)})
			if tc.trusted {
				b = b.Trusted()
			}
			for _, base := range tc.clients {
				b.Client(base, nil, time.Second)
			}
			b.Endpoint("e").Handle(noop)
			b.Build()

			assert.Equal(t, 1, logs.FilterMessage(tc.want).Len(), "expected exact tally line, got %v", logs.All())
		})
	}
}
