// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package urchin_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"slices"
	"sync"
	"testing"

	"ItsBagelBot/app/gossip/internal/provider"
	"ItsBagelBot/app/gossip/internal/providertest"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	uuidA = "069a79f444e94726a5befca90e38aaf5"
	uuidB = "b71e0c9d1f2d4c8eab12cd34ef56ab78"

	dashedUpperUUIDA   = "069A79F4-44E9-4726-A5BE-FCA90E38AAF5"
	batchCeiling       = 100
	batchPlayersPath   = "/v3/players"
	cubelifyPath       = "/v3/cubelify"
	playerTagsPath     = "/v3/player/tags"
	unexpectedCallText = "unexpected upstream call %s %s"
)

type batchTag struct {
	TagType string `json:"tag_type"`
	Reason  string `json:"reason"`
	AddedOn int64  `json:"added_on"`
}

type batchRecorder struct {
	mu      sync.Mutex
	batches [][]string
	players map[string][]batchTag
}

func (r *batchRecorder) handle(w http.ResponseWriter, req *http.Request) bool {
	if req.Method != http.MethodPost || req.URL.Path != batchPlayersPath {
		return false
	}
	body, _ := io.ReadAll(req.Body)
	_ = req.Body.Close()
	var posted struct {
		UUIDs []string `json:"uuids"`
	}
	_ = codec.Unmarshal(body, &posted)

	r.mu.Lock()
	r.batches = append(r.batches, posted.UUIDs)
	players := make(map[string][]batchTag, len(r.players))
	for k, v := range r.players {
		players[k] = v
	}
	r.mu.Unlock()

	payload, _ := codec.Marshal(map[string]any{"players": players})
	_, _ = w.Write(payload)
	return true
}

func (r *batchRecorder) all() [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.batches)
}

func (r *batchRecorder) count() int { return len(r.all()) }

func (r *batchRecorder) stubs(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if !r.handle(w, req) {
			t.Errorf(unexpectedCallText, req.Method, req.URL.Path)
		}
	})
}

func canonicalTestUUID(i int) string { return fmt.Sprintf("%032x", i) }

func queryTagsTogether(t *testing.T, p provider.Provider, accounts []string) []gossiprpc.UrchinTagsReply {
	t.Helper()
	var wg sync.WaitGroup
	replies := make([]gossiprpc.UrchinTagsReply, len(accounts))
	for i, account := range accounts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			replies[i] = providertest.Call[gossiprpc.UrchinTagsReply](t, p, "tags", gossiprpc.Request{Account: account})
		}()
	}
	wg.Wait()
	return replies
}

func TestBatchAggregatesDistinctPlayers(t *testing.T) {
	rec := &batchRecorder{players: map[string][]batchTag{
		uuidA: {{TagType: "blatant_cheater", Reason: "Fly / Killaura", AddedOn: 1700000000000}},
		uuidB: {},
	}}
	p := newProvider(t, rec.stubs(t))

	replies := queryTagsTogether(t, p, []string{dashedUpperUUIDA, uuidB})

	require.Equal(t, 1, rec.count(), "distinct players in one window must share one batch POST")
	assert.Equal(t, []string{uuidA, uuidB}, rec.all()[0], "batch body carries canonical undashed uuids")
	assert.Equal(t, gossiprpc.UrchinTagsReply{
		Player: dashedUpperUUIDA,
		Tags:   []gossiprpc.UrchinTag{{Type: "blatant_cheater", Reason: "Fly / Killaura", AddedOn: 1700000000}},
	}, replies[0])
	assert.Empty(t, replies[1].Error)
	assert.Empty(t, replies[1].Tags)
}

func TestBatchDedupsIdenticalPlayer(t *testing.T) {
	rec := &batchRecorder{players: map[string][]batchTag{uuidA: {{TagType: "sniper"}}}}
	p := newProvider(t, rec.stubs(t))
	spellings := []string{uuidA, dashedUpperUUIDA, "069A79F444E94726A5BEFCA90E38AAF5"}
	accounts := make([]string, 8)
	for i := range accounts {
		accounts[i] = spellings[i%len(spellings)]
	}

	replies := queryTagsTogether(t, p, accounts)

	require.Equal(t, 1, rec.count())
	assert.Equal(t, []string{uuidA}, rec.all()[0], "duplicate queries must collapse to one batch line")
	for i, r := range replies {
		require.Empty(t, r.Error, "caller %d", i)
		require.Len(t, r.Tags, 1)
	}
}

