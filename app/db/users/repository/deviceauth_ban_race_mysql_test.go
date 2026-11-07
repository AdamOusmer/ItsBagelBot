// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/devicegrant"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const banRaceVerifier = "banrace0123456789abcdefghijklmnopqrstuvwxyz"

// A redemption already past its ban check must not leave a live grant behind
// the ban: the ban waits on the owner row, then its revoke sweep catches it.
func TestDeviceAuthMySQLBanDuringRedemptionRevokesNewGrant(t *testing.T) {
	client, repo := newMySQLDeviceHarness(t, "bagel_device_ban_race_")
	ctx := t.Context()
	require.NoError(t, repo.Register(ctx, 6101, "banrace", "BanRace", "banrace@example.invalid"))

	code, err := repo.CreateDeviceAuthorizationCode(ctx, 6101, codeRequest(banRaceVerifier))
	require.NoError(t, err)

	insertStarted := make(chan struct{})
	release := make(chan struct{})
	client.DeviceGrant.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			if m.Op().Is(ent.OpCreate) {
				close(insertStarted)
				<-release
			}
			return next.Mutate(ctx, m)
		})
	})

	exchanged := make(chan error, 1)
	go func() {
		_, err := repo.ExchangeDeviceCode(context.Background(), code, banRaceVerifier, testRedirect)
		exchanged <- err
	}()
	<-insertStarted

	banned := make(chan error, 1)
	go func() { banned <- repo.SetBanned(context.Background(), 6101, true) }()

	select {
	case err := <-banned:
		t.Fatalf("ban finished while a redemption held the owner row: %v", err)
	case <-time.After(500 * time.Millisecond):
	}
	close(release)

	require.NoError(t, <-exchanged)
	require.NoError(t, <-banned)

	live, err := client.DeviceGrant.Query().
		Where(devicegrant.UserIDEQ(6101), devicegrant.RevokedAtIsNil()).
		Count(ctx)
	require.NoError(t, err)
	assert.Zero(t, live, "a grant created during the ban must be revoked by it")
}
