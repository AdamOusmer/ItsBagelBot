// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package urchin

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/app/gossip/internal/core"
	"ItsBagelBot/app/gossip/internal/provider"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// batchWindow is WIDER than the production 2ms default on purpose: a test
// wave of goroutines takes scheduler jitter to spawn, and the point is to
// exercise the real window/sleep/flush machinery deterministically, not the
// window's exact size.
const batchWindow = 15 * time.Millisecond

func newBatchProvider(t *testing.T, handler http.Handler) provider.Provider {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return New(Config{BaseURL: srv.URL, APIKey: "test-key", BatchWindow: batchWindow},
		provider.Deps{Cache: core.NewCache(newMemStore()), Log: zap.NewNop()})
}

// batchRecorder captures POST /v3/players bodies and answers them from a map,
// so several tests share one upstream stub.
type batchRecorder struct {
	mu      sync.Mutex
	batches [][]string // canonical uuids per POST, arrival order
	players map[string][]batchTag
}

func (r *batchRecorder) handle(w http.ResponseWriter, req *http.Request) bool {
	if req.Method != http.MethodPost || req.URL.Path != "/v3/players" {
		return false
	}
	var br batchRequest
	_ = codec.Unmarshal(readAllBody(req), &br)
	r.mu.Lock()
	r.batches = append(r.batches, br.UUIDs)
	players := make(map[string][]batchTag, len(r.players))
	for k, v := range r.players {
		players[k] = v
	}
	r.mu.Unlock()
	payload, _ := codec.Marshal(batchResponse{Players: players})
	_, _ = w.Write(payload)
	return true
}

// readAllBody drains a request body through the codec-only JSON discipline the
// repo's guard test enforces.
func readAllBody(req *http.Request) []byte {
	b, _ := io.ReadAll(req.Body)
	_ = req.Body.Close()
	return b
}

func (r *batchRecorder) all() [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.batches)
}

func (r *batchRecorder) count() int { return len(r.all()) }

// flattenBodies merges every recorded batch into one uuid set.
func flattenBatches(batches [][]string) []string {
	var out []string
	for _, b := range batches {
		out = append(out, b...)
	}
	return out
}

const (
	uuidA = "069a79f444e94726a5befca90e38aaf5"
	uuidB = "b71e0c9d1f2d4c8eab12cd34ef56ab78"
)

func TestCanonicalUUID(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
		ok   bool
	}{
		{uuidA, uuidA, true},
		{"069A79F4-44E9-4726-A5BE-FCA90E38AAF5", uuidA, true}, // dashed uppercase
		{"069a79f4-44e9-4726-a5be-fca90e38aaf5", uuidA, true}, // dashed lowercase
		{"Techno", "", false},
		{"069a79f444e94726a5befca90e38aaf", "", false},  // 31 chars
		{"zzza79f444e94726a5befca90e38aaf5", "", false}, // not hex
	} {
		got, ok := canonicalUUID(account(tc.in))
		assert.Equal(t, tc.ok, ok, tc.in)
		assert.Equal(t, tc.want, got, tc.in)
	}
}

// Two players queried by different channels inside one window must ride ONE
// POST /v3/players carrying both canonical uuids, whatever spelling each
// channel used.
func TestBatchAggregatesDistinctPlayers(t *testing.T) {
	rec := &batchRecorder{players: map[string][]batchTag{
		uuidA: {{TagType: "blatant_cheater", Reason: "Fly / Killaura", AddedOn: 1700000000000}},
		uuidB: {},
	}}
	p := newBatchProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rec.handle(w, r) {
			return
		}
		t.Errorf("unexpected upstream call %s %s", r.Method, r.URL.Path)
	}))
	h := endpoint(t, p, "tags")

	var wg sync.WaitGroup
	replies := make([]gossiprpc.UrchinTagsReply, 2)
	for i, acct := range []string{"069A79F4-44E9-4726-A5BE-FCA90E38AAF5", uuidB} {
		wg.Add(1)
		go func(i int, acct string) {
			defer wg.Done()
			replies[i] = asReply[gossiprpc.UrchinTagsReply](t, h(context.Background(), gossiprpc.Request{Account: acct}))
		}(i, acct)
	}
	wg.Wait()

	require.Equal(t, 1, rec.count(), "distinct players in one window must share one batch POST")
	assert.Equal(t, []string{uuidA, uuidB}, rec.all()[0], "batch body carries canonical undashed uuids")

	require.Empty(t, replies[0].Error)
	require.Len(t, replies[0].Tags, 1)
	assert.Equal(t, gossiprpc.UrchinTag{Type: "blatant_cheater", Reason: "Fly / Killaura", AddedOn: 1700000000}, replies[0].Tags[0])
	// The batch endpoint reports no display names, so the reply echoes the
	// typed identifier instead of a resolved name.
	assert.Equal(t, "069A79F4-44E9-4726-A5BE-FCA90E38AAF5", replies[0].Player)

	// Present-but-tagless is a SUCCESS (empty tag list), not an absence.
	require.Empty(t, replies[1].Error)
	assert.Empty(t, replies[1].Tags)
}

