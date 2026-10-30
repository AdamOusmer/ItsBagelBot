// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/outgress/internal/channels"
	"ItsBagelBot/app/twitch/outgress/internal/conduit"
	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	"ItsBagelBot/internal/domain/lifecycle"
	"ItsBagelBot/internal/valkeytest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	valkey_go "github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

type verifyTransport struct {
	present []string
	listErr bool
	gets    atomic.Int32
	posts   atomic.Int32
}

func (v *verifyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method != http.MethodGet {
		v.posts.Add(1)
		return subscriptionResponse(202, `{}`), nil
	}
	v.gets.Add(1)
	if v.listErr {
		return subscriptionResponse(503, `{"message":"outage"}`), nil
	}
	entries := make([]string, 0, len(v.present))
	for _, kind := range v.present {
		userID := testBot
		if kind == twitch.UserUpdateType {
			userID = testBroadcaster
		}
		entries = append(entries, fmt.Sprintf(`{"id":"%s","type":"%s","status":"enabled","transport":{"conduit_id":"conduit"},"condition":{"broadcaster_user_id":"%s","user_id":"%s"}}`, kind, kind, testBroadcaster, userID))
	}
	return subscriptionResponse(200, `{"data":[`+strings.Join(entries, ",")+`]}`), nil
}

func requiredTypesForTest(w *Worker) []string { return w.requiredSubscriptionTypes(testBroadcaster) }

func verifyWorker(t *testing.T, transport http.RoundTripper, client valkey_go.Client) *Worker {
	t.Helper()
	registry := channels.New(client)
	t.Cleanup(registry.Close)
	registry.SeedPause(false)
	return pipelineWorker(t, transport, func(c *Config) {
		c.Registry = registry
		c.Runtime = &fakeRuntime{servingSnapshot()}
		c.Owner = "test"
		c.Conduit = conduit.New(nil, "ingress.conduit", "conduit", time.Minute, zap.NewNop())
	})
}

func storedSubState(t *testing.T, w *Worker) (string, string) {
	t.Helper()
	ch, _, err := w.registry.Get(t.Context(), testBroadcaster)
	require.NoError(t, err)
	return ch.SubState, ch.SubError
}

func TestVerifyChannelAllRequiredPresentRecordsOK(t *testing.T) {
	probe := verifyWorker(t, &verifyTransport{}, valkeytest.Spawn(t))
	transport := &verifyTransport{present: requiredTypesForTest(probe)}
	w := verifyWorker(t, transport, valkeytest.Spawn(t))
	require.NoError(t, w.registry.SetSubState(t.Context(), testBroadcaster, subStateFailing, "stale"))

	v, err := w.VerifyChannel(t.Context(), testBroadcaster, false)

	require.NoError(t, err)
	assert.NotZero(t, v.Expected)
	assert.Equal(t, v.Expected, v.Present)
	assert.Empty(t, v.Missing)
	state, subErr := storedSubState(t, w)
	assert.Equal(t, subStateOK, state)
	assert.Empty(t, subErr)
}

func TestVerifyChannelMissingRecordsFailingAndRepairEnrolls(t *testing.T) {
	probe := verifyWorker(t, &verifyTransport{}, valkeytest.Spawn(t))
	required := requiredTypesForTest(probe)
	for _, repair := range []bool{false, true} {
		t.Run(fmt.Sprintf("repair=%t", repair), func(t *testing.T) {
			transport := &verifyTransport{present: required[1:]}
			w := verifyWorker(t, transport, valkeytest.Spawn(t))

			v, err := w.VerifyChannel(t.Context(), testBroadcaster, repair)

			require.NoError(t, err)
			assert.Equal(t, []string{required[0]}, v.Missing)
			assert.Equal(t, len(required)-1, v.Present)
			assert.Equal(t, repair, v.Repairing)
			if !repair {
				state, subErr := storedSubState(t, w)
				assert.Equal(t, subStateFailing, state)
				assert.Equal(t, "missing: "+required[0], subErr)
				assert.Zero(t, transport.posts.Load())
				return
			}
			require.Eventually(t, func() bool {
				state, _ := storedSubState(t, w)
				return state == subStateOK
			}, 5*time.Second, 10*time.Millisecond)
			assert.NotZero(t, transport.posts.Load())
		})
	}
}

