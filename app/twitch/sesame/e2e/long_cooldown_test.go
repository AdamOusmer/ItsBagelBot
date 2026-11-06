// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package e2e

import (
	"context"
	"database/sql"
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/internal/domain/event/lane"
	automationrpc "ItsBagelBot/internal/domain/rpc/automation"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/internal/valkeytest"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const (
	cooldownRecipe = "!cooldown"
	twoDaysSeconds = 2 * 86_400
	oneHourSeconds = 3600
	cooledReason   = "condition:0:cooldown"
)

type cooldownRig struct {
	*stack
	pool    *sql.DB
	channel uint64
	runsNow int
}

func startCooldownRig(t *testing.T, target loyaltyDB) *cooldownRig {
	t.Helper()
	vc := valkeytest.Real(t)
	natsURL := startNATS(t)
	nc, err := nats.Connect(natsURL)
	require.NoError(t, err)
	t.Cleanup(nc.Close)
	ensureOutgressStreams(t, natsURL)
	serveLoyalty(t, nc, target)
	s := &stack{
		vc: vc, nc: nc, natsURL: natsURL,
		vars:     engine.NewLoyaltyRPC(nc, engine.LoyaltyRPCPrefix(loyaltyPrefix)),
		runs:     engine.NewValkeyRunLog(vc, zap.NewNop()),
		source:   &fixtureSource{autos: map[uint64][]automation.Automation{}},
		cooldown: engine.NewValkeyCooldown(vc),
	}
	s.pub, err = bus.NewPublisher(natsURL, zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.pub.Close() })
	for i := range s.replicas {
		s.replicas[i] = s.replica(t)
	}
	return &cooldownRig{stack: s, pool: target.Pool}
}

func (r *cooldownRig) forChannel(t *testing.T, channel uint64, params step) {
	t.Helper()
	r.channel, r.runsNow = channel, 0
	recipe := recipeWith(t, channel, cooldownRecipe, chatStep("ran"))
	raw, err := codec.Marshal(params)
	require.NoError(t, err)
	recipe.Definition.Conditions = []automation.Condition{{Type: "cooldown", Params: raw}}
	require.Empty(t, automation.Validate(recipe.Definition))
	r.source.set(channel, recipe)
}

// Replicas alternate so each claim crosses NATS from a different pipeline.
func (r *cooldownRig) run(t *testing.T, viewer uint64) automationrpc.RunRecord {
	t.Helper()
	id := strconv.FormatUint(viewer, 10)
	r.process(t, r.runsNow%len(r.replicas), lane.Envelope{
		Type: "channel.chat.message", Lane: "premium", EventID: r.nextEvent("cooldown"), Text: cooldownRecipe,
		BroadcasterUserID: strconv.FormatUint(r.channel, 10), BroadcasterUserLogin: "streamer",
		ChatterUserID: id, ChatterUserLogin: "viewer" + id,
	})
	r.runsNow++
	var latest automationrpc.RunRecord
	require.Eventually(t, func() bool {
		runs, err := r.runs.Recent(context.Background(), r.channel, "", automation.RunsKept)
		if err != nil || len(runs) < r.runsNow {
			return false
		}
		latest = runs[0]
		return true
	}, settle, 20*time.Millisecond)
	return latest
}

func (r *cooldownRig) assertPasses(t *testing.T, viewer uint64) {
	t.Helper()
	assert.Equal(t, automationrpc.OutcomeOK, r.run(t, viewer).Outcome, "viewer %d", viewer)
}

func (r *cooldownRig) assertRefused(t *testing.T, viewer uint64) {
	t.Helper()
	rec := r.run(t, viewer)
	assert.Equal(t, automationrpc.OutcomeSkipped, rec.Outcome, "viewer %d", viewer)
	assert.Equal(t, cooledReason, rec.Reason, "viewer %d", viewer)
}

func (r *cooldownRig) storedWindows(t *testing.T) []int64 {
	t.Helper()
	rows, err := r.pool.Query("SELECT expires_at_ms FROM cooldown_claims WHERE user_id = ? ORDER BY cooldown_key", r.channel)
	require.NoError(t, err)
	defer rows.Close()
	expiries := []int64{}
	for rows.Next() {
		var expiry int64
		require.NoError(t, rows.Scan(&expiry))
		expiries = append(expiries, expiry)
	}
	require.NoError(t, rows.Err())
	return expiries
}

func (r *cooldownRig) viewerKey(viewer uint64) string {
	return "cooldown:autouser:" + strconv.FormatUint(r.channel, 10) + ":" + strconv.FormatUint(viewer, 10) + ":" + cooldownRecipe
}

func (r *cooldownRig) viewerKeyExists(t *testing.T, viewer uint64) bool {
	t.Helper()
	count, err := r.vc.Do(context.Background(), r.vc.B().Exists().Key(r.viewerKey(viewer)).Build()).AsInt64()
	require.NoError(t, err)
	return count == 1
}

func exerciseLongCooldowns(t *testing.T, r *cooldownRig) {
	const alice, bob, carol = uint64(101), uint64(102), uint64(103)

	t.Run("a 2-day viewer window refuses that viewer only", func(t *testing.T) {
		r.forChannel(t, uniqueChannel(21), step{"user_s": twoDaysSeconds})
		r.assertPasses(t, alice)
		r.assertRefused(t, alice)
		r.assertPasses(t, bob)

		stored := r.storedWindows(t)
		require.Len(t, stored, 2, "both windows live in SQL")
		assert.InDelta(t, time.Now().Add(2*24*time.Hour).UnixMilli(), stored[0], 60_000)
		assert.False(t, r.viewerKeyExists(t, alice), "a long window never reaches Valkey")
	})

	mixed := step{"global_s": twoDaysSeconds, "user_s": oneHourSeconds}

	t.Run("mixed windows pass once, then a refused global spends no viewer key", func(t *testing.T) {
		r.forChannel(t, uniqueChannel(22), mixed)
		r.assertPasses(t, alice)
		assert.True(t, r.viewerKeyExists(t, alice), "the winner holds its 1-hour Valkey window")
		assert.Len(t, r.storedWindows(t), 1, "the winner holds the 2-day SQL window")

		r.assertRefused(t, alice)
		r.assertRefused(t, bob)

		assert.False(t, r.viewerKeyExists(t, bob), "SQL refused first, so the viewer window stays free")
		assert.Len(t, r.storedWindows(t), 1)
	})

	t.Run("a refused viewer window gives the global row back", func(t *testing.T) {
		r.forChannel(t, uniqueChannel(23), mixed)
		ok, err := r.cooldown.Allow(context.Background(), r.viewerKey(carol), time.Hour)
		require.NoError(t, err)
		require.True(t, ok)

		r.assertRefused(t, carol)
		assert.Empty(t, r.storedWindows(t), "nothing stays spent in SQL")

		r.assertPasses(t, bob)
		assert.Len(t, r.storedWindows(t), 1, "the global window was still free")
	})
}

func TestLongCooldownsThroughNATSAndLoyaltyOnSQLite(t *testing.T) {
	db, err := sql.Open(testdb.Driver, testdb.MemDSN(testdb.Name(t.Name())))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	exerciseLongCooldowns(t, startCooldownRig(t, loyaltyDB{Pool: db, Dialect: "sqlite3"}))
}

func TestLongCooldownsThroughNATSAndLoyaltyOnMySQL(t *testing.T) {
	exerciseLongCooldowns(t, startCooldownRig(t, loyaltyDB{Pool: testdb.MySQL(t, "bagel_cooldown_"), Dialect: "mysql"}))
}