// Identical players spelled differently collapse onto one batch line and one
// shared outcome.
func TestBatchDedupsIdenticalPlayer(t *testing.T) {
	rec := &batchRecorder{players: map[string][]batchTag{
		uuidA: {{TagType: "sniper"}},
	}}
	p := newBatchProvider(t, rec.stubs(t))
	h := endpoint(t, p, "tags")

	const callers = 8
	var wg sync.WaitGroup
	replies := make([]gossiprpc.UrchinTagsReply, callers)
	spellings := []string{uuidA, "069A79F4-44E9-4726-A5BE-FCA90E38AAF5", "069A79F444E94726A5BEFCA90E38AAF5"}
	for i := range replies {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			replies[i] = asReply[gossiprpc.UrchinTagsReply](t, h(context.Background(), gossiprpc.Request{Account: spellings[i%len(spellings)]}))
		}(i)
	}
	wg.Wait()

	require.Equal(t, 1, rec.count())
	assert.Equal(t, []string{uuidA}, rec.all()[0], "duplicate queries must collapse to one batch line")
	for i, r := range replies {
		require.Empty(t, r.Error, "caller %d", i)
		require.Len(t, r.Tags, 1)
	}
}

// The batch hydrates individual playertags cache entries: a follow-up query
// through a DIFFERENT endpoint byte-cache (sniper resolves its uuid via
// playerTags) must find the batch answer warm rather than re-batching.
func TestBatchHydratesSharedPlayertagsCache(t *testing.T) {
	rec := &batchRecorder{players: map[string][]batchTag{
		uuidA: {{TagType: "cheater", Reason: "bhop"}},
	}}
	cubelifyHits := 0
	p := newBatchProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rec.handle(w, r) {
			return
		}
		switch r.URL.Path {
		case "/v3/cubelify":
			cubelifyHits++
			assert.Equal(t, uuidA, r.URL.Query().Get("uuid"), "cubelify must receive the canonical uuid")
			_, _ = w.Write([]byte(`{"score":{"value":7.5,"mode":"warn"},"tags":[]}`))
		default:
			t.Errorf("unexpected upstream call %s %s", r.Method, r.URL.Path)
		}
	}))

	reply := asReply[gossiprpc.UrchinTagsReply](t,
		endpoint(t, p, "tags")(context.Background(), gossiprpc.Request{Account: uuidA}))
	require.Empty(t, reply.Error)

	sniped := asReply[gossiprpc.UrchinSniperReply](t,
		endpoint(t, p, "sniper")(context.Background(), gossiprpc.Request{Account: "069A79F4-44E9-4726-A5BE-FCA90E38AAF5"}))
	require.Empty(t, sniped.Error)
	assert.Equal(t, 7.5, sniped.Score)
	assert.Equal(t, 1, cubelifyHits, "the uuid hop must be served by the hydrated batch entry")
	assert.Equal(t, 1, rec.count(), "the sniper leg must not re-batch a hydrated player")
}

// Players missing from a successful batch response are shaped as 404s AND
// negatively cached, so neither the tags command nor the sniper uuid hop asks
// again while the negative lives.
func TestBatchNegativeCachesMissingPlayers(t *testing.T) {
	rec := &batchRecorder{players: map[string][]batchTag{
		uuidA: {},
	}}
	p := newBatchProvider(t, rec.stubs(t))
	tagsH := endpoint(t, p, "tags")
	sniperH := endpoint(t, p, "sniper")

	first := asReply[gossiprpc.UrchinTagsReply](t,
		tagsH(context.Background(), gossiprpc.Request{Account: uuidB}))
	assert.Equal(t, "player not found", first.Error)

	second := asReply[gossiprpc.UrchinTagsReply](t,
		tagsH(context.Background(), gossiprpc.Request{Account: uuidB}))
	assert.Equal(t, "player not found", second.Error)

	sniped := asReply[gossiprpc.UrchinSniperReply](t,
		sniperH(context.Background(), gossiprpc.Request{Account: uuidB}))
	assert.Equal(t, "player not found", sniped.Error)

	assert.Equal(t, 1, rec.count(), "absent players must be answered from the negative cache")
}

