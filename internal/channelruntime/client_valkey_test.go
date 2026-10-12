package channelruntime

import (
	"ItsBagelBot/internal/domain/lifecycle"
	contract "ItsBagelBot/internal/domain/rpc/projection"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/internal/watchtime"
	"ItsBagelBot/pkg/codec"
	"context"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
	"os"
	"strconv"
	"testing"
	"time"
)

func TestOwnerHydrationInFlightCannotReturnPreBanAdmission(t *testing.T) {
	addr := os.Getenv("VALKEY_TEST_ADDR")
	if addr == "" {
		t.Skip("VALKEY_TEST_ADDR is not set")
	}
	vk, err := valkey.NewClient(valkey.ClientOption{InitAddress: []string{addr}, Password: os.Getenv("VALKEY_TEST_PASSWORD")})
	require.NoError(t, err)
	t.Cleanup(vk.Close)
	ctx := context.Background()
	id := uint64(time.Now().UnixNano())
	store := projection.NewStore(vk)
	t.Cleanup(func() {
		vk.Do(ctx, vk.B().Del().Key("settings:"+strconv.FormatUint(id, 10), watchtime.AdmissionKey(id)).Build())
	})
	require.NoError(t, store.SetUser(ctx, id, projection.UserProjection{AccountCreatedAt: 101, StateRevision: 1, Status: "paid", IsActive: true}))
	srv, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true})
	require.NoError(t, err)
	srv.Start()
	require.True(t, srv.ReadyForConnections(5*time.Second))
	t.Cleanup(srv.Shutdown)
	nc, err := nats.Connect(srv.ClientURL())
	require.NoError(t, err)
	t.Cleanup(nc.Close)
	old := lifecycle.State{Version: 1, DesiredEnabled: true, RuntimeState: lifecycle.Active, ChatRestriction: lifecycle.RestrictionNone, GrantHealth: lifecycle.GrantHealthy, AdmissionGeneration: 1}
	observed := make(chan struct{})
	release := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	_, err = nc.Subscribe("test.runtime.owner", func(msg *nats.Msg) {
		close(observed)
		<-release
		body, e := codec.Marshal(contract.UserReply{UserID: strconv.FormatUint(id, 10), AccountCreatedAt: 101, StateRevision: 1, Status: "paid", IsActive: true, Lifecycle: &old})
		if e == nil {
			_ = msg.Respond(body)
		}
	})
	require.NoError(t, err)
	require.NoError(t, nc.Flush())
	client := NewClient(store, nc, "test.runtime.owner")
	type result struct {
		s   Snapshot
		err error
	}
	done := make(chan result, 1)
	go func() { s, e := client.Get(ctx, id); done <- result{s, e} }()
	select {
	case <-observed:
	case <-time.After(3 * time.Second):
		t.Fatal("owner fallback was not requested")
	}
	banned := old
	banned.RuntimeState = lifecycle.Blocked
	banned.ChatRestriction = lifecycle.RestrictionBanned
	banned.AdmissionGeneration = 2
	require.NoError(t, store.SetLifecycle(ctx, id, banned, 101, 2))
	close(release)
	select {
	case got := <-done:
		require.NoError(t, got.err)
		require.Equal(t, banned, got.s.State)
		require.False(t, got.s.AllowsRuntime())
		require.False(t, got.s.AllowsBackground())
	case <-time.After(3 * time.Second):
		t.Fatal("owner fallback did not finish")
	}
}

func TestEngageRetriesRacedDormancyOnlyOnce(t *testing.T) {
	addr := os.Getenv("VALKEY_TEST_ADDR")
	if addr == "" {
		t.Skip("VALKEY_TEST_ADDR is not set")
	}
	for _, terminal := range []string{lifecycle.Dormant, lifecycle.Blocked, lifecycle.Disabled} {
		t.Run(terminal, func(t *testing.T) {
			vk, err := valkey.NewClient(valkey.ClientOption{InitAddress: []string{addr}, Password: os.Getenv("VALKEY_TEST_PASSWORD")})
			require.NoError(t, err)
			t.Cleanup(vk.Close)
			store := projection.NewStore(vk)
			ctx := t.Context()
			id := uint64(time.Now().UnixNano())
			t.Cleanup(func() {
				vk.Do(context.Background(), vk.B().Del().Key("settings:"+strconv.FormatUint(id, 10), watchtime.AdmissionKey(id)).Build())
			})
			old := lifecycle.State{Version: 1, DesiredEnabled: true, RuntimeState: lifecycle.Active, AdmissionGeneration: 1, ChatRestriction: lifecycle.RestrictionNone, GrantHealth: lifecycle.GrantHealthy, LastEngagedAt: time.Now().Add(-31 * 24 * time.Hour)}
			require.NoError(t, store.SetUser(ctx, id, projection.UserProjection{AccountCreatedAt: 101, StateRevision: 1, Status: "free", IsActive: true, Lifecycle: &old}))
			srv, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true})
			require.NoError(t, err)
			srv.Start()
			require.True(t, srv.ReadyForConnections(5*time.Second))
			t.Cleanup(srv.Shutdown)
			nc, err := nats.Connect(srv.ClientURL())
			require.NoError(t, err)
			t.Cleanup(nc.Close)
			calls := make(chan usersrpc.LifecycleRequest, 3)
			n := 0
			_, err = nc.Subscribe(usersrpc.LifecycleSubject, func(msg *nats.Msg) {
				var req usersrpc.LifecycleRequest
				if codec.Unmarshal(msg.Data, &req) != nil {
					return
				}
				n++
				calls <- req
				next := old
				next.RuntimeState = terminal
				next.AdmissionGeneration = 2
				applied := false
				if terminal == lifecycle.Blocked {
					next.ChatRestriction = lifecycle.RestrictionBanned
				}
				if terminal == lifecycle.Disabled {
					next.DesiredEnabled = false
				}
				if n == 2 {
					next.RuntimeState = lifecycle.Active
					next.AdmissionGeneration = 3
					applied = true
				}
				body, _ := codec.Marshal(usersrpc.LifecycleReply{State: next, AccountCreatedAt: 101, StateRevision: int64(n + 1), Applied: applied})
				_ = msg.Respond(body)
			})
			require.NoError(t, err)
			require.NoError(t, nc.Flush())
			client := NewClient(store, nc, "")
			got, err := client.Engage(ctx, id, Snapshot{Registered: true, AccountCreatedAt: 101, State: old}, "viewer", time.Now())
			require.NoError(t, err)
			require.Equal(t, int64(1), (<-calls).AdmissionGeneration)
			if terminal == lifecycle.Dormant {
				require.Equal(t, lifecycle.Active, got.State.RuntimeState)
				require.Equal(t, int64(2), (<-calls).AdmissionGeneration)
			} else {
				require.False(t, got.AllowsRuntime())
			}
			require.Empty(t, calls, "ban or disable must never generate a wake retry; dormancy is bounded to one retry")
		})
	}
}
