// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"testing"

	"ItsBagelBot/app/db/users/ent/user"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/bus/bustest"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// decodeChanged unwraps every UserChanged payload recorded on the fake bus.
func decodeChanged(t *testing.T, pub *bustest.Publisher) []data.UserChangedDTO {
	t.Helper()

	msgs := pub.On(data.SubjectUserChanged)
	out := make([]data.UserChangedDTO, 0, len(msgs))
	for _, m := range msgs {
		var dto data.UserChangedDTO
		require.NoError(t, codec.Unmarshal(m.Payload, &dto))
		out = append(out, dto)
	}
	return out
}

// Preference setters must be write-behind: accepted immediately, nothing on
// the bus and nothing in the database until the flush window drains — that is
// the entire point of routing them through the batcher.
func TestPreferenceWritesAreWriteBehind(t *testing.T) {
	client, pub, repo := setup(t)
	ctx := context.Background()

	require.NoError(t, repo.Register(ctx, 1001, "Mavey", "mavey@concordia.ca"))
	baseline := len(decodeChanged(t, pub)) // Register's own announcement

	// Flip AWAY from the schema defaults (active=true, locale=en) so a
	// premature write is distinguishable from an untouched row.
	require.NoError(t, repo.SetActive(ctx, 1001, false))
	require.NoError(t, repo.SetLocale(ctx, 1001, "fr"))

	assert.Len(t, decodeChanged(t, pub), baseline, "no event may precede the flush commit")

	row := client.User.Query().OnlyX(ctx)
	assert.True(t, row.IsActive, "no row write may precede the flush commit")
	assert.Equal(t, "en", row.Locale)

	// Close performs the final drain (the shutdown path).
	repo.Close(context.Background())

	row = client.User.Query().OnlyX(ctx)
	assert.False(t, row.IsActive)
	assert.Equal(t, "fr", row.Locale)

	events := decodeChanged(t, pub)
	require.Len(t, events, baseline+1, "the window announces exactly once")
	last := events[len(events)-1]
	assert.Equal(t, uint64(1001), last.UserID)
	assert.False(t, last.IsActive)
	assert.Equal(t, "fr", last.Locale)

	// The view cache must have been dropped at flush, not served stale.
	view, err := repo.Get(ctx, 1001)
	require.NoError(t, err)
	assert.False(t, view.IsActive)
	assert.Equal(t, "fr", view.Locale)
}

// A user touching several preferences inside one window gets ONE update and
// ONE announcement: the window merges per user at flush, and the latest
// queued value for a repeated preference wins.
func TestPreferenceWindowMergesPerUser(t *testing.T) {
	_, pub, repo := setup(t)
	ctx := context.Background()

	require.NoError(t, repo.Register(ctx, 1001, "Mavey", "mavey@concordia.ca"))
	repo.Close(context.Background()) // settle Register's synchronous announcement
	baseline := len(decodeChanged(t, pub))

	require.NoError(t, repo.SetActive(ctx, 1001, false))
	require.NoError(t, repo.SetLocale(ctx, 1001, "de"))
	require.NoError(t, repo.SetCustomCursor(ctx, 1001, true))
	require.NoError(t, repo.SetOnboarded(ctx, 1001, true))
	require.NoError(t, repo.SetCreatorCode(ctx, 1001, " bagel10 "))
	require.NoError(t, repo.SetCreatorCode(ctx, 1001, "bagel20")) // coalesces over the first

	repo.Close(context.Background())

	view, err := repo.Get(ctx, 1001)
	require.NoError(t, err)
	assert.False(t, view.IsActive)
	assert.Equal(t, "de", view.Locale)
	assert.True(t, view.CustomCursor)
	assert.True(t, view.Onboarded)
	require.NotNil(t, view.CreatorCode)
	assert.Equal(t, "bagel20", *view.CreatorCode, "the latest queued value must win")

	events := decodeChanged(t, pub)
	require.Len(t, events, baseline+1, "one user in one window is announced once")
	assert.Equal(t, uint64(1001), events[len(events)-1].UserID)
}

// Money and moderation stay outside the batcher by ADR-0008: SetStatus and
// SetBanned must land and announce synchronously, with no flush involved.
func TestMoneyAndModerationWritesStayImmediate(t *testing.T) {
	client, pub, repo := setup(t)
	ctx := context.Background()

	require.NoError(t, repo.Register(ctx, 1001, "Mavey", "mavey@concordia.ca"))

	require.NoError(t, repo.SetStatus(ctx, 1001, user.StatusPaid))
	require.NoError(t, repo.SetBanned(ctx, 1001, true))

	row := client.User.Query().OnlyX(ctx)
	assert.Equal(t, user.StatusPaid, row.Status)
	assert.True(t, row.Banned, "a ban may never wait for a flush window")

	events := decodeChanged(t, pub)
	require.Len(t, events, 3) // register + status + ban
	assert.Equal(t, string(user.StatusPaid), events[1].Status)
	assert.True(t, events[2].Banned)
}
