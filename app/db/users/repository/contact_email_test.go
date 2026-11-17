// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"testing"

	"ItsBagelBot/app/db/users/ent/user"
	"ItsBagelBot/app/db/users/repository"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContactEmailRoundTrip(t *testing.T) {
	client, _, repo := setup(t)
	ctx := context.Background()

	require.NoError(t, repo.Register(ctx, repository.Registration{ID: 42, Username: "streamer", DisplayName: "streamer", Email: "42@twitch.tv"}))

	require.NoError(t, repo.SetContactEmail(ctx, 42, "real@example.com"))

	got, err := repo.ContactEmail(ctx, 42)
	require.NoError(t, err)
	assert.Equal(t, "real@example.com", got)

	row := client.User.Query().Where(user.IDEQ(42)).OnlyX(ctx)
	assert.NotEmpty(t, row.EmailEnc)
	assert.NotContains(t, string(row.EmailEnc), "real@example.com")
}

func TestContactEmailAbsent(t *testing.T) {
	_, _, repo := setup(t)
	ctx := context.Background()

	require.NoError(t, repo.Register(ctx, repository.Registration{ID: 43, Username: "fresh", DisplayName: "fresh", Email: "43@twitch.tv"}))

	_, err := repo.ContactEmail(ctx, 43)
	assert.ErrorIs(t, err, repository.ErrNoContactEmail)
}

func TestContactEmailRejectsInvalid(t *testing.T) {
	_, _, repo := setup(t)
	ctx := context.Background()

	require.NoError(t, repo.Register(ctx, repository.Registration{ID: 44, Username: "streamer2", DisplayName: "streamer2", Email: "44@twitch.tv"}))

	assert.Error(t, repo.SetContactEmail(ctx, 44, "Not An Email <spoof@example.com>"))
	assert.Error(t, repo.SetContactEmail(ctx, 44, ""))
}

func TestContactEmailEnvelopeBoundToUser(t *testing.T) {
	client, _, repo := setup(t)
	ctx := context.Background()

	require.NoError(t, repo.Register(ctx, repository.Registration{ID: 45, Username: "owner", DisplayName: "owner", Email: "45@twitch.tv"}))
	require.NoError(t, repo.Register(ctx, repository.Registration{ID: 46, Username: "other", DisplayName: "other", Email: "46@twitch.tv"}))
	require.NoError(t, repo.SetContactEmail(ctx, 45, "bound@example.com"))

	sealed := client.User.Query().Where(user.IDEQ(45)).OnlyX(ctx).EmailEnc
	client.User.UpdateOneID(46).SetEmailEnc(sealed).ExecX(ctx)

	_, err := repo.ContactEmail(ctx, 46)
	assert.Error(t, err)
}
