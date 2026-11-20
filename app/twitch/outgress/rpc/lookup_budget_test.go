// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"
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
)

type lookupBudgetRoute struct {
	name     string
	buckets  lookupBucketKeys
	provider string
	path     string
	token    string
	handle   func(context.Context, *Manage) (string, int64)
}

type lookupBudgetCase struct {
	name       string
	statuses   []int
	blockAt    int
	limiterErr error
	firstBody  string
	wantError  string
}

type lookupBucketKeys [2]string

var (
	appLookupKeys = lookupBucketKeys{"ratelimit:helix:app:standard", "ratelimit:helix:app"}
	botLookupKeys = lookupBucketKeys{"ratelimit:helix:user:bot:standard", "ratelimit:helix:user:bot"}
)

func ageLookupRoutes() []lookupBudgetRoute {
	return []lookupBudgetRoute{
		{
			name: "followage", buckets: botLookupKeys, provider: "helix:bot:456",
			path: "/helix/channels/followers?broadcaster_id=123&user_id=456", token: "Bearer bot",
			handle: func(ctx context.Context, m *Manage) (string, int64) {
				reply := m.handleFollowage(ctx, outgressrpc.FollowageRequest{BroadcasterID: "123", TargetID: "456"})
				return reply.Error, reply.RetryAtUnixMilli
			},
		},
		{
			name: "accountage", buckets: appLookupKeys, provider: "helix:app",
			path: "/helix/users?id=456", token: "Bearer app",
			handle: func(ctx context.Context, m *Manage) (string, int64) {
				reply := m.handleAccountAge(ctx, outgressrpc.AccountAgeRequest{TargetID: "456"})
				return reply.Error, reply.RetryAtUnixMilli
			},
		},
	}
}

func TestAgeLookupsChargeEveryTwitchAttempt(t *testing.T) {
	const invalidToken = `{"message":"invalid token"}`
	cases := []lookupBudgetCase{
		{name: "success", statuses: []int{http.StatusOK}},
		{name: "401 retry", statuses: []int{http.StatusUnauthorized, http.StatusOK}, firstBody: invalidToken},
		{name: "local denial", blockAt: 1, wantError: "lookup failed"},
		{name: "limiter error", blockAt: 1, limiterErr: errors.New("limiter unavailable"), wantError: "lookup failed"},
		{name: "retry denied", statuses: []int{http.StatusUnauthorized}, blockAt: 2, firstBody: invalidToken, wantError: "lookup failed"},
		{name: "provider 429", statuses: []int{http.StatusTooManyRequests}, wantError: "lookup failed"},
		{name: "provider 429 on retry", statuses: []int{http.StatusUnauthorized, http.StatusTooManyRequests}, firstBody: invalidToken, wantError: "lookup failed"},
		{name: "missing scope", statuses: []int{http.StatusUnauthorized}, firstBody: `{"message":"Missing scope: moderator:read:followers"}`, wantError: "lookup failed"},
	}
	for _, route := range ageLookupRoutes() {
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

func (l *lookupBudgetLimiter) AllowOrdered(ctx context.Context, standard, shared ratelimit.Request) (uint8, error) {
	_, err := l.chatterFakeLimiter.AllowOrdered(ctx, standard, shared)
	if err != nil {
		return 0, err
	}
	if len(l.pairs) == l.denyAt {
		return 1, nil
	}
	return 0, nil
}

func lookupCharges(limiter *chatterFakeLimiter) []lookupBucketKeys {
	keys := make([]lookupBucketKeys, 0, len(limiter.pairs))
	for _, pair := range limiter.pairs {
		keys = append(keys, lookupBucketKeys{pair[0].Key, pair[1].Key})
	}
	return keys
}

func checkLookupBudget(t *testing.T, route lookupBudgetRoute, tc lookupBudgetCase) {
	t.Helper()
	started := time.Now()
	reset := started.Add(time.Minute).Truncate(time.Second)
	limiter := &lookupBudgetLimiter{denyAt: tc.blockAt, chatterFakeLimiter: chatterFakeLimiter{err: tc.limiterErr}}
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
		require.Equal(t, calls, len(limiter.pairs), "charge before HTTP")
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{
			"Ratelimit-Reset": {strconv.FormatInt(reset.Unix(), 10)},
		}}, nil
	}))
	m := newTestManage(api)
	m.limiter = limiter
	message, retryAt := route.handle(t.Context(), m)
	require.Equal(t, tc.wantError, message)
	require.Equal(t, len(tc.statuses), calls)
	require.Equal(t, slices.Repeat([]lookupBucketKeys{route.buckets}, max(calls, tc.blockAt)), lookupCharges(&limiter.chatterFakeLimiter))
	tc.checkRetryAt(t, retryAt, started, reset)
}

