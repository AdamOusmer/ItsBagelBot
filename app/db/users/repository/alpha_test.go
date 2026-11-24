// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"testing"

	"ItsBagelBot/app/db/users/repository"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetAlphaRequiresAdminAuditsAndPublishes(t *testing.T) {
	client, pub, repo := setup(t)
	ctx := t.Context()
	require.NoError(t, repo.Register(ctx, repository.Registration{ID: 10, Username: "target", DisplayName: "target", Email: "target@example.com"}))
	audit := staffService(t, repo, []usersrpc.AdminAcctView{{ID: 11, Role: "moderator", Active: true, Login: "mod"}, {ID: 12, Role: "admin", Active: true, Login: "admin"}})

	assert.Error(t, repo.SetAlpha(ctx, 10, true, 11))
	assert.False(t, client.User.GetX(ctx, 10).Alpha)

	require.NoError(t, repo.SetAlpha(ctx, 10, true, 12))
	assert.True(t, client.User.GetX(ctx, 10).Alpha)
	changed := decodeChanged(t, pub)
	assert.True(t, changed[len(changed)-1].Alpha)
	got := <-audit.audit
	assert.Equal(t, [2]string{"set_alpha", "enabled=true"}, [2]string{got.Action, got.Detail})

	require.NoError(t, repo.SetAlpha(ctx, 10, false, 12))
	changed = decodeChanged(t, pub)
	assert.False(t, changed[len(changed)-1].Alpha)
	view, err := repo.Get(ctx, 10)
	require.NoError(t, err)
	assert.False(t, view.Alpha)
}
