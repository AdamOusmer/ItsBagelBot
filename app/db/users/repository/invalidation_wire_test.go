// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"testing"
	"time"

	"ItsBagelBot/app/db/dbtest"
	"ItsBagelBot/app/db/users/repository"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/invalidate"
	billingrpc "ItsBagelBot/internal/domain/rpc/billing"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/bus"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestUserMutationsIgnoreInvalidationWireErrors(t *testing.T) {
	client, pub, _ := setup(t)
	nc := testnats.Connect(t)
	h := bus.NewInvalidations(nc, "test.invalidate", nil)
	repo := repository.NewUsers(repository.UsersConfig{Client: client, Packer: dbtest.NewPacker(t), Pub: h.Publisher(pub), Log: zap.NewNop()})
	t.Cleanup(func() { repo.Close(t.Context()) })
	nc.Close()
	require.ErrorIs(t, h.Publish(invalidate.ScopeStatus, 1001), nats.ErrConnectionClosed)
	require.NoError(t, repo.Register(t.Context(), repository.Registration{ID: 1001, Username: "before", Email: "before@example.com"}))
	applied, err := repo.ApplyBilling(t.Context(), billingrpc.ApplyRequest{UserID: 1001, EventID: "activate", OccurredAt: time.Now(), Action: billingrpc.ActionActivate})
	require.NoError(t, err)
	require.True(t, applied)
	prior, err := repo.SyncIdentity(t.Context(), 1001, "after", "After")
	require.NoError(t, err)
	require.Equal(t, "before", prior)
	require.GreaterOrEqual(t, len(pub.On(data.SubjectUserChanged)), 3)
}
