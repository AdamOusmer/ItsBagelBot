// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package custom

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"ItsBagelBot/app/gossip/internal/core"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type seenPost struct {
	method string
	ct     string
	body   string
}

type postUpstream struct {
	srv    *httptest.Server
	mu     sync.Mutex
	seen   []seenPost
	status int
}

func newPostUpstream(t *testing.T, status int) *postUpstream {
	t.Helper()
	u := &postUpstream{status: status}
	u.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		u.mu.Lock()
		u.seen = append(u.seen, seenPost{r.Method, r.Header.Get("Content-Type"), string(b)})
		u.mu.Unlock()
		w.WriteHeader(u.status)
	}))
	t.Cleanup(u.srv.Close)
	return u
}

func callPost(t *testing.T, h *harness, req gossiprpc.Request) gossiprpc.CustomFetchReply {
	t.Helper()
	res := h.p.post(context.Background(), req)
	reply, ok := res.(gossiprpc.CustomFetchReply)
	require.True(t, ok, "handler returned %T", res)
	return reply
}

func postRequest(url, body string) gossiprpc.Request {
	return gossiprpc.Request{Account: "ch1", ChannelID: "ch1", Def: &gossiprpc.FetchDef{URL: url}, Body: body}
}

func TestPostSendsBodyWithMatchingContentType(t *testing.T) {
	for _, tc := range []struct{ name, body, wantCT string }{
		{"json body", `{"a":1}`, "application/json"},
		{"plain text body", "hello chat", "text/plain; charset=utf-8"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			up := newPostUpstream(t, http.StatusNoContent)

			reply := callPost(t, h, postRequest(up.srv.URL+"/hook", tc.body))

			assert.Equal(t, gossiprpc.FetchOK, reply.Status)
			require.Len(t, up.seen, 1)
			assert.Equal(t, seenPost{http.MethodPost, tc.wantCT, tc.body}, up.seen[0])
			assert.Empty(t, h.store.Keys(), "post results are never cached")
		})
	}
}

func TestPostMapsUpstreamStatus(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		want   gossiprpc.FetchStatus
	}{
		{"created", http.StatusCreated, gossiprpc.FetchOK},
		{"server error", http.StatusInternalServerError, gossiprpc.FetchUpstreamError},
		{"redirect is not followed", http.StatusFound, gossiprpc.FetchUpstreamError},
		{"upstream throttle", http.StatusTooManyRequests, gossiprpc.FetchLimited},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			up := newPostUpstream(t, tc.status)

			reply := callPost(t, h, postRequest(up.srv.URL, "x"))

			assert.Equal(t, tc.want, reply.Status)
			assert.Len(t, up.seen, 1, "no retries")
		})
	}
}

func TestPostRejectsOversizeAndMalformedRequests(t *testing.T) {
	h := newHarness(t)
	up := newPostUpstream(t, http.StatusOK)

	for name, req := range map[string]gossiprpc.Request{
		"oversize body":  postRequest(up.srv.URL, strings.Repeat("a", 4097)),
		"no def":         {ChannelID: "ch1", Body: "x"},
		"empty url":      postRequest("", "x"),
		"no channel":     {Def: &gossiprpc.FetchDef{URL: up.srv.URL}, Body: "x"},
		"unparseable":    postRequest("::not a url", "x"),
		"scheme missing": postRequest("//example.com/x", "x"),
	} {
		assert.Equal(t, gossiprpc.FetchBadDef, callPost(t, h, req).Status, name)
	}
	assert.Empty(t, up.seen)
}

func TestPostAcceptsBodyAtTheLimit(t *testing.T) {
	h := newHarness(t)
	up := newPostUpstream(t, http.StatusOK)

	reply := callPost(t, h, postRequest(up.srv.URL, strings.Repeat("a", 4096)))

	assert.Equal(t, gossiprpc.FetchOK, reply.Status)
}

func TestPostBucketDenialAnswersLimited(t *testing.T) {
	h := newHarness(t)
	up := newPostUpstream(t, http.StatusOK)
	h.p.admit = func(context.Context, *flight, bool) error {
		return &core.UpstreamError{Status: 429, LocalDeny: true}
	}

	reply := callPost(t, h, postRequest(up.srv.URL, "x"))

	assert.Equal(t, gossiprpc.FetchLimited, reply.Status)
	assert.Empty(t, up.seen)
}

func TestPostHostileTargetsAreDenied(t *testing.T) {
	core.SetSSRFCheckForTests(true)
	t.Cleanup(func() { core.SetSSRFCheckForTests(false) })
	h := newHarness(t)

	for _, target := range []string{"https://127.0.0.1/x", "https://169.254.169.254/x", "https://10.9.9.9/x", "https://[::1]/x", "https://localhost/x"} {
		assert.Equal(t, gossiprpc.FetchDenied, callPost(t, h, postRequest(target, "x")).Status, target)
	}
	assert.Zero(t, h.socks.Conns(), "denied targets never open a tunnel")
}
