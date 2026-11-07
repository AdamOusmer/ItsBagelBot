// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/db/modules/repository"
	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/internal/valkeytest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// A named window is cooldown_add's one source of truth for its whole life: the mover must never hand it to Valkey,
// even when it is due within the move window, or an extend afterward would be writing to a row nobody reads any more.
func TestCooldownMoverSkipsNamedRowsButStillMovesPlainOnes(t *testing.T) {
	vc := valkeytest.Real(t)
	repo, raw := sqliteStateRepo(t)
	mover := repository.NewCooldownMover(repo, vc, "mover-test", zap.NewNop())
	ctx := context.Background()

	namedKey := automation.LongCooldownNamedKey(cooldownChannel, "subathon")
	_, won := claimCooldowns(t, repo, window(namedKey))
	require.True(t, won)
	_, won = claimCooldowns(t, repo, window("plain"))
	require.True(t, won)

	due := time.Now().Add(90 * time.Minute).UnixMilli()
	_, err := raw.Exec("UPDATE cooldown_claims SET expires_at_ms = ? WHERE cooldown_key IN (?, ?)", due, namedKey, "plain")
	require.NoError(t, err)

	require.NoError(t, mover.MoveDue(ctx))

	rows := cooldownRows(t, raw)
	assert.Equal(t, []string{namedKey}, rows, "the named row stays; cooldown_add has nothing else to extend")

	markerKey := automation.LongCooldownMarkerKey(cooldownChannel, "plain")
	ttl, err := vc.Do(ctx, vc.B().Pttl().Key(markerKey).Build()).AsInt64()
	require.NoError(t, err)
	assert.Positive(t, ttl, "the plain row moved into its marker")

	namedMarker := automation.LongCooldownMarkerKey(cooldownChannel, namedKey)
	exists, err := vc.Do(ctx, vc.B().Exists().Key(namedMarker).Build()).AsInt64()
	require.NoError(t, err)
	assert.Zero(t, exists, "the named window never gets a marker from the mover")
}
