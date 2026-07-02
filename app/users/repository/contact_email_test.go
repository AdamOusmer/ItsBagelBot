package repository_test

import (
	"context"
	"testing"

	"ItsBagelBot/app/users/ent/user"
	"ItsBagelBot/app/users/repository"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContactEmailRoundTrip(t *testing.T) {
	client, _, repo := setup(t)
	ctx := context.Background()

	require.NoError(t, repo.Register(ctx, 42, "streamer", "42@twitch.tv"))

	require.NoError(t, repo.SetContactEmail(ctx, 42, "real@example.com"))

	got, err := repo.ContactEmail(ctx, 42)
	require.NoError(t, err)
	assert.Equal(t, "real@example.com", got)

	// Ciphertext at rest, never the plaintext address.
	row := client.User.Query().Where(user.IDEQ(42)).OnlyX(ctx)
	assert.NotEmpty(t, row.EmailEnc)
	assert.NotContains(t, string(row.EmailEnc), "real@example.com")
}

func TestContactEmailAbsent(t *testing.T) {
	_, _, repo := setup(t)
	ctx := context.Background()

	require.NoError(t, repo.Register(ctx, 43, "fresh", "43@twitch.tv"))

	_, err := repo.ContactEmail(ctx, 43)
	assert.ErrorIs(t, err, repository.ErrNoContactEmail)
}

func TestContactEmailRejectsInvalid(t *testing.T) {
	_, _, repo := setup(t)
	ctx := context.Background()

	require.NoError(t, repo.Register(ctx, 44, "streamer2", "44@twitch.tv"))

	assert.Error(t, repo.SetContactEmail(ctx, 44, "Not An Email <spoof@example.com>"))
	assert.Error(t, repo.SetContactEmail(ctx, 44, ""))
}

func TestContactEmailEnvelopeBoundToUser(t *testing.T) {
	client, _, repo := setup(t)
	ctx := context.Background()

	require.NoError(t, repo.Register(ctx, 45, "owner", "45@twitch.tv"))
	require.NoError(t, repo.Register(ctx, 46, "other", "46@twitch.tv"))
	require.NoError(t, repo.SetContactEmail(ctx, 45, "bound@example.com"))

	// Copy user 45's envelope onto user 46: the AAD mismatch must fail the
	// unseal, so a swapped ciphertext can never leak another user's address.
	sealed := client.User.Query().Where(user.IDEQ(45)).OnlyX(ctx).EmailEnc
	client.User.UpdateOneID(46).SetEmailEnc(sealed).ExecX(ctx)

	_, err := repo.ContactEmail(ctx, 46)
	assert.Error(t, err)
}
