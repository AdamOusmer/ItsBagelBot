// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	outgressrpc "ItsBagelBot/internal/domain/rpc/outgress"
	"ItsBagelBot/pkg/ratelimit"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type lookupBudgetRoute struct {
	name   string
	bucket ratelimit.Request
	path   string
	token  string
	handle func(context.Context, *Manage) (string, int64)
}

type lookupBudgetCase struct {
	name      string
	statuses  []int
	denyAt    int
	firstBody string
	wantError string
}

func TestAgeLookupsChargeEveryTwitchAttempt(t *testing.T) {
	routes := []lookupBudgetRoute{
		{
			name: "followage", bucket: ratelimit.HelixBotRequest(),
			path: "/helix/channels/followers?broadcaster_id=123&user_id=456", token: "Bearer bot",
			handle: func(ctx context.Context, m *Manage) (string, int64) {
				reply := m.handleFollowage(ctx, outgressrpc.FollowageRequest{BroadcasterID: "123", TargetID: "456"})
				return reply.Error, reply.RetryAtUnixMilli
			},
		},
		{
			name: "accountage", bucket: ratelimit.HelixAppRequest(),
			path: "/helix/users?id=456", token: "Bearer app",
			handle: func(ctx context.Context, m *Manage) (string, int64) {
				reply := m.handleAccountAge(ctx, outgressrpc.AccountAgeRequest{TargetID: "456"})
				return reply.Error, reply.RetryAtUnixMilli
			},
		},
	}
	const invalidToken = `{"message":"invalid token"}`
	cases := []lookupBudgetCase{
		{name: "success", statuses: []int{http.StatusOK}},
		{name: "401 retry", statuses: []int{http.StatusUnauthorized, http.StatusOK}, firstBody: invalidToken},
		{name: "local denial", denyAt: 1, wantError: "lookup failed"},
		{name: "retry denied", statuses: []int{http.StatusUnauthorized}, denyAt: 2, firstBody: invalidToken, wantError: "lookup failed"},
		{name: "provider 429", statuses: []int{http.StatusTooManyRequests}, wantError: "lookup failed"},
		{name: "provider 429 on retry", statuses: []int{http.StatusUnauthorized, http.StatusTooManyRequests}, firstBody: invalidToken, wantError: "lookup failed"},
		{name: "missing scope", statuses: []int{http.StatusUnauthorized}, firstBody: `{"message":"Missing scope: moderator:read:followers"}`, wantError: "lookup failed"},
	}
	for _, route := range routes {
		t.Run(route.name, func(t *testing.T) {
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) { checkLookupBudget(t, route, tc) })
			}
		})
	}
}

type lookupBudgetLimiter struct {
	chatterFakeLimiter
	denyAt int
}

func (l *lookupBudgetLimiter) Allow(_ context.Context, bucket ratelimit.Request) (bool, error) {
	l.requests = append(l.requests, bucket)
	return len(l.requests) != l.denyAt, nil
}

func checkLookupBudget(t *testing.T, route lookupBudgetRoute, tc lookupBudgetCase) {
	t.Helper()
	started := time.Now()
	reset := started.Add(time.Minute).Truncate(time.Second)
	limiter := &lookupBudgetLimiter{denyAt: tc.denyAt}
	api := twitch.NewClient("client", twitch.NewStaticTokenSource("app"), twitch.NewStaticTokenSource("bot"), nil)
	calls := 0
	api.SetTransport(chatterRevocationTransport(func(req *http.Request) (*http.Response, error) {
		require.Less(t, calls, len(tc.statuses))
		require.Equal(t, route.path, req.URL.RequestURI())
		require.Equal(t, route.token, req.Header.Get("Authorization"))

		status := tc.statuses[calls]
		body := `{"data":[]}`
		if calls == 0 && tc.firstBody != "" {
			body = tc.firstBody
		}
		calls++
		require.Equal(t, calls, len(limiter.requests), "charge before HTTP")
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{
			"Ratelimit-Reset": {strconv.FormatInt(reset.Unix(), 10)},
		}}, nil
	}))
	m := &Manage{twitch: api, limiter: limiter, log: zap.NewNop()}
	message, retryAt := route.handle(t.Context(), m)
	require.Equal(t, tc.wantError, message)
	require.Equal(t, len(tc.statuses), calls)
	require.Equal(t, slices.Repeat([]ratelimit.Request{route.bucket}, max(calls, tc.denyAt)), limiter.requests)
	tc.checkRetryAt(t, retryAt, started, reset)
}

func (tc lookupBudgetCase) checkRetryAt(t *testing.T, retryAt int64, started, reset time.Time) {
	t.Helper()
	switch {
	case tc.denyAt != 0:
		require.WithinRange(t, time.UnixMilli(retryAt), started.Add(time.Second).Truncate(time.Millisecond), time.Now().Add(time.Second))
	case slices.Contains(tc.statuses, http.StatusTooManyRequests):
		require.Equal(t, reset.UnixMilli(), retryAt)
	default:
		require.Zero(t, retryAt)
	}
}
