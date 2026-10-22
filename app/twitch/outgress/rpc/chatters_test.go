// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	"ItsBagelBot/internal/domain/rpc/manage"
	"ItsBagelBot/pkg/ratelimit"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type chatterFakeAPI struct {
	calls                  int
	liveCalls              int
	live                   bool
	page                   twitch.ChattersPage
	err                    error
	bid, moderator, cursor string
	onPage                 func(context.Context)
}

func (a *chatterFakeAPI) GetChattersPage(ctx context.Context, request twitch.ChattersPageRequest) (twitch.ChattersPage, error) {
	a.calls++
	a.bid = request.BroadcasterID
	a.moderator = request.ModeratorID
	a.cursor = request.Cursor
	if a.onPage != nil {
		a.onPage(ctx)
	}
	return a.page, a.err
}
func (a *chatterFakeAPI) StreamSession(context.Context, string) (string, time.Time, bool, error) {
	a.liveCalls++
	if !a.live {
		return "", time.Time{}, false, nil
	}
	return "stream-1", time.Unix(100, 0), true, nil
}

type chatterFakeLimiter struct {
	requests []ratelimit.Request
	pairs    [][2]ratelimit.Request
	allow    bool
	denied   uint8
	err      error
}

func (l *chatterFakeLimiter) Allow(_ context.Context, r ratelimit.Request) (bool, error) {
	l.requests = append(l.requests, r)
	return l.allow, l.err
}
func (l *chatterFakeLimiter) AllowOrdered(_ context.Context, a, b ratelimit.Request) (uint8, error) {
	l.pairs = append(l.pairs, [2]ratelimit.Request{a, b})
	return l.denied, l.err
}

var chatterAdmitAll = func(context.Context, manage.ChattersRequest) (bool, error) { return true, nil }

func newChatters(api chatterAPI, now time.Time, opts ChattersOptions) *chatters {
	return &chatters{twitch: api, botID: "456", log: zap.NewNop(), now: func() time.Time { return now }, opts: opts}
}

func chatterHandler(api *chatterFakeAPI, now time.Time) *chatters {
	return newChatters(api, now, ChattersOptions{Limiter: &chatterFakeLimiter{allow: true}, Admit: chatterAdmitAll})
}
func chatterReq(now time.Time) manage.ChattersRequest {
	return manage.ChattersRequest{BroadcasterID: "123", RequestID: "request-1", WindowID: "window-1", SessionGeneration: "gen", LiveSession: "session", DeadlineUnixMilli: now.Add(time.Second).UnixMilli()}
}

