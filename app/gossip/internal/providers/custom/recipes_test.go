// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package custom

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ItsBagelBot/app/gossip/internal/core"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/internal/valkeytest"
	"ItsBagelBot/pkg/ratelimit"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func budgetedHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	h.p.limiter = ratelimit.New(valkeytest.Spawn(t))
	h.p.admit = h.p.spendBudget
	h.route(t, "/wx", staged{status: http.StatusOK, ct: "application/json", body: `{"v":1}`})
	return h
}

func TestRecipesGetTheirOwnLargerBudget(t *testing.T) {
	h := budgetedHarness(t)
	for range 20 {
		require.Equal(t, gossiprpc.FetchOK, call(t, h, inlineFetch(h.srv.URL+"/wx")).Status)
	}
}

func TestRecipePostsGetTheRecipeBudget(t *testing.T) {
	h := budgetedHarness(t)
	for range 20 {
		require.Equal(t, gossiprpc.FetchOK, callPost(t, h, postRequest(h.srv.URL+"/wx", "x")).Status)
	}
}

func TestRecipeBudgetStillEnds(t *testing.T) {
	h := budgetedHarness(t)
	statuses := map[gossiprpc.FetchStatus]int{}
	for range 80 {
		statuses[call(t, h, inlineFetch(h.srv.URL+"/wx")).Status]++
	}
	assert.Positive(t, statuses[gossiprpc.FetchLimited])
}

func TestCommandFetchesKeepTheirOwnBudget(t *testing.T) {
	h := budgetedHarness(t)
	h.addDef("d", "/wx", gossiprpc.FetchDef{IsActive: true})
	req := gossiprpc.Request{ChannelID: "ch1", DefID: "d", Fresh: true}
	statuses := map[gossiprpc.FetchStatus]int{}
	for range 20 {
		statuses[call(t, h, req).Status]++
	}
	assert.Positive(t, statuses[gossiprpc.FetchLimited], "commands stay on the smaller budget")
}

func inlineFetch(url string) gossiprpc.Request {
	return gossiprpc.Request{ChannelID: "ch1", Def: &gossiprpc.FetchDef{Name: "get:ch1", URL: url, JSONPath: []string{"v"}, IsActive: true}}
}

func TestInlineFetchSpendsBudgetAndSkipsTheCache(t *testing.T) {
	h := newHarness(t)
	h.route(t, "/wx", staged{status: http.StatusOK, ct: "application/json", body: `{"v":1}`})

	for range 2 {
		reply := call(t, h, inlineFetch(h.srv.URL+"/wx"))
		require.Equal(t, gossiprpc.FetchOK, reply.Status)
		assert.Equal(t, []string{"1"}, reply.Values)
	}

	assert.Equal(t, int32(2), h.admit.Load(), "a recipe loop must not bypass the buckets")
	assert.Equal(t, int32(2), h.hits.Load())
	assert.Empty(t, h.store.Keys())
}

func TestInlineFetchBucketDenialAnswersLimited(t *testing.T) {
	h := newHarness(t)
	h.route(t, "/wx", staged{status: http.StatusOK, ct: "application/json", body: `{"v":1}`})
	h.p.admit = func(context.Context, *flight, bool) error {
		return &core.UpstreamError{Status: http.StatusTooManyRequests, LocalDeny: true}
	}

	reply := call(t, h, inlineFetch(h.srv.URL+"/wx"))

	assert.Equal(t, gossiprpc.FetchLimited, reply.Status)
	assert.Zero(t, h.hits.Load())
}

func TestSavedValuesKeepUpToTheRecipeLimit(t *testing.T) {
	long := strings.Repeat("é", 800)
	h := newHarness(t)
	h.route(t, "/wx", staged{status: http.StatusOK, ct: "text/plain; charset=utf-8", body: long})

	inline := inlineFetch(h.srv.URL + "/wx")
	inline.Def.JSONPath = nil
	assert.Equal(t, []string{strings.Repeat("é", 500)}, call(t, h, inline).Values)

	h.addDef("d", "/wx", gossiprpc.FetchDef{IsActive: true})
	assert.Equal(t, []string{strings.Repeat("é", 256)}, call(t, h, gossiprpc.Request{ChannelID: "ch1", DefID: "d"}).Values, "command fetches keep the chat limit")

	rehearsal := inlineFetch(h.srv.URL + "/wx")
	rehearsal.Def.JSONPath, rehearsal.DryRun = nil, true
	assert.Equal(t, []string{strings.Repeat("é", 256)}, call(t, h, rehearsal).Values, "a rehearsal shows what a command would get")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(long))
	}))
	t.Cleanup(srv.Close)
	asked := postRequest(srv.URL, "x")
	asked.CaptureBody = true
	assert.Equal(t, []string{strings.Repeat("é", 500)}, callPost(t, h, asked).Values)
}

func TestPostCapturesTheResponseOnlyWhenAsked(t *testing.T) {
	h := newHarness(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("  created \xff\n"))
	}))
	t.Cleanup(srv.Close)

	plain := callPost(t, h, postRequest(srv.URL, "x"))
	assert.Equal(t, gossiprpc.FetchOK, plain.Status)
	assert.Empty(t, plain.Values)

	asked := postRequest(srv.URL, "x")
	asked.CaptureBody = true
	captured := callPost(t, h, asked)
	assert.Equal(t, gossiprpc.FetchOK, captured.Status)
	assert.Equal(t, []string{"created"}, captured.Values)
}
