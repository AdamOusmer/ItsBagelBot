// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus/bustest"

	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

const failedRPCReply = `{"error":"request failed","code":"internal"}`

type refusedWrites struct {
	valkey.Client
	mu   sync.Mutex
	cmds []string
}

func (r *refusedWrites) Do(_ context.Context, cmd valkey.Completed) valkey.ValkeyResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cmds = append(r.cmds, strings.Join(cmd.Commands(), " "))
	return valkey.NewResult(valkey.ValkeyMessage{}, errors.New("write refused"))
}

func (r *refusedWrites) issued() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.cmds...)
}

func TestLiveOnlyGateRetriesAfterProjectorError(t *testing.T) {
	nc := bustest.NATS(t)
	served := bustest.Respond(t, nc, "test.live", failedRPCReply, `{"live":true,"known":true}`)
	live := NewValkeyLiveStore(newFakeValkey(t).client, nc, nil, LiveConfig{ProjectorLiveSubject: "test.live"})
	p := &Pipeline{live: live}
	c := &module.Context{BroadcasterID: 9}
	ctx := context.Background()

	ok, err := p.liveOK(ctx, c, true)
	require.Error(t, err)
	require.False(t, ok, "a failed live check blocks live-only commands for this message")

	ok, err = p.liveOK(ctx, c, true)
	require.NoError(t, err)
	require.True(t, ok, "the failed check must not be cached as offline")
	require.EqualValues(t, 2, served.Load())
}

func TestCounterBumpSkipsWhileScopeIsUnknown(t *testing.T) {
	nc := bustest.NATS(t)
	served := bustest.Respond(t, nc, "test.loyalty.counter.get", failedRPCReply, `{"found":true,"counter":{"name":"deaths","scope":"viewer"}}`)
	writes := &refusedWrites{Client: newFakeValkey(t).client}
	store := NewValkeyLoyaltyStore(writes, NewLoyaltyRPC(nc, "test.loyalty"), nil, zap.NewNop())
	bump := CounterBump{BroadcasterID: 5, Name: "deaths", Viewer: Viewer{ID: 77}, Delta: 1}
	ctx := context.Background()

	_, err := store.CounterBump(ctx, bump)
	require.Error(t, err)
	require.Empty(t, writes.issued(), "no counter key is bumped while the scope is unknown")

	_, _ = store.CounterBump(ctx, bump)
	require.NotEmpty(t, writes.issued())
	require.Contains(t, writes.issued()[0], loyalCounterViewerPrefix, "the retried lookup routes the bump to the viewer key")
	require.EqualValues(t, 2, served.Load())
}

func TestCustomCommandStaysSilentWhileLookupFails(t *testing.T) {
	nc := bustest.NATS(t)
	commands := bustest.Respond(t, nc, "test.commands", failedRPCReply,
		`{"commands":[{"name":"hi","response":"hello there","is_active":true,"perm":"everyone"}]}`)
	users := bustest.Respond(t, nc, "test.users", failedRPCReply)
	proj := projection.NewClient(projection.Config{
		Store:    projection.NewStore(newFakeValkey(t).client),
		NC:       nc,
		Subjects: projection.Subjects{Users: "test.users", Commands: "test.commands"},
		TTL:      time.Minute,
		Log:      zap.NewNop(),
	})
	t.Cleanup(proj.Close)
	pub := &fakePublisher{}
	p := newPipelineWith(pub, proj)

	require.NoError(t, p.Process(chatMsg(t, "standard", "!hi")))
	require.Empty(t, pub.snapshot(), "no reply while the command lookup fails")

	require.NoError(t, p.Process(chatMsg(t, "standard", "!hi")))
	got := pub.snapshot()
	require.Len(t, got, 1, "the failed lookup must not be cached as a missing command")
	require.Contains(t, string(got[0].msg.Payload), "hello there", "a failed user lookup still answers in the default locale")
	require.EqualValues(t, 2, commands.Load())
	require.Positive(t, users.Load())
}
