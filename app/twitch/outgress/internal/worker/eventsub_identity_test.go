// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/outgress/internal/channels"
	"ItsBagelBot/app/twitch/outgress/internal/conduit"
	"ItsBagelBot/internal/domain/rpc/manage"
	"ItsBagelBot/internal/valkeytest"
	"ItsBagelBot/pkg/bus"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func eventSubIdentityWorker(t *testing.T, transport http.RoundTripper) (*Worker, *valkeytest.Server) {
	t.Helper()
	srv := valkeytest.New(t)
	registry := channels.New(srv.Client())
	t.Cleanup(registry.Close)
	registry.SeedPause(false)
	registry.Prime(manage.Channel{BroadcasterID: testBroadcaster, Enabled: true, IsMod: true})
	w := pipelineWorker(t, transport, func(c *Config) {
		c.Registry = registry
		c.Runtime = &fakeRuntime{servingSnapshot()}
		c.Owner = "test"
		c.Conduit = conduit.New(nil, "ingress.conduit", "conduit", time.Minute, zap.NewNop())
	})
	return w, srv
}

func countingTransport(calls *atomic.Int32) subscriptionTransport {
	return func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return subscriptionResponse(202, `{}`), nil
	}
}

func processEventSubJob(w *Worker, fence string) error {
	body := `{"type":"eventsub","broadcaster_id":"` + testBroadcaster + `"` + fence + `,"payload":{"mode":"enable"}}`
	return w.Process(bus.NewMessage("test-message", []byte(body)))
}

func TestEventSubJobWithoutFenceIsStampedAndEnrolls(t *testing.T) {
	var calls atomic.Int32
	transport := countingTransport(&calls)
	w, srv := eventSubIdentityWorker(t, transport)

	require.NoError(t, processEventSubJob(w, ""))

	assert.NotZero(t, calls.Load(), "an unfenced job must reach Twitch once stamped from the runtime snapshot")
	var state string
	srv.With(func(s *valkeytest.Server) { state = s.Hashes["outgress:channel:"+testBroadcaster]["sub_state"] })
	assert.Equal(t, subStateOK, state)
}

func TestEventSubJobWithStaleFenceIsDropped(t *testing.T) {
	for name, fence := range map[string]string{
		"stale incarnation":   `,"account_created_at":122,"admission_generation":2`,
		"stale generation":    `,"account_created_at":123,"admission_generation":1`,
		"partial incarnation": `,"account_created_at":123`,
		"partial generation":  `,"admission_generation":2`,
	} {
		t.Run(name, func(t *testing.T) {
			var calls atomic.Int32
			transport := countingTransport(&calls)
			w, _ := eventSubIdentityWorker(t, transport)

			require.NoError(t, processEventSubJob(w, fence))

			assert.Zero(t, calls.Load())
		})
	}
}
