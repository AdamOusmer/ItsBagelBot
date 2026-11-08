// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ItsBagelBot/app/projector/rpc"
	"ItsBagelBot/internal/domain/event/data"
	projectorrpc "ItsBagelBot/internal/domain/rpc/projector"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/health"
	"ItsBagelBot/pkg/svcboot"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type healthSubscriber struct {
	bus.Subscriber
	ok bool
}

func (s healthSubscriber) Healthy() bool { return s.ok }

func projectorHealth(t *testing.T, nc *nats.Conn, consumerOK bool) *health.Set {
	t.Helper()
	return svcboot.NewHealthSet(svcboot.Health{
		Log: zap.NewNop(), NC: nc, Service: serviceName, QueueGroup: queueGroup,
	}, tierChecks(nc, healthSubscriber{ok: consumerOK})...)
}

func ownerHealth(t *testing.T, nc *nats.Conn, failingOwner, mode string) {
	t.Helper()
	for _, owner := range dataTierServices {
		if owner == failingOwner && mode == "absent" {
			continue
		}
		set := health.NewSet(owner)
		if owner == failingOwner {
			set.Add(health.Check{Name: "mysql", Probe: func(context.Context) error { return errors.New("database unavailable") }})
		}
		_, err := bus.SubscribeRPCHealth(nc, owner, owner+"-health", set)
		require.NoError(t, err)
	}
}

func healthGET(set *health.Set, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	set.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestProjectorOwnerOutageDegradesWithoutRemovingReadiness(t *testing.T) {
	for _, owner := range dataTierServices {
		for _, mode := range []string{"absent", "down"} {
			t.Run(owner+"/"+mode, func(t *testing.T) {
				nc := testnats.Connect(t)
				ownerHealth(t, nc, owner, mode)
				set := projectorHealth(t, nc, true)

				require.Equal(t, http.StatusOK, healthGET(set, "/healthz").Code)
				require.Equal(t, http.StatusOK, healthGET(set, "/readyz").Code)
				status := healthGET(set, "/status")
				require.Equal(t, http.StatusMultiStatus, status.Code)
				var report health.Report
				require.NoError(t, codec.Unmarshal(status.Body.Bytes(), &report))
				require.Equal(t, health.StatusDegraded, report.Status)
				for _, check := range report.Checks {
					require.Equal(t, check.Name != owner, check.OK, "check %s: %s", check.Name, check.Error)
					if check.Name == owner {
						require.True(t, check.Optional)
						require.NotEmpty(t, check.Error)
					}
				}
				ctx, cancel := context.WithTimeout(t.Context(), time.Second)
				defer cancel()
				require.ErrorIs(t, bus.HealthProbe(nc, serviceName).Probe(ctx), health.ErrDegraded)
			})
		}
	}
}

func TestProjectorReadinessFitsProbeBudgetWithSilentOwners(t *testing.T) {
	for _, tc := range []struct {
		name string
		all  bool
	}{{name: "one owner"}, {name: "all owners", all: true}} {
		t.Run(tc.name, func(t *testing.T) {
			nc := testnats.Connect(t)
			for i, owner := range dataTierServices {
				if i == 0 || tc.all {
					_, err := nc.Subscribe(bus.RPCHealthSubject(owner), func(*nats.Msg) {})
					require.NoError(t, err)
					continue
				}
				_, err := bus.SubscribeRPCHealth(nc, owner, owner+"-health", health.NewSet(owner))
				require.NoError(t, err)
			}
			require.NoError(t, nc.Flush())
			set := projectorHealth(t, nc, true)
			server := httptest.NewTLSServer(set.Handler())
			defer server.Close()
			client := server.Client()
			// Match Kubernetes' default probe deadline, including the HTTP/TLS round trip.
			client.Timeout = time.Second
			response, err := client.Get(server.URL + "/readyz")
			require.NoError(t, err, "optional owner probes must run concurrently within the readiness deadline")
			defer response.Body.Close()
			require.Equal(t, http.StatusOK, response.StatusCode)

			report := set.Snapshot(t.Context())
			require.Equal(t, health.StatusDegraded, report.Status)
			for _, check := range report.Checks {
				if check.Name == dataTierServices[0] {
					require.False(t, check.OK, "the silent owner remains visible as unhealthy")
					require.True(t, check.Optional)
					require.Contains(t, check.Error, "deadline exceeded")
				}
			}
		})
	}
}

func TestProjectorEssentialFailuresStillRemoveReadiness(t *testing.T) {
	for _, failure := range []string{"nats", "lane_stream"} {
		t.Run(failure, func(t *testing.T) {
			nc := testnats.Connect(t)
			ownerHealth(t, nc, "", "")
			set := projectorHealth(t, nc, failure != "lane_stream")
			if failure == "nats" {
				nc.Close()
			}
			require.Equal(t, http.StatusOK, healthGET(set, "/healthz").Code)
			require.Equal(t, http.StatusServiceUnavailable, healthGET(set, "/readyz").Code)
			require.Equal(t, health.StatusDown, set.Snapshot(t.Context()).Status)
			if failure == "lane_stream" {
				ctx, cancel := context.WithTimeout(t.Context(), time.Second)
				defer cancel()
				err := bus.HealthProbe(nc, serviceName).Probe(ctx)
				require.ErrorContains(t, err, "lane_stream")
				require.NotErrorIs(t, err, health.ErrDegraded)
			}
		})
	}
}

func TestProjectorKeepsServingAndFoldingWithUsersUnavailable(t *testing.T) {
	nc := testnats.Connect(t)
	ownerHealth(t, nc, "users", "absent")
	set := projectorHealth(t, nc, true)
	store := projection.NewStore(testValkey(t))
	require.NoError(t, store.SetUser(t.Context(), 7, projection.UserProjection{Status: "premium", IsActive: true}))
	require.NoError(t, rpc.SubscribeStatus(nc, store, "test.projector.status", "test.absent.users", "", queueGroup, nil, zap.NewNop()))
	require.NoError(t, nc.Flush())
	require.Equal(t, http.StatusOK, healthGET(set, "/readyz").Code)

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	reply, err := bus.RequestJSON[projectorrpc.StatusReply](ctx, nc, "test.projector.status", projectorrpc.StatusRequest{BroadcasterID: "7"})
	require.NoError(t, err)
	require.Empty(t, reply.Error)
	require.Equal(t, "premium", reply.Tier, "warm projections remain readable without their owner")

	p := NewProjector(Deps{Store: store, Log: zap.NewNop()})
	require.NoError(t, p.HandleCommandChanged(message(t, "command-during-users-outage", data.CommandChangedDTO{
		UserID: 7, Name: "lurk", Response: "hello", Perm: "everyone", IsActive: true,
	})))
	command, found, _, err := store.GetCommand(t.Context(), 7, "lurk")
	require.NoError(t, err)
	require.True(t, found, "another owner's events still update projections")
	require.Equal(t, "hello", command.Response)
}