func TestChattersRefuseBeforeSpendingAnything(t *testing.T) {
	tests := []struct {
		name       string
		expired    bool
		active     bool
		admitErr   error
		wantCode   string
		wantAdmits int
	}{
		{"expired queued work never reaches admission", true, true, nil, "expired", 0},
		{"a removed channel is inactive", false, false, nil, "inactive", 1},
		{"an unavailable authority fails closed", false, false, errors.New("down"), "unavailable", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Now()
			api := &chatterFakeAPI{}
			c := chatterHandler(api, now)
			admits := 0
			c.opts.Admit = func(context.Context, manage.ChattersRequest) (bool, error) { admits++; return tt.active, tt.admitErr }
			req := chatterReq(now)
			if tt.expired {
				req.DeadlineUnixMilli = now.Add(-time.Second).UnixMilli()
			}

			reply := c.handleGet(t.Context(), req)

			require.Equal(t, tt.wantCode, reply.ErrorCode)
			require.Equal(t, tt.wantAdmits, admits)
			require.Zero(t, api.calls+api.liveCalls, "refused work never calls Twitch")
		})
	}
}
func TestChattersReplyCorrelatesWindowTenantAndCursor(t *testing.T) {
	now := time.Now()
	api := &chatterFakeAPI{page: twitch.ChattersPage{Chatters: []twitch.Chatter{{ID: "42", Login: "alice"}}, NextCursor: "next"}}
	c := chatterHandler(api, now)
	req := chatterReq(now)
	req.Cursor = "previous"
	r := c.handleGet(t.Context(), req)
	require.Equal(t, "", r.Error)
	require.Equal(t, req.BroadcasterID, r.BroadcasterID)
	require.Equal(t, req.RequestID, r.RequestID)
	require.Equal(t, req.WindowID, r.WindowID)
	require.Equal(t, req.SessionGeneration, r.SessionGeneration)
	require.Equal(t, req.LiveSession, r.LiveSession)
	require.Equal(t, "next", r.NextCursor)
	require.False(t, r.Complete)
	require.Equal(t, "123", api.bid)
	require.Equal(t, "456", api.moderator)
	require.Equal(t, "previous", api.cursor)
}
func TestChattersPreservesFreshLiveCheckWhenPageFails(t *testing.T) {
	now := time.Now()
	retry := now.Add(time.Minute)
	api := &chatterFakeAPI{live: true, err: &twitch.AdmissionError{Code: "rate_limited", RetryAt: retry}}
	c := chatterHandler(api, now)
	req := chatterReq(now)
	req.CheckLive = true
	r := c.handleGet(t.Context(), req)
	require.True(t, r.Live)
	require.Equal(t, "stream-1", r.StreamID)
	require.Equal(t, time.Unix(100, 0).UnixMilli(), r.StreamStartedAtUnixMilli)
	require.Equal(t, now.UnixMilli(), r.CheckedAtUnixMilli)
	require.Equal(t, "rate_limited", r.ErrorCode)
	require.Equal(t, retry.UnixMilli(), r.RetryAtUnixMilli)
}
func TestChattersFreshOfflineCheckSkipsAttendance(t *testing.T) {
	now := time.Now()
	api := &chatterFakeAPI{live: false}
	c := chatterHandler(api, now)
	req := chatterReq(now)
	req.CheckLive = true
	r := c.handleGet(t.Context(), req)
	require.Equal(t, "", r.Error)
	require.False(t, r.Live)
	require.True(t, r.Complete)
	require.Equal(t, now.UnixMilli(), r.CheckedAtUnixMilli)
	require.Equal(t, 0, api.calls)
}
func chatterClient(rt func(*http.Request) (*http.Response, error)) *twitch.Client {
	api := twitch.NewClient("client", twitch.NewStaticTokenSource("app"), twitch.NewStaticTokenSource("bot"), nil)
	api.SetTransport(chatterRevocationTransport(rt))
	return api
}

func chatterJSON(status int, body string) (*http.Response, error) {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
}

const (
	liveStreamBody = `{"data":[{"id":"stream","user_id":"123","type":"live","started_at":"2026-09-26T00:00:00Z"}]}`
	emptyPageBody  = `{"data":[],"pagination":{}}`
)

func TestChattersPageUsesSharedBotQuotaAndBoundedWatchShare(t *testing.T) {
	now := time.Now()
	limiter := &chatterFakeLimiter{allow: true}
	api := chatterClient(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/helix/streams" {
			return chatterJSON(200, liveStreamBody)
		}
		return chatterJSON(200, emptyPageBody)
	})
	c := newChatters(api, now, ChattersOptions{Limiter: limiter, Admit: chatterAdmitAll})
	req := chatterReq(now)
	req.CheckLive = true

	reply := c.handleGet(t.Context(), req)

	require.Empty(t, reply.ErrorCode)
	var tenants []string
	for _, r := range limiter.requests {
		tenants = append(tenants, r.Bucket.Scope+"="+r.Bucket.Value)
	}
	var keys [][2]string
	for _, pair := range limiter.pairs {
		keys = append(keys, [2]string{pair[0].Key, pair[1].Key})
	}
	require.Equal(t, []string{"watch:tenant=123", "watch:tenant=123"}, tenants)
	require.Equal(t, [][2]string{
		{"ratelimit:watch:live", "ratelimit:helix:app"},
		{"ratelimit:watch:chatters", "ratelimit:helix:user:bot"},
	}, keys)
}
func TestChattersRateAdmissionStopsBeforeSharedSpend(t *testing.T) {
	tests := []struct {
		name      string
		limiter   *chatterFakeLimiter
		wantPairs int
	}{
		{"tenant quota denied", &chatterFakeLimiter{allow: false}, 0},
		{"shared quota denied", &chatterFakeLimiter{allow: true, denied: 2}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Now()
			httpCalls := 0
			api := chatterClient(func(*http.Request) (*http.Response, error) {
				httpCalls++
				return chatterJSON(200, emptyPageBody)
			})
			c := newChatters(api, now, ChattersOptions{Limiter: tt.limiter, Admit: chatterAdmitAll})

			reply := c.handleGet(t.Context(), chatterReq(now))

			require.Equal(t, "rate_limited", reply.ErrorCode)
			require.Greater(t, reply.RetryAtUnixMilli, now.UnixMilli())
			require.Equal(t, tt.wantPairs, len(tt.limiter.pairs))
			require.Zero(t, httpCalls, "a quota denial never reaches HTTP")
		})
	}
}