func (tc lookupBudgetCase) checkRetryAt(t *testing.T, retryAt int64, started, reset time.Time) {
	t.Helper()
	switch {
	case tc.blockAt != 0:
		require.WithinRange(t, time.UnixMilli(retryAt), started.Add(time.Second).Truncate(time.Millisecond), time.Now().Add(time.Second))
	case slices.Contains(tc.statuses, http.StatusTooManyRequests):
		require.Equal(t, reset.UnixMilli(), retryAt)
	default:
		require.Zero(t, retryAt)
	}
}

func TestAgeLookupProviderCooldown(t *testing.T) {
	for _, route := range ageLookupRoutes() {
		t.Run(route.name, func(t *testing.T) {
			fixture := newChatterCooldownFixture(time.Now().Truncate(time.Second))
			reset := fixture.now.Add(time.Minute)
			for _, step := range []struct {
				name       string
				advance    time.Duration
				retryAt    int64
				httpCalls  int
				newCharges int
			}{
				{name: "provider 429", retryAt: reset.UnixMilli(), httpCalls: 1, newCharges: 1},
				{name: "cooldown blocks another handler", retryAt: reset.UnixMilli(), httpCalls: 1},
				{name: "lookups resume at reset", advance: time.Minute, httpCalls: 2, newCharges: 1},
			} {
				t.Run(step.name, func(t *testing.T) {
					fixture.now = fixture.now.Add(step.advance)
					limiter := &chatterFakeLimiter{}
					m := newTestManage(fixture.api)
					m.limiter = limiter
					m.cooldown = ProviderCooldown{RetryAt: fixture.retryAt, ObserveReset: fixture.observe}

					_, retryAt := route.handle(t.Context(), m)
					require.Equal(t, step.retryAt, retryAt)
					require.Equal(t, step.httpCalls, fixture.calls)
					require.Len(t, limiter.pairs, step.newCharges)
				})
			}
			require.Equal(t, map[string]time.Time{route.provider: reset}, fixture.resets)
		})
	}
}

func TestFollowageByLoginChargesAppThenBot(t *testing.T) {
	attempts := []struct {
		path  string
		token string
		body  string
	}{
		{"/helix/users?login=viewer", "Bearer app", `{"data":[{"id":"456"}]}`},
		{"/helix/channels/followers?broadcaster_id=123&user_id=456", "Bearer bot", `{"data":[{"followed_at":"2026-01-01T00:00:00Z"}]}`},
	}
	limiter := &chatterFakeLimiter{allow: true}
	api := twitch.NewClient("client", twitch.NewStaticTokenSource("app"), twitch.NewStaticTokenSource("bot"), nil)
	calls := 0
	api.SetTransport(chatterRevocationTransport(func(req *http.Request) (*http.Response, error) {
		require.Less(t, calls, len(attempts))
		attempt := attempts[calls]
		calls++

		require.Equal(t, attempt.path, req.URL.RequestURI())
		require.Equal(t, attempt.token, req.Header.Get("Authorization"))
		require.Len(t, limiter.pairs, calls)
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(attempt.body))}, nil
	}))
	m := newTestManage(api)
	m.limiter = limiter

	reply := m.handleFollowage(t.Context(), outgressrpc.FollowageRequest{BroadcasterID: "123", TargetLogin: "viewer"})
	require.Equal(t, outgressrpc.FollowageReply{
		TargetID: "456", UserFound: true, Following: true, FollowedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}, reply)
	require.Equal(t, len(attempts), calls)
	require.Equal(t, []lookupBucketKeys{appLookupKeys, botLookupKeys}, lookupCharges(limiter))
}