func TestVerifyChannelNeverOverwritesOwnedStates(t *testing.T) {
	probe := verifyWorker(t, &verifyTransport{}, valkeytest.Spawn(t))
	for _, owned := range []string{subStateBanned, subStateRevoked} {
		t.Run(owned, func(t *testing.T) {
			transport := &verifyTransport{present: requiredTypesForTest(probe)[1:]}
			w := verifyWorker(t, transport, valkeytest.Spawn(t))
			require.NoError(t, w.registry.SetSubState(t.Context(), testBroadcaster, owned, "owned"))

			v, err := w.VerifyChannel(t.Context(), testBroadcaster, true)

			require.NoError(t, err)
			assert.Len(t, v.Missing, 1)
			assert.False(t, v.Repairing)
			state, subErr := storedSubState(t, w)
			assert.Equal(t, owned, state)
			assert.Equal(t, "owned", subErr)
			assert.Zero(t, transport.posts.Load())
		})
	}
}

func TestVerifyChannelDormantChannelIsReportedButNeverWritten(t *testing.T) {
	transport := &verifyTransport{}
	w := verifyWorker(t, transport, valkeytest.Spawn(t))
	runtime := &fakeRuntime{servingSnapshot()}
	runtime.snap.State.RuntimeState = lifecycle.Dormant
	w.runtime = runtime

	v, err := w.VerifyChannel(t.Context(), testBroadcaster, true)

	require.NoError(t, err)
	assert.NotEmpty(t, v.Missing)
	assert.False(t, v.Repairing)
	state, _ := storedSubState(t, w)
	assert.Empty(t, state)
	assert.Zero(t, transport.posts.Load())
}

func TestVerifyChannelListErrorKeepsStoredState(t *testing.T) {
	transport := &verifyTransport{listErr: true}
	w := verifyWorker(t, transport, valkeytest.Spawn(t))
	require.NoError(t, w.registry.SetSubState(t.Context(), testBroadcaster, subStateOK, ""))

	v, err := w.VerifyChannel(t.Context(), testBroadcaster, true)

	require.NoError(t, err)
	assert.NotEmpty(t, v.Error)
	assert.False(t, v.Repairing)
	state, _ := storedSubState(t, w)
	assert.Equal(t, subStateOK, state)
	assert.Zero(t, transport.posts.Load())
}

func TestVerifyChannelSecondCallWithinWindowIsThrottled(t *testing.T) {
	transport := &verifyTransport{}
	w := verifyWorker(t, transport, valkeytest.Spawn(t))

	_, err := w.VerifyChannel(t.Context(), testBroadcaster, false)
	require.NoError(t, err)
	gets := transport.gets.Load()
	require.NotZero(t, gets)

	v, err := w.VerifyChannel(t.Context(), testBroadcaster, false)

	require.NoError(t, err)
	assert.True(t, v.Throttled)
	assert.Equal(t, gets, transport.gets.Load())
}

func TestUnhealthySweepHealsServedChannelThenIsLocked(t *testing.T) {
	probe := verifyWorker(t, &verifyTransport{}, valkeytest.Spawn(t))
	transport := &verifyTransport{present: requiredTypesForTest(probe)}
	w := verifyWorker(t, transport, valkeytest.Spawn(t))
	ctx := t.Context()
	require.NoError(t, w.registry.SetSubState(ctx, testBroadcaster, subStateFailing, "stale"))

	w.sweepUnhealthy(ctx)

	state, _ := storedSubState(t, w)
	assert.Equal(t, subStateOK, state)
	ids, err := w.registry.UnhealthyBatch(ctx, 10)
	require.NoError(t, err)
	assert.Empty(t, ids)

	require.NoError(t, w.registry.SetSubState(ctx, testBroadcaster, subStateFailing, "again"))
	gets := transport.gets.Load()
	w.sweepUnhealthy(ctx)
	assert.Equal(t, gets, transport.gets.Load())
	state, _ = storedSubState(t, w)
	assert.Equal(t, subStateFailing, state)
}

func TestUnhealthySweepLeavesDormantChannelInSet(t *testing.T) {
	transport := &verifyTransport{}
	w := verifyWorker(t, transport, valkeytest.Spawn(t))
	runtime := &fakeRuntime{servingSnapshot()}
	runtime.snap.State.RuntimeState = lifecycle.Dormant
	w.runtime = runtime
	ctx := t.Context()
	require.NoError(t, w.registry.SetSubState(ctx, testBroadcaster, subStateFailing, "stale"))

	w.sweepUnhealthy(ctx)

	state, subErr := storedSubState(t, w)
	assert.Equal(t, subStateFailing, state)
	assert.Equal(t, "stale", subErr)
	ids, err := w.registry.UnhealthyBatch(ctx, 10)
	require.NoError(t, err)
	assert.Equal(t, []string{testBroadcaster}, ids)
	assert.Zero(t, transport.posts.Load())
}
