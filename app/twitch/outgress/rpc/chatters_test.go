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
	"go.uber.org/zap"
)

type chatterFakeAPI struct {
	calls                  int
	liveCalls              int
	live                   bool
	page                   twitch.ChattersPage
	err                    error
	bid, moderator, cursor string
}

func (a *chatterFakeAPI) GetChattersPage(ctx context.Context, bid, mod, cursor string) (twitch.ChattersPage, error) {
	a.calls++
	a.bid = bid
	a.moderator = mod
	a.cursor = cursor
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
func chatterHandler(api *chatterFakeAPI, now time.Time) *chatters {
	return &chatters{twitch: api, botID: "456", log: zap.NewNop(), now: func() time.Time { return now }, opts: ChattersOptions{Limiter: &chatterFakeLimiter{allow: true}, Admit: func(context.Context, manage.ChattersRequest) (bool, error) { return true, nil }}}
}
func chatterReq(now time.Time) manage.ChattersRequest {
	return manage.ChattersRequest{BroadcasterID: "123", RequestID: "request-1", WindowID: "window-1", SessionGeneration: "gen", LiveSession: "session", DeadlineUnixMilli: now.Add(time.Second).UnixMilli()}
}

func TestChattersExpiredQueuedWorkNeverCallsAPIOrAdmission(t *testing.T) {
	now := time.Now()
	api := &chatterFakeAPI{}
	c := chatterHandler(api, now)
	admitted := false
	c.opts.Admit = func(context.Context, manage.ChattersRequest) (bool, error) { admitted = true; return true, nil }
	req := chatterReq(now)
	req.DeadlineUnixMilli = now.Add(-time.Second).UnixMilli()
	reply := c.handleGet(t.Context(), req)
	if reply.ErrorCode != "expired" || api.calls != 0 || api.liveCalls != 0 || admitted {
		t.Fatalf("expired work executed: %+v calls=%d admission=%v", reply, api.calls, admitted)
	}
}
func TestChattersAdmissionFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name   string
		active bool
		err    error
		code   string
	}{
		{"removed", false, nil, "inactive"}, {"authority unavailable", false, errors.New("down"), "unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now()
			api := &chatterFakeAPI{}
			c := chatterHandler(api, now)
			c.opts.Admit = func(context.Context, manage.ChattersRequest) (bool, error) { return tc.active, tc.err }
			r := c.handleGet(t.Context(), chatterReq(now))
			if r.ErrorCode != tc.code || api.calls != 0 {
				t.Fatalf("reply=%+v calls=%d", r, api.calls)
			}
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
	if r.Error != "" || r.BroadcasterID != req.BroadcasterID || r.RequestID != req.RequestID || r.WindowID != req.WindowID || r.SessionGeneration != req.SessionGeneration || r.LiveSession != req.LiveSession || r.NextCursor != "next" || r.Complete || api.bid != "123" || api.moderator != "456" || api.cursor != "previous" {
		t.Fatalf("correlation lost: %+v api=%+v", r, api)
	}
}
func TestChattersPreservesFreshLiveCheckWhenPageFails(t *testing.T) {
	now := time.Now()
	retry := now.Add(time.Minute)
	api := &chatterFakeAPI{live: true, err: &twitch.AdmissionError{Code: "rate_limited", RetryAt: retry}}
	c := chatterHandler(api, now)
	req := chatterReq(now)
	req.CheckLive = true
	r := c.handleGet(t.Context(), req)
	if !r.Live || r.StreamID != "stream-1" || r.StreamStartedAtUnixMilli != time.Unix(100, 0).UnixMilli() || r.CheckedAtUnixMilli != now.UnixMilli() || r.ErrorCode != "rate_limited" || r.RetryAtUnixMilli != retry.UnixMilli() {
		t.Fatalf("live confirmation lost: %+v", r)
	}
}
func TestChattersFreshOfflineCheckSkipsAttendance(t *testing.T) {
	now := time.Now()
	api := &chatterFakeAPI{live: false}
	c := chatterHandler(api, now)
	req := chatterReq(now)
	req.CheckLive = true
	r := c.handleGet(t.Context(), req)
	if r.Error != "" || r.Live || !r.Complete || r.CheckedAtUnixMilli != now.UnixMilli() || api.calls != 0 {
		t.Fatalf("offline confirmation ignored: %+v", r)
	}
}
func TestChattersPageUsesSharedBotQuotaAndBoundedWatchShare(t *testing.T) {
	now := time.Now()
	c := chatterHandler(&chatterFakeAPI{}, now)
	l := &chatterFakeLimiter{allow: true}
	c.opts.Limiter = l
	if err := c.admit(manage.ChattersRequest{BroadcasterID: "123", RequestID: "watch-test"})(t.Context(), "/helix/chat/chatters?broadcaster_id=123"); err != nil {
		t.Fatal(err)
	}
	if len(l.requests) != 1 || l.requests[0].Bucket.Scope != "watch:tenant" || l.requests[0].Bucket.Value != "123" || len(l.pairs) != 1 || l.pairs[0][0].Key != "ratelimit:watch:chatters" || l.pairs[0][1].Key != "ratelimit:helix:user:bot" {
		t.Fatalf("wrong quota keys: tenant=%+v pairs=%+v", l.requests, l.pairs)
	}
	if err := c.admit(manage.ChattersRequest{BroadcasterID: "789", RequestID: "watch-test"})(t.Context(), "/helix/streams?user_id=789"); err != nil {
		t.Fatal(err)
	}
	if l.requests[1].Bucket.Value != "789" || l.pairs[1][0].Key != "ratelimit:watch:live" || l.pairs[1][1].Key != "ratelimit:helix:app" {
		t.Fatal("live check not charged to app quota")
	}
}
func TestChattersRateAdmissionStopsBeforeSharedSpend(t *testing.T) {
	now := time.Now()
	c := chatterHandler(&chatterFakeAPI{}, now)
	l := &chatterFakeLimiter{allow: false}
	c.opts.Limiter = l
	err := c.admit(manage.ChattersRequest{BroadcasterID: "123", RequestID: "watch-test"})(t.Context(), "/helix/chat/chatters")
	var rate *twitch.AdmissionError
	if !errors.As(err, &rate) || rate.Code != "rate_limited" || !rate.RetryAt.After(now) || len(l.pairs) != 0 {
		t.Fatalf("tenant denial did not stop: %v %+v", err, l.pairs)
	}
	l.allow = true
	l.denied = 2
	err = c.admit(manage.ChattersRequest{BroadcasterID: "123", RequestID: "watch-test"})(t.Context(), "/helix/chat/chatters")
	if !errors.As(err, &rate) || rate.Code != "rate_limited" {
		t.Fatalf("shared denial ignored: %v", err)
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
			api := twitch.NewClient("client", twitch.NewStaticTokenSource("app"), twitch.NewStaticTokenSource("bot"), nil)
			api.SetTransport(chatterRevocationTransport(func(req *http.Request) (*http.Response, error) {
				calls++
				active = false
				status := 200
				body := `{"data":[{"id":"stream","user_id":"123","type":"live","started_at":"2026-09-26T00:00:00Z"}]}`
				if mode == "401 retry" {
					status = 401
					body = `{"error":"Unauthorized","message":"Invalid OAuth token"}`
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			}))
			limiter := &chatterFakeLimiter{allow: true}
			c := &chatters{twitch: api, botID: "456", log: zap.NewNop(), now: func() time.Time { return now }, opts: ChattersOptions{Limiter: limiter, Admit: func(context.Context, manage.ChattersRequest) (bool, error) { return active, nil }}}
			req := chatterReq(now)
			req.CheckLive = mode == "live check"
			reply := c.handleGet(t.Context(), req)
			if reply.ErrorCode != "inactive" || calls != 1 || len(limiter.pairs) != 1 {
				t.Fatalf("revocation spent provider work: reply=%+v http=%d quota=%d", reply, calls, len(limiter.pairs))
			}
			if req.CheckLive && (!reply.Live || reply.CheckedAtUnixMilli == 0) {
				t.Fatal("successful live confirmation lost")
			}
		})
	}
}

