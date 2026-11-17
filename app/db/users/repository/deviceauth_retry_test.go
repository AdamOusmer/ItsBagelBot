// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/repository"
	"ItsBagelBot/pkg/db"

	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeviceCodeRetryRefreshesExpiryAndTokenAnchors(t *testing.T) {
	for _, expires := range []bool{false, true} {
		t.Run(map[bool]string{false: "fresh TTL anchors", true: "expired during retry"}[expires], func(t *testing.T) {
			h := setupDeviceAuth(t)
			createTestUser(t, h, 5001)
			code, err := h.repo.CreateDeviceAuthorizationCode(t.Context(), 5001, codeRequest(testVerifier))
			require.NoError(t, err)
			retryAt := time.Now().UTC().Add(100 * time.Millisecond)
			if expires {
				retryAt = time.Now().UTC().Add(time.Second)
				require.NoError(t, h.client.AuthorizationCode.Update().SetExpiresAt(retryAt).Exec(t.Context()))
			}
			var attempts atomic.Int32
			h.client.DeviceGrant.Use(func(next ent.Mutator) ent.Mutator {
				return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
					if attempts.Add(1) == 1 {
						time.Sleep(time.Until(retryAt))
						return nil, &mysql.MySQLError{Number: db.MySQLDeadlock}
					}
					return next.Mutate(ctx, m)
				})
			})
			tokens, err := h.repo.ExchangeDeviceCode(t.Context(), code, testVerifier, testRedirect)
			if expires {
				assert.ErrorIs(t, err, repository.ErrDeviceInvalidGrant)
				assert.Equal(t, repository.DeviceTokens{}, tokens)
				assert.Zero(t, h.client.DeviceGrant.Query().CountX(t.Context()))
				return
			}
			require.NoError(t, err)
			grant := h.client.DeviceGrant.Query().OnlyX(t.Context())
			assert.False(t, grant.CreatedAt.Before(retryAt), "creation time must come from the successful attempt")
			assert.Equal(t, [2]time.Time{grant.CreatedAt.Add(60 * 24 * time.Hour), grant.CreatedAt.Add(180 * 24 * time.Hour)},
				[2]time.Time{grant.RefreshSlidingExpiresAt, grant.RefreshAbsoluteExpiresAt})
		})
	}
}
