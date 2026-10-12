package worker

import (
	"ItsBagelBot/internal/channelruntime"
	"ItsBagelBot/internal/domain/lifecycle"
	"ItsBagelBot/internal/domain/outgress"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
	"context"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

type fakeRuntime struct{ snap channelruntime.Snapshot }

func (r *fakeRuntime) Get(context.Context, uint64) (channelruntime.Snapshot, error) {
	return r.snap, nil
}
func (r *fakeRuntime) Apply(context.Context, uint64, usersrpc.LifecycleReply) error { return nil }
func servingSnapshot() channelruntime.Snapshot {
	return channelruntime.Snapshot{Registered: true, AccountCreatedAt: 123, State: lifecycle.State{Version: 1, DesiredEnabled: true, RuntimeState: lifecycle.Active, GrantHealth: lifecycle.GrantHealthy, ChatRestriction: lifecycle.RestrictionNone, AdmissionGeneration: 2}}
}
func TestLifecycleRejectsQueuedAndBatchGeneration(t *testing.T) {
	r := &fakeRuntime{servingSnapshot()}
	w := &Worker{runtime: r}
	for _, p := range []*outgress.Message{
		{BroadcasterID: "42"},
		{BroadcasterID: "42", AccountCreatedAt: 122, AdmissionGeneration: 2},
		{BroadcasterID: "42", AccountCreatedAt: 123, AdmissionGeneration: 1},
	} {
		ok, err := w.admitPayload(context.Background(), p)
		require.NoError(t, err)
		require.False(t, ok)
	}
	p := &outgress.Message{BroadcasterID: "42", AccountCreatedAt: 123, AdmissionGeneration: 2}
	ok, err := w.admitPayload(context.Background(), p)
	require.NoError(t, err)
	require.True(t, ok)
	r.snap.State.ChatRestriction = lifecycle.RestrictionBanned
	ok, err = w.admitPayload(context.Background(), p)
	require.NoError(t, err)
	require.False(t, ok)
}
func TestLifecycleSuppressesInactiveBackground(t *testing.T) {
	r := &fakeRuntime{servingSnapshot()}
	w := &Worker{runtime: r}
	for _, state := range []string{lifecycle.Dormant, lifecycle.Blocked, lifecycle.Disabled, lifecycle.Recovering, lifecycle.Degraded} {
		r.snap.State.RuntimeState = state
		require.False(t, w.backgroundAllowed(context.Background(), "42"))
		w.warmBroadcasterToken(context.Background(), "42")
		require.NoError(t, w.processStreamStatus(context.Background(), &outgress.Message{BroadcasterID: "42"}))
	}
}
func TestModerationOtherViewersCauseNoWork(t *testing.T) {
	w := &Worker{botID: "bot"}
	body, err := codec.Marshal(botModeration{Type: "channel.ban", BroadcasterID: "42", UserID: "viewer"})
	require.NoError(t, err)
	require.NoError(t, w.HandleBotModeration(&bus.Message{Payload: body}))
}
func TestDelayedModerationCannotAttachToReplacementAccount(t *testing.T) {
	current := usersrpc.LifecycleReply{AccountCreatedAt: 200, State: lifecycle.State{OwnedSubscriptions: []string{"replacement-ban", "replacement-unban"}}}
	require.False(t, moderationMatchesIncarnation(botModeration{SubscriptionID: "old-ban", ObservedAt: time.UnixMicro(300)}, current))
	require.False(t, moderationMatchesIncarnation(botModeration{SubscriptionID: "replacement-ban", ObservedAt: time.UnixMicro(100)}, current))
	require.True(t, moderationMatchesIncarnation(botModeration{SubscriptionID: "replacement-ban", ObservedAt: time.UnixMicro(300)}, current))
}
