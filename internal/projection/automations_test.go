// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package projection

import (
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/bus/bustest"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func ownerResponder(t *testing.T, nc *nats.Conn, subject, reply string) *atomic.Int32 {
	t.Helper()
	var calls atomic.Int32
	sub, err := nc.Subscribe(subject, func(m *nats.Msg) {
		calls.Add(1)
		_ = m.Respond([]byte(reply))
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = sub.Unsubscribe() })
	return &calls
}

func automationsClient(t *testing.T) (*Client, *Store, *nats.Conn) {
	t.Helper()
	nc := bustest.NATS(t)
	store, _ := newTestStore(t)
	c := NewClient(Config{Store: store, NC: nc, TTL: time.Minute, Log: zap.NewNop(), Subjects: Subjects{
		Automations: "test.automations",
	}})
	t.Cleanup(c.Close)
	return c, store, nc
}

func TestAutomationsReadProjectionBeforeOwner(t *testing.T) {
	c, store, nc := automationsClient(t)
	calls := ownerResponder(t, nc, "test.automations", `{"automations":[{"id":"cold","name":"cold"}]}`)
	ctx := t.Context()

	cold, err := c.Automations(ctx, 5)
	require.NoError(t, err)
	require.Len(t, cold, 1)
	assert.Equal(t, "cold", cold[0].ID, "an unprojected channel reads the owner")

	require.NoError(t, store.SetAutomations(ctx, 6, []automation.Automation{{ID: "warm", Name: "warm"}}))
	require.NoError(t, store.ApplyAutomation(ctx, data.AutomationChangedDTO{UserID: 6, ID: "late", Automation: &automation.Automation{ID: "late"}}))
	warm, err := c.Automations(ctx, 6)
	require.NoError(t, err)
	assert.Len(t, warm, 2)
	assert.Equal(t, int32(1), calls.Load(), "a projected channel never reaches the owner")
}

func TestAutomationIndexRecompilesAfterInvalidation(t *testing.T) {
	store, _ := newTestStore(t)
	c, evict := invalidatedClient(t, store)
	ctx := t.Context()
	revision := func() int64 {
		idx, err := c.AutomationIndex(ctx, 8)
		require.NoError(t, err)
		return idx.Plan("a").Automation.Revision
	}

	require.NoError(t, store.SetAutomations(ctx, 8, []automation.Automation{{ID: "a", Name: "a", Enabled: true, Revision: 1}}))
	require.EqualValues(t, 1, revision())

	require.NoError(t, store.SetAutomations(ctx, 8, []automation.Automation{{ID: "a", Name: "a", Enabled: true, Revision: 2}}))
	assert.EqualValues(t, 1, revision(), "the compiled index is served until the invalidation arrives")

	evict("automations", 8)
	require.Eventually(t, func() bool { return revision() == 2 }, 2*time.Second, 5*time.Millisecond)
}