type chatterRevocationTransport func(*http.Request) (*http.Response, error)

func (f chatterRevocationTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
func TestChattersRevocationStopsNextPageAnd401Retry(t *testing.T) {
	for _, mode := range []string{"live check", "401 retry"} {
		t.Run(mode, func(t *testing.T) {
			now := time.Now()
			active := true
			calls := 0
			api := chatterClient(func(req *http.Request) (*http.Response, error) {
				calls++
				active = false
				status := 200
				body := `{"data":[{"id":"stream","user_id":"123","type":"live","started_at":"2026-09-26T00:00:00Z"}]}`
				if mode == "401 retry" {
					status = 401
					body = `{"error":"Unauthorized","message":"Invalid OAuth token"}`
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			limiter := &chatterFakeLimiter{allow: true}
			c := &chatters{twitch: api, botID: "456", log: zap.NewNop(), now: func() time.Time { return now }, opts: ChattersOptions{Limiter: limiter, Admit: func(context.Context, manage.ChattersRequest) (bool, error) { return active, nil }}}
			req := chatterReq(now)
			req.CheckLive = mode == "live check"
			reply := c.handleGet(t.Context(), req)
			require.Equal(t, "inactive", reply.ErrorCode)
			require.Equal(t, 1, calls)
			require.Equal(t, 1, len(limiter.pairs))
			require.False(t, req.CheckLive && (!reply.Live || reply.CheckedAtUnixMilli == 0),
				"successful live confirmation lost")
		})
	}
}

type chatterCooldownFixture struct {
	now    time.Time
	calls  int
	resets map[string]time.Time
	api    *twitch.Client
}

func newChatterCooldownFixture(now time.Time) *chatterCooldownFixture {
	f := &chatterCooldownFixture{now: now, resets: map[string]time.Time{}}
	f.api = twitch.NewClient("client", twitch.NewStaticTokenSource("app"), twitch.NewStaticTokenSource("bot"), nil)
	f.api.SetTransport(chatterRevocationTransport(f.response))
	return f
}
func (f *chatterCooldownFixture) response(*http.Request) (*http.Response, error) {
	f.calls++
	if f.calls == 1 {
		header := make(http.Header)
		header.Set("Ratelimit-Reset", strconv.FormatInt(f.now.Add(time.Minute).Unix(), 10))
		return &http.Response{StatusCode: 429, Header: header, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	}
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"data":[],"pagination":{}}`))}, nil
}
func (f *chatterCooldownFixture) retryAt(_ context.Context, id string) (time.Time, error) {
	reset := f.resets[id]
	if !reset.After(f.now) {
		return time.Time{}, nil
	}
	return reset, nil
}
func (f *chatterCooldownFixture) observe(_ context.Context, id string, reset time.Time) error {
	f.resets[id] = reset
	return nil
}
func (f *chatterCooldownFixture) replica() *chatters {
	opts := ChattersOptions{Limiter: &chatterFakeLimiter{allow: true}, Admit: func(context.Context, manage.ChattersRequest) (bool, error) { return true, nil }, ProviderRetryAt: f.retryAt, ObserveProviderReset: f.observe}
	return &chatters{twitch: f.api, botID: "456", log: zap.NewNop(), now: func() time.Time { return f.now }, opts: opts}
}
func TestChattersProviderResetIsSharedAcrossTenantsAndReplicas(t *testing.T) {
	f := newChatterCooldownFixture(time.Now())
	req := chatterReq(f.now)
	first := f.replica().handleGet(t.Context(), req)
	require.Equal(t, "rate_limited", first.ErrorCode)
	require.False(t, f.resets["helix:bot:456"].IsZero())
	req.BroadcasterID = "789"
	second := f.replica().handleGet(t.Context(), req)
	require.Equal(t, "rate_limited", second.ErrorCode)
	require.Equal(t, first.RetryAtUnixMilli, second.RetryAtUnixMilli)
	require.Equal(t, 1, f.calls)
	// An app-token live probe remains available during the bot-token cooldown.
	req.CheckLive = true
	third := f.replica().handleGet(t.Context(), req)
	require.Empty(t, third.ErrorCode)
	require.True(t, third.Complete)
	require.Equal(t, 2, f.calls)
	f.now = f.resets["helix:bot:456"].Add(time.Second)
	req = chatterReq(f.now)
	req.BroadcasterID = "789"
	fourth := f.replica().handleGet(t.Context(), req)
	require.Empty(t, fourth.ErrorCode)
	require.Equal(t, 3, f.calls)
}
func TestChattersProviderCooldownErrorsFailClosed(t *testing.T) {
	for _, mode := range []string{"read", "write"} {
		t.Run(mode, func(t *testing.T) {
			now := time.Now()
			calls := 0
			quota := &chatterFakeLimiter{allow: true}
			api := chatterClient(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: 429, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`))}, nil
			})
			opts := ChattersOptions{Limiter: quota, Admit: func(context.Context, manage.ChattersRequest) (bool, error) { return true, nil }, ProviderRetryAt: func(context.Context, string) (time.Time, error) {
				if mode == "read" {
					return time.Time{}, errors.New("down")
				}
				return time.Time{}, nil
			}, ObserveProviderReset: func(context.Context, string, time.Time) error { return errors.New("down") }}
			c := &chatters{twitch: api, botID: "456", log: zap.NewNop(), now: func() time.Time { return now }, opts: opts}
			reply := c.handleGet(t.Context(), chatterReq(now))
			require.Equal(t, "unavailable", reply.ErrorCode)
			require.False(t, mode == "read" && (calls != 0 || len(quota.requests) != 0),
				"cooldown read failure spent quota or HTTP")
			require.False(t, mode == "write" && calls != 1,
				"did not observe provider response")
		})
	}
}
func TestChattersAppCooldownDoesNotBlockBotAndLocalQuotaDoesNotPersist(t *testing.T) {
	now := time.Now()
	httpCalls, writes := 0, 0
	limiter := &chatterFakeLimiter{allow: true}
	api := chatterClient(func(req *http.Request) (*http.Response, error) {
		httpCalls++
		return chatterJSON(200, emptyPageBody)
	})
	c := newChatters(api, now, ChattersOptions{
		Limiter: limiter, Admit: chatterAdmitAll,
		ProviderRetryAt: func(_ context.Context, id string) (time.Time, error) {
			if id == "helix:app" {
				return now.Add(time.Minute), nil
			}
			return time.Time{}, nil
		},
		ObserveProviderReset: func(context.Context, string, time.Time) error { writes++; return nil },
	})
	live := chatterReq(now)
	live.CheckLive = true

	require.Equal(t, "rate_limited", c.handleGet(t.Context(), live).ErrorCode, "the app cooldown stops the live probe")
	require.Zero(t, httpCalls)

	require.Empty(t, c.handleGet(t.Context(), chatterReq(now)).ErrorCode, "the bot token is not blocked by the app cooldown")
	require.Equal(t, 1, httpCalls)

	limiter.allow = false
	require.Equal(t, "rate_limited", c.handleGet(t.Context(), chatterReq(now)).ErrorCode)
	require.Zero(t, writes, "a local quota denial is not a provider observation")
}

