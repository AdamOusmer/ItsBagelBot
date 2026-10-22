// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	"ItsBagelBot/internal/domain/rpc/manage"
	"ItsBagelBot/pkg/ratelimit"

	"github.com/stretchr/testify/require"
)

const tieredPageBody = `{"data":[{"user_id":"7","user_login":"seven"},{"user_id":"8","user_login":"eight"}],"pagination":{}}`

func tierClient(rt func(*http.Request) (*http.Response, error)) *twitch.Client {
	owner := twitch.NewBroadcasterTokens(func(string) *twitch.Source { return twitch.NewStaticTokenSource("owner") })
	api := twitch.NewClient("client", twitch.NewStaticTokenSource("app"), twitch.NewStaticTokenSource("bot"), owner)
	api.SetTransport(chatterRevocationTransport(rt))
	return api
}

func TestChattersWithTiersTagsSubscribersOnBroadcasterQuota(t *testing.T) {
	now := time.Now()
	limiter := &chatterFakeLimiter{allow: true}
	var lookedUp []string
	var auth string
	api := tierClient(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == twitch.SubscriptionsPath {
			lookedUp = req.URL.Query()["user_id"]
			auth = req.Header.Get("Authorization")
			return chatterJSON(200, `{"data":[{"user_id":"7","tier":"2000"}]}`)
		}
		return chatterJSON(200, tieredPageBody)
	})
	c := newChatters(api, now, ChattersOptions{Limiter: limiter, Admit: chatterAdmitAll})
	req := chatterReq(now)
	req.WithTiers = true

	reply := c.handleGet(t.Context(), req)

	require.Empty(t, reply.ErrorCode)
	require.Equal(t, []manage.Chatter{{ID: "7", Login: "seven", Tier: "2000"}, {ID: "8", Login: "eight"}}, reply.Chatters)
	require.Equal(t, []string{"7", "8"}, lookedUp)
	require.Equal(t, "Bearer owner", auth)
	require.Len(t, limiter.requests, 1, "only the chatter page pays the attendance tenant budget")
	require.Len(t, limiter.pairs, 2)
	require.Equal(t, ratelimit.HelixUserRequest("123").Key, limiter.pairs[1][1].Key)
}

func TestChattersWithoutTiersNeverCallsSubscriptions(t *testing.T) {
	now := time.Now()
	subscriptionCalls := 0
	api := tierClient(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == twitch.SubscriptionsPath {
			subscriptionCalls++
		}
		return chatterJSON(200, tieredPageBody)
	})
	c := newChatters(api, now, ChattersOptions{Limiter: &chatterFakeLimiter{allow: true}, Admit: chatterAdmitAll})

	reply := c.handleGet(t.Context(), chatterReq(now))

	require.Empty(t, reply.ErrorCode)
	require.Len(t, reply.Chatters, 2)
	require.Zero(t, subscriptionCalls)
}

func TestChattersTierLookup429KeepsPageAndBotCooldown(t *testing.T) {
	now := time.Now()
	var observed []string
	api := tierClient(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == twitch.SubscriptionsPath {
			res, _ := chatterJSON(429, `{}`)
			res.Header.Set("Ratelimit-Reset", strconv.FormatInt(now.Add(time.Minute).Unix(), 10))
			return res, nil
		}
		return chatterJSON(200, tieredPageBody)
	})
	opts := ChattersOptions{
		Limiter: &chatterFakeLimiter{allow: true},
		Admit:   chatterAdmitAll,
		ObserveProviderReset: func(_ context.Context, id string, _ time.Time) error {
			observed = append(observed, id)
			return nil
		},
	}
	c := newChatters(api, now, opts)
	req := chatterReq(now)
	req.WithTiers = true

	reply := c.handleGet(t.Context(), req)

	require.Empty(t, reply.ErrorCode)
	require.Len(t, reply.Chatters, 2)
	require.False(t, slices.ContainsFunc(reply.Chatters, func(ch manage.Chatter) bool { return ch.Tier != "" }))
	require.Empty(t, observed, "a broadcaster-token 429 never cools down the bot identity")
}