// A wave larger than Coral's 100-uuid ceiling drains in capped sequential
// POSTs, and every caller still gets its own answer.
func TestBatchCapsAt100PerRequest(t *testing.T) {
	const total = 150
	rec := &batchRecorder{}
	players := make(map[string][]batchTag, total)
	for i := range total {
		players[canonicalTestUUID(i)] = nil
	}
	rec.players = players

	p := newBatchProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !rec.handle(w, r) {
			t.Errorf("unexpected upstream call %s %s", r.Method, r.URL.Path)
		}
	}))
	h := endpoint(t, p, "tags")

	errs := make([]string, total)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range total {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = asReply[gossiprpc.UrchinTagsReply](t,
				h(context.Background(), gossiprpc.Request{Account: canonicalTestUUID(i)})).Error
		}(i)
	}
	close(start)
	wg.Wait()

	batches := rec.all()
	require.GreaterOrEqual(t, len(batches), 2, "%d players cannot fit one request", total)
	for i, b := range batches {
		assert.LessOrEqual(t, len(b), batchLimit, "batch %d exceeds Coral's ceiling", i)
	}
	flat := flattenBatches(batches)
	slices.Sort(flat)
	want := make([]string, 0, total)
	for i := range total {
		want = append(want, canonicalTestUUID(i))
	}
	assert.Equal(t, want, flat, "every queried player must be covered by the drained batches")
	for i, e := range errs {
		assert.Empty(t, e, "caller %d", i)
	}
}

// An infrastructure failure fails the whole wave WITHOUT caching anything:
// callers get the friendly fallback, and a retry after recovery performs a
// fresh POST rather than serving a pinned failure.
func TestBatchInfraFailureIsNotCached(t *testing.T) {
	rec := &batchRecorder{players: map[string][]batchTag{uuidA: {}}}
	var mu sync.Mutex
	healthy := false
	p := newBatchProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		ok := healthy
		mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"boom"}`))
			return
		}
		if !rec.handle(w, r) {
			t.Errorf("unexpected upstream call %s %s", r.Method, r.URL.Path)
		}
	}))
	h := endpoint(t, p, "tags")

	failed := asReply[gossiprpc.UrchinTagsReply](t, h(context.Background(), gossiprpc.Request{Account: uuidA}))
	assert.Equal(t, "tags lookup failed", failed.Error)

	mu.Lock()
	healthy = true
	mu.Unlock()
	retried := asReply[gossiprpc.UrchinTagsReply](t, h(context.Background(), gossiprpc.Request{Account: uuidA}))
	require.Empty(t, retried.Error)

	assert.Equal(t, 1, rec.count(), "the failed wave must POST again once the upstream recovers")
}

// Username lookups never touch the batcher: they still resolve through the
// individual GET, which is the only Coral surface that maps a username.
func TestUsernameLookupsSkipBatcher(t *testing.T) {
	rec := &batchRecorder{}
	tagsHits := 0
	p := newBatchProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			rec.handle(w, r)
			t.Error("username lookup must not reach the batch endpoint")
			return
		}
		require.Equal(t, "/v3/player/tags", r.URL.Path)
		tagsHits++
		_, _ = w.Write([]byte(`{"uuid":"deadbeef","displayname":"Techno","tags":[]}`))
	}))

	reply := asReply[gossiprpc.UrchinTagsReply](t,
		endpoint(t, p, "tags")(context.Background(), gossiprpc.Request{Account: "Techno"}))
	require.Empty(t, reply.Error)
	assert.Equal(t, 1, tagsHits)
	assert.Zero(t, rec.count())
}

// stubs builds the plain upstream handler for a recorder-backed provider:
// batch POSTs answered from its player map, anything else a test failure.
func (r *batchRecorder) stubs(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if r.handle(w, req) {
			return
		}
		t.Errorf("unexpected upstream call %s %s", req.Method, req.URL.Path)
	})
}

// canonicalTestUUID derives a stable valid uuid from an index.
func canonicalTestUUID(i int) string { return fmt.Sprintf("%032x", i) }

// These tests stage plain-http loopback upstreams the gate rightly refuses;
// production binaries never set this (see core.SetSSRFCheckForTests).
func init() { core.SetSSRFCheckForTests(false) }
