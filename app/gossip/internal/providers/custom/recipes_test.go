// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package custom

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"ItsBagelBot/app/gossip/internal/core"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
