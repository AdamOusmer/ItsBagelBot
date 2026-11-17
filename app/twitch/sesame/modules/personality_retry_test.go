// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/internal/domain/rpc"
	"ItsBagelBot/internal/valkeytest"
	"ItsBagelBot/pkg/bus"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type retryPersonalityStore struct {
	*fakePersonality
	failures int
	cancel   context.CancelFunc
	failure  error
}

func (s *retryPersonalityStore) Feed(ctx context.Context, id uint64, name, eventID string) (engine.FeedCounts, error) {
	counts, _ := s.fakePersonality.Feed(ctx, id, name, eventID)
	if s.cancel != nil {
		s.cancel()
	}
	if len(s.feedIDs) <= s.failures {
		return engine.FeedCounts{}, s.failure
	}
	return counts, nil
}

func TestPersonalityFeedRetryBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name      string
		identity  string
		failures  int
		failure   error
		cancel    bool
		wantCalls int
		wantReply int
		wantErr   error
	}{
		{name: "deadline", identity: "one", failures: 1, failure: context.DeadlineExceeded, wantCalls: 2, wantReply: 1},
		{name: "timeout", identity: "one", failures: 1, failure: nats.ErrTimeout, wantCalls: 2, wantReply: 1},
		{name: "no responders", identity: "one", failures: 1, failure: fmt.Errorf("wrapped: %w", nats.ErrNoResponders), wantCalls: 2, wantReply: 1},
		{name: "unavailable", identity: "one", failures: 1, failure: bus.RPCReplyError{Code: rpc.CodeUnavailable}, wantCalls: 2, wantReply: 1},
		{name: "exhausted", identity: "one", failures: 3, failure: nats.ErrTimeout, wantCalls: 3, wantErr: nats.ErrTimeout},
		{name: "canceled", identity: "one", failures: 3, failure: nats.ErrTimeout, cancel: true, wantCalls: 1, wantErr: context.Canceled},
		{name: "invalid", identity: "one", failures: 3, failure: bus.RPCReplyError{Code: rpc.CodeInvalid}, wantCalls: 1, wantErr: bus.RPCReplyError{Code: rpc.CodeInvalid}},
		{name: "range", identity: "one", failures: 3, failure: errors.New("personality: feed total outside signed integer range"), wantCalls: 1},
		{name: "backend", identity: "one", failures: 3, failure: errors.New("personality: no feed total backend"), wantCalls: 1},
		{name: "unknown", identity: "one", failures: 3, failure: assert.AnError, wantCalls: 1, wantErr: assert.AnError},
		{name: "empty identity", wantCalls: 1, wantReply: 1},
		{name: "empty identity failure", failures: 1, failure: nats.ErrTimeout, wantCalls: 1, wantErr: nats.ErrTimeout},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pinPersonalityRand(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			store := &retryPersonalityStore{fakePersonality: &fakePersonality{}, failures: tc.failures, failure: tc.failure}
			if tc.cancel {
				store.cancel = cancel
			}
			m := Personality(engine.Deps{Personality: store, Cooldown: engine.NewValkeyCooldown(valkeytest.Spawn(t))})
			c := personalityCtx("feed the bagel")
			c.Env.MsgID = tc.identity
			var col collector
			err := m.Events[c.Env.Type](ctx, c, col.emit)
			wantErr := tc.wantErr
			if wantErr == nil {
				wantErr = tc.failure
			}
			if tc.wantReply > 0 {
				wantErr = nil
			}
			require.ErrorIs(t, err, wantErr)
			assert.Equal(t, struct{ Calls, Replies int }{tc.wantCalls, tc.wantReply}, struct{ Calls, Replies int }{len(store.feedIDs), len(col.out)})
		})
	}
}

type slowPersonalityStore struct {
	*fakePersonality
	deadlines []time.Time
}

func (s *slowPersonalityStore) Feed(ctx context.Context, _ uint64, _, _ string) (engine.FeedCounts, error) {
	deadline, _ := ctx.Deadline()
	s.deadlines = append(s.deadlines, deadline)
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	<-ctx.Done()
	return engine.FeedCounts{}, ctx.Err()
}

func TestPersonalityFeedSharesDeadline(t *testing.T) {
	for _, tc := range []struct {
		name   string
		budget time.Duration
		calls  int
	}{
		{"overall", 10 * time.Second, 2},
		{"parent", 100 * time.Millisecond, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pinPersonalityRand(t)
			ctx, cancel := context.WithTimeout(t.Context(), tc.budget)
			defer cancel()
			store := &slowPersonalityStore{fakePersonality: &fakePersonality{}}
			c := personalityCtx("feed the bagel")
			c.Env.MsgID = "one"
			var col collector
			start := time.Now()
			m := Personality(engine.Deps{Personality: store})
			err := m.Events[c.Env.Type](ctx, c, col.emit)
			require.ErrorIs(t, err, context.DeadlineExceeded)
			assert.WithinDuration(t, start.Add(min(tc.budget, 2500*time.Millisecond)), time.Now(), time.Second)
			require.Len(t, store.deadlines, tc.calls)
			for _, deadline := range store.deadlines {
				assert.Equal(t, store.deadlines[0], deadline)
			}
			assert.Empty(t, col.out)
		})
	}
}

func TestGoldenFeedSkipsWrite(t *testing.T) {
	for _, tc := range []struct {
		name  string
		store *fakePersonality
		trial bool
	}{
		{name: "healthy", store: &fakePersonality{}},
		{name: "unavailable", store: &fakePersonality{err: assert.AnError}},
		{name: "no store"},
		{name: "trial", store: &fakePersonality{}, trial: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			patchPersonalityRand(t, func(int) int { return 0 }, func() bool { return true })
			c := personalityCtx("feed the bagel")
			if tc.trial {
				c.Env.Origin = "trial"
			}
			d := engine.Deps{}
			if tc.store != nil {
				d.Personality = tc.store
			}
			out := runEvent(t, Personality(d), c)
			require.Len(t, out, 1)
			assert.Equal(t, expandUser(i18n.T(c.Locale, "personality.golden"), c), out[0].Text)
			if tc.store != nil {
				assert.Empty(t, tc.store.writes)
			}
		})
	}
}
