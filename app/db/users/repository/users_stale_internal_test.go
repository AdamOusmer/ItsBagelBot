// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/enttest"
	"ItsBagelBot/pkg/bus/bustest"
	"ItsBagelBot/pkg/cache"

	"ItsBagelBot/internal/testdb"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.uber.org/zap"
)

func TestGetDoesNotServeStaleViewOfDeletedUser(t *testing.T) {
	client := enttest.Open(t, testdb.Driver, testdb.MemDSN("usersstale"))
	t.Cleanup(func() { _ = client.Close() })

	r := NewUsers(client, nil, bustest.NewPublisher(), nil, zap.NewNop())
	t.Cleanup(func() { r.Close(context.Background()) })
	r.views.Close()
	r.views = cache.New[UserView](userCacheCapacity, 10*time.Millisecond, cache.StaleOnError(time.Minute))

	ctx := context.Background()
	require.NoError(t, r.Register(ctx, 1001, "mavey", "Mavey", "mavey@example.com"))
	_, err := r.Get(ctx, 1001)
	require.NoError(t, err)

	client.User.DeleteOneID(1001).ExecX(ctx)
	time.Sleep(30 * time.Millisecond)

	_, err = r.Get(ctx, 1001)
	assert.True(t, ent.IsNotFound(err), "deleted user must not be served from the stale window, got %v", err)
}