func TestChattersProviderResetSurvivesCallerCancellation(t *testing.T) {
	now := time.Now()
	ctx, cancel := context.WithCancel(t.Context())
	api := &chatterFakeAPI{
		err:    &twitch.AdmissionError{Provider: true, Code: "rate_limited", RetryAt: now.Add(time.Minute)},
		onPage: func(context.Context) { cancel() },
	}
	c := chatterHandler(api, now)
	observed := false
	c.opts.ObserveProviderReset = func(ctx context.Context, id string, reset time.Time) error {
		require.NoError(t, ctx.Err())
		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		require.LessOrEqual(t, time.Until(deadline), time.Second)
		observed = true
		return nil
	}

	reply := c.handleGet(ctx, chatterReq(now))

	require.True(t, observed)
	require.Equal(t, "rate_limited", reply.ErrorCode)
	require.Equal(t, now.Add(time.Minute).UnixMilli(), reply.RetryAtUnixMilli)
}

type chatterSlow429Body struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *chatterSlow429Body) Read([]byte) (int, error) {
	b.once.Do(func() { close(b.entered) })
	<-b.release
	return 0, io.EOF
}
func (b *chatterSlow429Body) Close() error { return nil }
func TestChatters429HeadersPublishSharedResetBeforeBodyDrain(t *testing.T) {
	now := time.Now()
	body := &chatterSlow429Body{entered: make(chan struct{}), release: make(chan struct{})}
	calls := 0
	var mu sync.Mutex
	resets := map[string]time.Time{}
	api := chatterClient(func(*http.Request) (*http.Response, error) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n == 1 {
			h := make(http.Header)
			h.Set("Ratelimit-Reset", strconv.FormatInt(now.Add(time.Minute).Unix(), 10))
			return &http.Response{StatusCode: 429, Header: h, Body: body}, nil
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"data":[]}`))}, nil
	})
	opts := ChattersOptions{Limiter: &chatterFakeLimiter{allow: true}, Admit: func(context.Context, manage.ChattersRequest) (bool, error) { return true, nil }, ProviderRetryAt: func(_ context.Context, id string) (time.Time, error) {
		mu.Lock()
		defer mu.Unlock()
		reset := resets[id]
		if !reset.After(now) {
			return time.Time{}, nil
		}
		return reset, nil
	}, ObserveProviderReset: func(_ context.Context, id string, at time.Time) error {
		mu.Lock()
		defer mu.Unlock()
		resets[id] = at
		return nil
	}}
	c := &chatters{twitch: api, botID: "456", log: zap.NewNop(), now: func() time.Time { return now }, opts: opts}
	done := make(chan manage.ChattersReply)
	go func() { done <- c.handleGet(t.Context(), chatterReq(now)) }()
	<-body.entered
	req := chatterReq(now)
	req.BroadcasterID = "789"
	second := c.handleGet(t.Context(), req)
	mu.Lock()
	n := calls
	mu.Unlock()
	close(body.release)
	first := <-done
	require.Equal(t, "rate_limited", second.ErrorCode)
	require.Equal(t, 1, n)
	require.Equal(t, "rate_limited", first.ErrorCode)
	require.Equal(t, first.RetryAtUnixMilli, second.RetryAtUnixMilli)
}

func TestChattersProviderAuthorityOwnsCooldownExpiry(t *testing.T) {
	now := time.Now()
	podClock := now.Add(time.Hour)
	httpCalls := 0
	limiter := &chatterFakeLimiter{allow: true}
	api := chatterClient(func(*http.Request) (*http.Response, error) {
		httpCalls++
		return chatterJSON(200, emptyPageBody)
	})
	c := newChatters(api, podClock, ChattersOptions{
		Limiter: limiter, Admit: chatterAdmitAll,
		ProviderRetryAt: func(context.Context, string) (time.Time, error) { return now.Add(time.Minute), nil },
	})

	reply := c.handleGet(t.Context(), chatterReq(podClock))

	require.Equal(t, "rate_limited", reply.ErrorCode, "a cooldown the authority still reports is live whatever this pod's clock says")
	require.Empty(t, limiter.requests)
	require.Zero(t, httpCalls)
}

func TestLegacyViewerListingUsesIndependentAdmissionAndSharedQuota(t *testing.T) {
	now := time.Now()
	calls := 0
	quota := &chatterFakeLimiter{allow: true}
	api := chatterClient(func(req *http.Request) (*http.Response, error) {
		calls++
		cursor := req.URL.Query().Get("after")
		body := `{"data":[{"user_id":"11","user_login":"one"}],"pagination":{"cursor":"next"}}`
		if cursor == "next" {
			body = `{"data":[{"user_id":"12","user_login":"two"}],"pagination":{}}`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	c := &chatters{twitch: api, botID: "456", now: func() time.Time { return now }, opts: ChattersOptions{Limiter: quota, Admit: func(context.Context, manage.ChattersRequest) (bool, error) {
		t.Fatal("viewer listing used loyalty admission")
		return false, nil
	}, AdmitViewer: func(context.Context, string) (bool, error) { return true, nil }}}
	reply := c.handleGet(t.Context(), manage.ChattersRequest{BroadcasterID: "123"})
	require.Equal(t, "", reply.Error)
	require.True(t, reply.Complete)
	require.Equal(t, 2, len(reply.Chatters))
	require.Equal(t, 2, calls)
	require.Equal(t, 2, len(quota.pairs))
}

func TestLegacyViewerIncompleteListingNeverReturnsPartialAttendance(t *testing.T) {
	now := time.Now()
	calls := 0
	quota := &chatterFakeLimiter{allow: true}
	api := chatterClient(func(*http.Request) (*http.Response, error) {
		calls++
		body := fmt.Sprintf(`{"data":[{"user_id":"11","user_login":"one"}],"pagination":{"cursor":"%d"}}`, calls)
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	c := &chatters{twitch: api, botID: "456", now: func() time.Time { return now }, opts: ChattersOptions{Limiter: quota, AdmitViewer: func(context.Context, string) (bool, error) { return true, nil }}}
	reply := c.handleGet(t.Context(), manage.ChattersRequest{BroadcasterID: "123"})
	require.NotEqual(t, "", reply.Error)
	require.False(t, reply.Complete)
	require.Equal(t, 0, len(reply.Chatters))
	require.Equal(t, viewerChattersMaxPages, calls)
}

func TestLegacyViewerCooldownAndRevocationStopHTTP(t *testing.T) {
	for _, mode := range []string{"cooldown", "revocation", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			now := time.Now()
			calls := 0
			quota := &chatterFakeLimiter{allow: true}
			active := true
			api := chatterClient(func(*http.Request) (*http.Response, error) {
				calls++
				active = false
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"data":[],"pagination":{"cursor":"next"}}`))}, nil
			})
			c := &chatters{twitch: api, botID: "456", now: func() time.Time { return now }, opts: ChattersOptions{Limiter: quota, AdmitViewer: func(context.Context, string) (bool, error) { return active, nil }, ProviderRetryAt: func(context.Context, string) (time.Time, error) {
				if mode == "cooldown" {
					return now.Add(time.Minute), nil
				}
				return time.Time{}, nil
			}}}
			req := manage.ChattersRequest{BroadcasterID: "123"}
			if mode == "deadline" {
				req.DeadlineUnixMilli = now.Add(-time.Second).UnixMilli()
			}
			reply := c.handleGet(t.Context(), req)
			wantCalls := 0
			wantCode := "rate_limited"
			if mode == "revocation" {
				wantCalls = 1
				wantCode = "inactive"
			}
			if mode == "deadline" {
				wantCode = "expired"
			}
			require.Equal(t, wantCode, reply.ErrorCode)
			require.Equal(t, wantCalls, calls)
			require.Equal(t, 0, len(reply.Chatters))
			require.Equal(t, wantCalls, len(quota.pairs))
		})
	}
}

func TestChattersRPCReportsMissingBotScope(t *testing.T) {
	now := time.Now()
	api := &chatterFakeAPI{err: &twitch.ChatterAuthorizationError{Status: http.StatusUnauthorized, MissingScope: "moderator:read:chatters"}}
	reply := chatterHandler(api, now).handleGet(t.Context(), chatterReq(now))
	require.Equal(t, "authorization", reply.ErrorCode)
	require.True(t, reply.MissingScope)
	require.Contains(t, reply.Error, "moderator:read:chatters")
	require.Contains(t, reply.Error, "reauthorize the bot")
	require.Empty(t, reply.Chatters)
}