func TestChattersProviderResetIsSharedAcrossTenantsAndReplicas(t *testing.T) {
	now := time.Now()
	calls := 0
	resets := map[string]time.Time{}
	api := twitch.NewClient("client", twitch.NewStaticTokenSource("app"), twitch.NewStaticTokenSource("bot"), nil)
	api.SetTransport(chatterRevocationTransport(func(req *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			header := make(http.Header)
			header.Set("Ratelimit-Reset", strconv.FormatInt(now.Add(time.Minute).Unix(), 10))
			return &http.Response{StatusCode: 429, Header: header, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
		}
		body := `{"data":[],"pagination":{}}`
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	}))
	opts := ChattersOptions{Limiter: &chatterFakeLimiter{allow: true}, Admit: func(context.Context, manage.ChattersRequest) (bool, error) { return true, nil }, ProviderRetryAt: func(_ context.Context, id string) (time.Time, error) {
		reset := resets[id]
		if !reset.After(now) {
			return time.Time{}, nil
		}
		return reset, nil
	}, ObserveProviderReset: func(_ context.Context, id string, reset time.Time) error { resets[id] = reset; return nil }}
	makeReplica := func() *chatters {
		return &chatters{twitch: api, botID: "456", log: zap.NewNop(), now: func() time.Time { return now }, opts: opts}
	}
	req := chatterReq(now)
	first := makeReplica().handleGet(t.Context(), req)
	if first.ErrorCode != "rate_limited" || resets["helix:bot:456"].IsZero() {
		t.Fatalf("provider reset lost: %+v map=%v", first, resets)
	}
	req.BroadcasterID = "789"
	second := makeReplica().handleGet(t.Context(), req)
	if second.ErrorCode != "rate_limited" || second.RetryAtUnixMilli != first.RetryAtUnixMilli || calls != 1 {
		t.Fatalf("replica continued exhausted bot token: %+v http=%d", second, calls)
	}
	// The app identity remains available while only the bot identity is exhausted.
	req.CheckLive = true
	third := makeReplica().handleGet(t.Context(), req)
	if third.ErrorCode != "" || calls != 2 || !third.Complete {
		t.Fatalf("bot cooldown polluted app quota: %+v http=%d", third, calls)
	}
	now = resets["helix:bot:456"].Add(time.Second)
	req = chatterReq(now)
	req.BroadcasterID = "789"
	fourth := makeReplica().handleGet(t.Context(), req)
	if fourth.ErrorCode != "" || calls != 3 {
		t.Fatalf("cooldown did not expire: %+v http=%d", fourth, calls)
	}
}
func TestChattersProviderCooldownErrorsFailClosed(t *testing.T) {
	for _, mode := range []string{"read", "write"} {
		t.Run(mode, func(t *testing.T) {
			now := time.Now()
			calls := 0
			quota := &chatterFakeLimiter{allow: true}
			api := twitch.NewClient("client", twitch.NewStaticTokenSource("app"), twitch.NewStaticTokenSource("bot"), nil)
			api.SetTransport(chatterRevocationTransport(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: 429, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`))}, nil
			}))
			opts := ChattersOptions{Limiter: quota, Admit: func(context.Context, manage.ChattersRequest) (bool, error) { return true, nil }, ProviderRetryAt: func(context.Context, string) (time.Time, error) {
				if mode == "read" {
					return time.Time{}, errors.New("down")
				}
				return time.Time{}, nil
			}, ObserveProviderReset: func(context.Context, string, time.Time) error { return errors.New("down") }}
			c := &chatters{twitch: api, botID: "456", log: zap.NewNop(), now: func() time.Time { return now }, opts: opts}
			reply := c.handleGet(t.Context(), chatterReq(now))
			if reply.ErrorCode != "unavailable" {
				t.Fatalf("authority failure swallowed: %+v", reply)
			}
			if mode == "read" && (calls != 0 || len(quota.requests) != 0) {
				t.Fatal("cooldown read failure spent quota or HTTP")
			}
			if mode == "write" && calls != 1 {
				t.Fatal("did not observe provider response")
			}
		})
	}
}
func TestChattersAppCooldownDoesNotBlockBotAndLocalQuotaDoesNotPersist(t *testing.T) {
	now := time.Now()
	c := chatterHandler(&chatterFakeAPI{}, now)
	writes := 0
	c.opts.ProviderRetryAt = func(_ context.Context, id string) (time.Time, error) {
		if id == "helix:app" {
			return now.Add(time.Minute), nil
		}
		return time.Time{}, nil
	}
	c.opts.ObserveProviderReset = func(context.Context, string, time.Time) error { writes++; return nil }
	err := c.admit(chatterReq(now))(t.Context(), "/helix/streams?user_id=123")
	var admission *twitch.AdmissionError
	if !errors.As(err, &admission) || admission.Code != "rate_limited" {
		t.Fatalf("app cooldown ignored: %v", err)
	}
	if err := c.admit(chatterReq(now))(t.Context(), "/helix/chat/chatters?broadcaster_id=123"); err != nil {
		t.Fatalf("app cooldown blocked bot: %v", err)
	}
	c.providerFailure(t.Context(), "helix:bot:456", &twitch.AdmissionError{Code: "rate_limited", RetryAt: now.Add(time.Second)})
	if writes != 0 {
		t.Fatal("local quota denial established provider cooldown")
	}
}

func TestChattersProviderResetSurvivesCallerCancellation(t *testing.T) {
	now := time.Now()
	c := chatterHandler(&chatterFakeAPI{}, now)
	observed := false
	c.opts.ObserveProviderReset = func(ctx context.Context, id string, reset time.Time) error {
		if ctx.Err() != nil {
			t.Fatalf("provider feedback inherited expired caller: %v", ctx.Err())
		}
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > time.Second {
			t.Fatal("provider persistence lacks bounded deadline")
		}
		observed = true
		return nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	code, reset := c.providerFailure(ctx, "helix:bot:456", &twitch.AdmissionError{Provider: true, Code: "rate_limited", RetryAt: now.Add(time.Minute)})
	if !observed || code != "rate_limited" || !reset.Equal(now.Add(time.Minute)) {
		t.Fatal("provider reset lost after caller deadline")
	}
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
	api := twitch.NewClient("client", twitch.NewStaticTokenSource("app"), twitch.NewStaticTokenSource("bot"), nil)
	api.SetTransport(chatterRevocationTransport(func(*http.Request) (*http.Response, error) {
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
	}))
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
	if second.ErrorCode != "rate_limited" || n != 1 || first.ErrorCode != "rate_limited" {
		t.Fatalf("unexpected reproduction second=%+v calls=%d first=%+v", second, n, first)
	}
	if second.RetryAtUnixMilli != first.RetryAtUnixMilli {
		t.Fatal("shared headerreset mismatch")
	}
}

func TestChattersProviderAuthorityOwnsCooldownExpiry(t *testing.T) {
	now := time.Now()
	c := chatterHandler(&chatterFakeAPI{}, now.Add(time.Hour))
	limiter := &chatterFakeLimiter{allow: true}
	c.opts.Limiter = limiter
	c.opts.ProviderRetryAt = func(context.Context, string) (time.Time, error) { return now.Add(time.Minute), nil }
	err := c.admit(chatterReq(now))(t.Context(), "/helix/chat/chatters?broadcaster_id=123")
	var denied *twitch.AdmissionError
	if !errors.As(err, &denied) || denied.Code != "rate_limited" || len(limiter.requests) != 0 {
		t.Fatalf("pod clock overrode authoritative cooldown: %v quota=%d", err, len(limiter.requests))
	}
}

func TestLegacyViewerListingUsesIndependentAdmissionAndSharedQuota(t *testing.T) {
	now := time.Now()
	calls := 0
	quota := &chatterFakeLimiter{allow: true}
	api := twitch.NewClient("client", twitch.NewStaticTokenSource("app"), twitch.NewStaticTokenSource("bot"), nil)
	api.SetTransport(chatterRevocationTransport(func(req *http.Request) (*http.Response, error) {
		calls++
		cursor := req.URL.Query().Get("after")
		body := `{"data":[{"user_id":"11","user_login":"one"}],"pagination":{"cursor":"next"}}`
		if cursor == "next" {
			body = `{"data":[{"user_id":"12","user_login":"two"}],"pagination":{}}`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	}))
	c := &chatters{twitch: api, botID: "456", now: func() time.Time { return now }, opts: ChattersOptions{Limiter: quota, Admit: func(context.Context, manage.ChattersRequest) (bool, error) {
		t.Fatal("viewer listing used loyalty admission")
		return false, nil
	}, AdmitViewer: func(context.Context, string) (bool, error) { return true, nil }}}
	reply := c.handleGet(t.Context(), manage.ChattersRequest{BroadcasterID: "123"})
	if reply.Error != "" || !reply.Complete || len(reply.Chatters) != 2 || calls != 2 || len(quota.pairs) != 2 {
		t.Fatalf("legacy listing: reply=%+v HTTP=%d quota=%d", reply, calls, len(quota.pairs))
	}
}

func TestLegacyViewerIncompleteListingNeverReturnsPartialAttendance(t *testing.T) {
	now := time.Now()
	calls := 0
	quota := &chatterFakeLimiter{allow: true}
	api := twitch.NewClient("client", twitch.NewStaticTokenSource("app"), twitch.NewStaticTokenSource("bot"), nil)
	api.SetTransport(chatterRevocationTransport(func(*http.Request) (*http.Response, error) {
		calls++
		body := fmt.Sprintf(`{"data":[{"user_id":"11","user_login":"one"}],"pagination":{"cursor":"%d"}}`, calls)
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	}))
	c := &chatters{twitch: api, botID: "456", now: func() time.Time { return now }, opts: ChattersOptions{Limiter: quota, AdmitViewer: func(context.Context, string) (bool, error) { return true, nil }}}
	reply := c.handleGet(t.Context(), manage.ChattersRequest{BroadcasterID: "123"})
	if reply.Error == "" || reply.Complete || len(reply.Chatters) != 0 || calls != viewerChattersMaxPages {
		t.Fatalf("partial listing escaped: reply=%+v HTTP=%d", reply, calls)
	}
}

func TestLegacyViewerCooldownAndRevocationStopHTTP(t *testing.T) {
	for _, mode := range []string{"cooldown", "revocation", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			now := time.Now()
			calls := 0
			quota := &chatterFakeLimiter{allow: true}
			active := true
			api := twitch.NewClient("client", twitch.NewStaticTokenSource("app"), twitch.NewStaticTokenSource("bot"), nil)
			api.SetTransport(chatterRevocationTransport(func(*http.Request) (*http.Response, error) {
				calls++
				active = false
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"data":[],"pagination":{"cursor":"next"}}`))}, nil
			}))
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
			if reply.ErrorCode != wantCode || calls != wantCalls || len(reply.Chatters) != 0 || len(quota.pairs) != wantCalls {
				t.Fatalf("legacy bound: reply=%+v HTTP=%d quota=%d", reply, calls, len(quota.pairs))
			}
		})
	}
}
