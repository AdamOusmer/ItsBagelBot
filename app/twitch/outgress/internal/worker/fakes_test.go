// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/outgress/internal/channels"
	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	"ItsBagelBot/internal/domain/rpc/manage"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/ratelimit"

	"go.uber.org/zap"
)

const (
	testBroadcaster = "44322889"
	testBot         = "987654"
)

type recordedRequest struct {
	Method string
	Path   string
	Query  string
	Auth   string
	Body   string
}

type scriptedResponse struct {
	status int
	body   string
	err    error
}

// scriptedTransport answers requests in order and records them; once the script is spent it answers 204.
type scriptedTransport struct {
	mu        sync.Mutex
	responses []scriptedResponse
	requests  []recordedRequest
}

func (t *scriptedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		body, _ = io.ReadAll(req.Body)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	next := scriptedResponse{status: http.StatusNoContent}
	if len(t.requests) < len(t.responses) {
		next = t.responses[len(t.requests)]
	}
	t.requests = append(t.requests, recordedRequest{
		Method: req.Method, Path: req.URL.Path, Query: req.URL.RawQuery,
		Auth: req.Header.Get("Authorization"), Body: string(body),
	})
	if next.err != nil {
		return nil, next.err
	}
	return &http.Response{StatusCode: next.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(next.body))}, nil
}

func (t *scriptedTransport) recorded() []recordedRequest {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]recordedRequest(nil), t.requests...)
}

func (t *scriptedTransport) callCount() int { return len(t.recorded()) }

type allowAll struct{}

func (allowAll) Allow(context.Context, ratelimit.Request) (bool, error) { return true, nil }
func (allowAll) AllowOrdered(context.Context, ratelimit.Request, ratelimit.Request) (uint8, error) {
	return 0, nil
}

// scriptedLimiter records every bucket key it is asked about and denies the keys it was told to.
type scriptedLimiter struct {
	mu        sync.Mutex
	denied    map[string]bool
	denyOnce  map[string]bool
	errs      map[string]error
	guardWait time.Duration
	calls     []string
}

func (s *scriptedLimiter) key(req ratelimit.Request) string {
	if req.Key != "" {
		return req.Key
	}
	return req.DynamicPrefix + req.Bucket.Value
}

func (s *scriptedLimiter) Allow(_ context.Context, req ratelimit.Request) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := s.key(req)
	s.calls = append(s.calls, key)
	if err := s.errs[key]; err != nil {
		return false, err
	}
	if s.denyOnce[key] {
		delete(s.denyOnce, key)
		return false, nil
	}
	return !s.denied[key], nil
}

func (s *scriptedLimiter) AllowOrdered(context.Context, ratelimit.Request, ratelimit.Request) (uint8, error) {
	return 0, nil
}

func (s *scriptedLimiter) GuardRetryAfter() time.Duration { return s.guardWait }

func (s *scriptedLimiter) keys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...)
}

type workerOption func(*Config)

func withLimiter(l ratelimit.Manager) workerOption { return func(c *Config) { c.Limiter = l } }

func withBatchStore(s BatchStore) workerOption { return func(c *Config) { c.Batch = s } }

func withLog(log *zap.Logger) workerOption { return func(c *Config) { c.Log = log } }

func withBlocked(b *BlockedLog) workerOption { return func(c *Config) { c.Blocked = b } }

func withLane(l Lane) workerOption { return func(c *Config) { c.Lane = l } }

func withTwitch(c *twitch.Client) workerOption { return func(cfg *Config) { cfg.Twitch = c } }

func newTwitch(rt http.RoundTripper, broadcaster func(string) *twitch.Source) *twitch.Client {
	c := twitch.NewClient("test-client-id", twitch.NewStaticTokenSource("app-token"), twitch.NewStaticTokenSource("bot-token"),
		twitch.NewBroadcasterTokens(broadcaster))
	c.SetTransport(rt)
	return c
}

func staticBroadcaster(string) *twitch.Source {
	return twitch.NewStaticTokenSource("broadcaster-token")
}

// pipelineWorker is a worker whose Twitch calls go to rt and whose registry already knows testBroadcaster as an unpaused moderator channel.
func pipelineWorker(tb testing.TB, rt http.RoundTripper, opts ...workerOption) *Worker {
	tb.Helper()
	registry := channels.New(nil)
	tb.Cleanup(registry.Close)
	registry.SeedPause(false)
	registry.Prime(manage.Channel{BroadcasterID: testBroadcaster, Enabled: true, IsMod: true})

	cfg := Config{
		Log:      zap.NewNop(),
		Limiter:  allowAll{},
		Registry: registry,
		BotID:    testBot,
		Lane:     LanePremium,
		Twitch:   newTwitch(rt, staticBroadcaster),
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	return New(cfg)
}

type testMessage struct {
	Type, Broadcaster, Sender, To, MsgID, Color, As, Method, Endpoint, Payload string
}

func (m testMessage) body() string {
	if m.Broadcaster == "" {
		m.Broadcaster = testBroadcaster
	}
	parts := []string{`"type":"` + m.Type + `"`, `"broadcaster_id":"` + m.Broadcaster + `"`}
	for key, value := range map[string]string{"sender_id": m.Sender, "to": m.To, "msg_id": m.MsgID, "color": m.Color, "as": m.As, "method": m.Method, "endpoint": m.Endpoint} {
		if value != "" {
			parts = append(parts, `"`+key+`":"`+value+`"`)
		}
	}
	if m.Payload != "" {
		parts = append(parts, `"payload":`+m.Payload)
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func (m testMessage) send(w *Worker) error {
	return w.Process(bus.NewMessage("test-message", []byte(m.body())))
}