func TestBatchHydratesSharedPlayertagsCache(t *testing.T) {
	rec := &batchRecorder{players: map[string][]batchTag{
		uuidA: {{TagType: "cheater", Reason: "bhop"}},
	}}
	cubelifyHits := 0
	p := newProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rec.handle(w, r) {
			return
		}
		if r.URL.Path != cubelifyPath {
			t.Errorf(unexpectedCallText, r.Method, r.URL.Path)
			return
		}
		cubelifyHits++
		assert.Equal(t, uuidA, r.URL.Query().Get("uuid"), "cubelify must receive the canonical uuid")
		_, _ = w.Write([]byte(`{"score":{"value":7.5,"mode":"warn"},"tags":[]}`))
	}))

	tags := providertest.Call[gossiprpc.UrchinTagsReply](t, p, "tags", gossiprpc.Request{Account: uuidA})
	require.Empty(t, tags.Error)

	sniped := providertest.Call[gossiprpc.UrchinSniperReply](t, p, "sniper", gossiprpc.Request{Account: dashedUpperUUIDA})
	require.Empty(t, sniped.Error)
	assert.Equal(t, 7.5, sniped.Score)
	assert.Equal(t, 1, cubelifyHits, "the uuid hop must be served by the hydrated batch entry")
	assert.Equal(t, 1, rec.count(), "the sniper leg must not re-batch a hydrated player")
}

func TestBatchNegativeCachesMissingPlayers(t *testing.T) {
	rec := &batchRecorder{players: map[string][]batchTag{uuidA: {}}}
	p := newProvider(t, rec.stubs(t))

	for _, endpoint := range []string{"tags", "tags", "sniper"} {
		res := providertest.Endpoint(t, p, endpoint)(context.Background(), gossiprpc.Request{Account: uuidB})
		assert.Equal(t, "player not found", providertest.ErrorOf(t, res), endpoint)
	}
	assert.Equal(t, 1, rec.count(), "absent players must be answered from the negative cache")
}

func TestBatchCapsAt100PerRequest(t *testing.T) {
	const total = 150
	rec := &batchRecorder{players: make(map[string][]batchTag, total)}
	for i := range total {
		rec.players[canonicalTestUUID(i)] = nil
	}
	p := newProvider(t, rec.stubs(t))

	errs := make([]string, total)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range total {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs[i] = providertest.Call[gossiprpc.UrchinTagsReply](t, p, "tags", gossiprpc.Request{Account: canonicalTestUUID(i)}).Error
		}()
	}
	close(start)
	wg.Wait()

	batches := rec.all()
	require.GreaterOrEqual(t, len(batches), 2, "%d players cannot fit one request", total)
	for i, b := range batches {
		assert.LessOrEqual(t, len(b), batchCeiling, "batch %d exceeds Coral's ceiling", i)
	}
	flat := slices.Concat(batches...)
	slices.Sort(flat)
	want := make([]string, 0, total)
	for i := range total {
		want = append(want, canonicalTestUUID(i))
	}
	assert.Equal(t, want, flat, "every queried player must be covered by the drained batches")
	assert.Equal(t, make([]string, total), errs)
}

func TestBatchInfraFailureIsNotCached(t *testing.T) {
	rec := &batchRecorder{players: map[string][]batchTag{uuidA: {}}}
	var mu sync.Mutex
	healthy := false
	p := newProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		ok := healthy
		mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"boom"}`))
			return
		}
		if !rec.handle(w, r) {
			t.Errorf(unexpectedCallText, r.Method, r.URL.Path)
		}
	}))

	failed := providertest.Call[gossiprpc.UrchinTagsReply](t, p, "tags", gossiprpc.Request{Account: uuidA})
	assert.Equal(t, "tags lookup failed", failed.Error)

	mu.Lock()
	healthy = true
	mu.Unlock()
	retried := providertest.Call[gossiprpc.UrchinTagsReply](t, p, "tags", gossiprpc.Request{Account: uuidA})
	require.Empty(t, retried.Error)

	assert.Equal(t, 1, rec.count(), "the failed wave must POST again once the upstream recovers")
}

func TestOnlyCanonicalUUIDsAreBatched(t *testing.T) {
	for _, tc := range []struct {
		name    string
		account string
		batched bool
	}{
		{"a username goes straight to the tags endpoint", "Techno", false},
		{"a 31 digit hex string is a username", "069a79f444e94726a5befca90e38aaf", false},
		{"a 32 character string with a non-hex digit is a username", "zzza79f444e94726a5befca90e38aaf5", false},
		{"an undashed uuid is batched", uuidA, true},
		{"a dashed uuid is batched", "069a79f4-44e9-4726-a5be-fca90e38aaf5", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := &batchRecorder{players: map[string][]batchTag{uuidA: {}}}
			tagsHits := 0
			p := newProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if rec.handle(w, r) {
					return
				}
				require.Equal(t, playerTagsPath, r.URL.Path)
				tagsHits++
				_, _ = w.Write([]byte(`{"uuid":"deadbeef","displayname":"Techno","tags":[]}`))
			}))

			reply := providertest.Call[gossiprpc.UrchinTagsReply](t, p, "tags", gossiprpc.Request{Account: tc.account})

			require.Empty(t, reply.Error)
			assert.Equal(t, tc.batched, rec.count() == 1)
			assert.Equal(t, !tc.batched, tagsHits == 1)
		})
	}
}
