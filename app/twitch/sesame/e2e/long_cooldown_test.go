// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package e2e

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	modulesent "ItsBagelBot/app/db/modules/ent"
	modulesrepo "ItsBagelBot/app/db/modules/repository"
	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/internal/domain/event/lane"
	automationrpc "ItsBagelBot/internal/domain/rpc/automation"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/internal/valkeytest"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/bus/bustest"
	"ItsBagelBot/pkg/codec"

	baseent "entgo.io/ent"
	entsql "entgo.io/ent/dialect/sql"
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
	moverLeaseKey  = "cooldown:long:mover"
)

type cooldownRig struct {
	*stack
	pool       *sql.DB
	dialect    string
	service    recipeStateService
	channel    uint64
	runsNow    int
	claimCalls atomic.Int64
	peekCalls  atomic.Int64
	failDelete atomic.Bool

	chatMu sync.Mutex
	chat   []chatSent
}

func startCooldownRig(t *testing.T, target recipeDB) *cooldownRig {
	t.Helper()
	vc := valkeytest.Real(t)
	natsURL := startNATS(t)
	nc, err := nats.Connect(natsURL)
	require.NoError(t, err)
	t.Cleanup(nc.Close)
	ensureOutgressStreams(t, natsURL)
	service := serveRecipeState(t, nc, target)
	s := &stack{
		vc: vc, nc: nc, natsURL: natsURL,
		vars:     engine.NewRecipeStateRPC(nc, modulesPrefix),
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
	rig := &cooldownRig{stack: s, pool: target.Pool, dialect: target.Dialect, service: service}
	_, err = nc.Subscribe(modulesPrefix+".cooldown.>", func(msg *nats.Msg) {
		if strings.Contains(msg.Subject, ".claim") {
			rig.claimCalls.Add(1)
		}
		if strings.Contains(msg.Subject, ".peek") {
			rig.peekCalls.Add(1)
		}
	})
	require.NoError(t, err)
	_, err = nc.Subscribe("twitch.outgress.>", rig.recordChat)
	require.NoError(t, err)
	require.NoError(t, nc.Flush())
	service.client.CooldownClaim.Use(func(next baseent.Mutator) baseent.Mutator {
		return baseent.MutateFunc(func(ctx context.Context, m baseent.Mutation) (baseent.Value, error) {
			if m.Op().Is(baseent.OpDelete) && rig.failDelete.Load() {
				return nil, assert.AnError
			}
			return next.Mutate(ctx, m)
		})
	})
	return rig
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

func (r *cooldownRig) globalKey() string {
	return "cooldown:auto:" + strconv.FormatUint(r.channel, 10) + ":" + cooldownRecipe
}

func (r *cooldownRig) viewerKeyExists(t *testing.T, viewer uint64) bool {
	t.Helper()
	count, err := r.vc.Do(context.Background(), r.vc.B().Exists().Key(r.viewerKey(viewer)).Build()).AsInt64()
	require.NoError(t, err)
	return count == 1
}

func (r *cooldownRig) markerTTL(t *testing.T, window string) time.Duration {
	t.Helper()
	key := automation.LongCooldownMarkerKey(r.channel, window)
	ms, err := r.vc.Do(context.Background(), r.vc.B().Pttl().Key(key).Build()).AsInt64()
	require.NoError(t, err)
	return time.Duration(ms) * time.Millisecond
}

func (r *cooldownRig) dropMarker(t *testing.T, window string) {
	t.Helper()
	key := automation.LongCooldownMarkerKey(r.channel, window)
	require.NoError(t, r.vc.Do(context.Background(), r.vc.B().Del().Key(key).Build()).Error())
}

func (r *cooldownRig) setExpiry(t *testing.T, window string, expiry time.Time) int64 {
	t.Helper()
	ms := expiry.UnixMilli()
	result, err := r.pool.Exec("UPDATE cooldown_claims SET expires_at_ms = ? WHERE user_id = ? AND cooldown_key = ?", ms, r.channel, window)
	require.NoError(t, err)
	changed, err := result.RowsAffected()
	require.NoError(t, err)
	require.EqualValues(t, 1, changed)
	return ms
}

func (r *cooldownRig) mover(owner string) *modulesrepo.CooldownMover {
	return modulesrepo.NewCooldownMover(r.service.state, r.vc, owner, zap.NewNop())
}

// Flush proves the server saw the claim subject before counting; the callback still runs on its own goroutine.
func (r *cooldownRig) claimCount(t *testing.T) int64 {
	t.Helper()
	require.NoError(t, r.nc.Flush())
	time.Sleep(50 * time.Millisecond)
	return r.claimCalls.Load()
}

const alice, bob, carol = uint64(101), uint64(102), uint64(103)

func exerciseLongCooldowns(t *testing.T, r *cooldownRig) {
	exerciseClaims(t, r)
	exerciseMarkers(t, r)
	exerciseMover(t, r)
	exerciseMoverLease(t, r)
	exerciseRecipeState(t, r)
}

func exerciseClaims(t *testing.T, r *cooldownRig) {
	t.Run("a 2-day viewer window refuses that viewer only", func(t *testing.T) {
		r.forChannel(t, uniqueChannel(21), step{"user_s": twoDaysSeconds})
		r.assertPasses(t, alice)
		r.assertRefused(t, alice)
		r.assertPasses(t, bob)

		stored := r.storedWindows(t)
		require.Len(t, stored, 2, "both windows live in SQL")
		assert.InDelta(t, time.Now().Add(2*24*time.Hour).UnixMilli(), stored[0], 60_000)
		assert.False(t, r.viewerKeyExists(t, alice), "a long window takes no short key")
	})

	mixed := step{"global_s": twoDaysSeconds, "user_s": oneHourSeconds}

	t.Run("mixed windows pass once, then a refused global spends no viewer key", func(t *testing.T) {
		r.forChannel(t, uniqueChannel(22), mixed)
		r.assertPasses(t, alice)
		assert.True(t, r.viewerKeyExists(t, alice), "the winner holds its 1-hour Valkey window")
		assert.Len(t, r.storedWindows(t), 1, "the winner holds the 2-day SQL window")

		r.assertRefused(t, alice)
		r.assertRefused(t, bob)

		assert.False(t, r.viewerKeyExists(t, bob), "the marker refused first, so the viewer window stays free")
		assert.Len(t, r.storedWindows(t), 1)
	})

	t.Run("a refused viewer window gives the global row and its marker back", func(t *testing.T) {
		r.forChannel(t, uniqueChannel(23), mixed)
		ok, err := r.cooldown.Allow(context.Background(), r.viewerKey(carol), time.Hour)
		require.NoError(t, err)
		require.True(t, ok)

		r.assertRefused(t, carol)
		assert.Empty(t, r.storedWindows(t), "nothing stays spent in SQL")
		assert.EqualValues(t, -2, r.markerTTL(t, r.globalKey())/time.Millisecond, "nothing stays spent in Valkey")

		r.assertPasses(t, bob)
		assert.Len(t, r.storedWindows(t), 1, "the global window was still free")
	})
}

func exerciseMarkers(t *testing.T, r *cooldownRig) {
	t.Run("a live marker answers with no RPC", func(t *testing.T) {
		r.forChannel(t, uniqueChannel(24), step{"global_s": twoDaysSeconds})
		before := r.claimCount(t)
		r.assertPasses(t, alice)
		afterWin := r.claimCount(t)
		assert.EqualValues(t, 1, afterWin-before, "the winner asks the database once")
		assert.InDelta(t, float64(24*time.Hour), float64(r.markerTTL(t, r.globalKey())), float64(time.Minute))

		for range 5 {
			r.assertRefused(t, bob)
		}

		t.Logf("claim RPCs: win=%d, five refusals=%d", afterWin-before, r.claimCount(t)-afterWin)
		assert.EqualValues(t, afterWin, r.claimCount(t), "refusals never left Sesame")
	})

	t.Run("marker expiry falls through to the database and marks again", func(t *testing.T) {
		r.forChannel(t, uniqueChannel(25), step{"global_s": twoDaysSeconds})
		r.assertPasses(t, alice)
		key := automation.LongCooldownMarkerKey(r.channel, r.globalKey())
		require.NoError(t, r.vc.Do(context.Background(), r.vc.B().Pexpire().Key(key).Milliseconds(1).Build()).Error())
		time.Sleep(20 * time.Millisecond)
		before := r.claimCount(t)

		r.assertRefused(t, bob)
		assert.EqualValues(t, 1, r.claimCount(t)-before, "one database read")
		assert.InDelta(t, float64(24*time.Hour), float64(r.markerTTL(t, r.globalKey())), float64(time.Minute), "the refusal marked again")

		r.assertRefused(t, bob)
		assert.EqualValues(t, 1, r.claimCount(t)-before, "the new marker answers the next one")
	})
}

func exerciseMover(t *testing.T, r *cooldownRig) {
	t.Run("the job moves a due row, the marker holds it, and it passes after expiry", func(t *testing.T) {
		r.forChannel(t, uniqueChannel(26), step{"global_s": twoDaysSeconds})
		r.assertPasses(t, alice)
		r.dropMarker(t, r.globalKey())
		r.setExpiry(t, r.globalKey(), time.Now().Add(1500*time.Millisecond))

		require.NoError(t, r.mover("solo").MoveDue(context.Background()))

		assert.Empty(t, r.storedWindows(t), "the row left the database")
		left := r.markerTTL(t, r.globalKey())
		assert.Positive(t, left)
		assert.LessOrEqual(t, left, 1500*time.Millisecond, "the marker carries the exact time left")
		before := r.claimCount(t)
		r.assertRefused(t, bob)
		assert.EqualValues(t, before, r.claimCount(t), "still refused with no row and no RPC")

		time.Sleep(1600 * time.Millisecond)
		r.assertPasses(t, bob)
		assert.Len(t, r.storedWindows(t), 1, "a fresh claim")
	})

	t.Run("the job extends a shorter marker and never shortens a longer one", func(t *testing.T) {
		r.forChannel(t, uniqueChannel(27), step{"global_s": twoDaysSeconds, "user_s": twoDaysSeconds})
		r.assertPasses(t, alice)
		extended := 24*time.Hour + 30*time.Minute
		r.setExpiry(t, r.globalKey(), time.Now().Add(extended))
		r.setExpiry(t, r.viewerKey(alice), time.Now().Add(time.Hour))

		require.NoError(t, r.mover("solo").MoveDue(context.Background()))

		assert.Empty(t, r.storedWindows(t))
		assert.InDelta(t, float64(extended), float64(r.markerTTL(t, r.globalKey())), float64(time.Minute), "a 24-hour marker grows to the time left")
		assert.InDelta(t, float64(24*time.Hour), float64(r.markerTTL(t, r.viewerKey(alice))), float64(time.Minute), "a longer marker stays")
	})

	t.Run("rows with more than 25 hours left stay and expired rows go", func(t *testing.T) {
		r.forChannel(t, uniqueChannel(28), step{"global_s": twoDaysSeconds, "user_s": twoDaysSeconds})
		r.assertPasses(t, alice)
		far := r.setExpiry(t, r.globalKey(), time.Now().Add(25*time.Hour+10*time.Minute))
		r.setExpiry(t, r.viewerKey(alice), time.Now().Add(-time.Second))

		require.NoError(t, r.mover("solo").MoveDue(context.Background()))

		assert.Equal(t, []int64{far}, r.storedWindows(t))
	})

	t.Run("a crash between marker and delete repeats harmlessly", func(t *testing.T) {
		r.forChannel(t, uniqueChannel(29), step{"global_s": twoDaysSeconds})
		r.assertPasses(t, alice)
		r.dropMarker(t, r.globalKey())
		r.setExpiry(t, r.globalKey(), time.Now().Add(time.Hour))

		r.failDelete.Store(true)
		err := r.mover("solo").MoveDue(context.Background())
		r.failDelete.Store(false)
		require.Error(t, err)
		assert.Len(t, r.storedWindows(t), 1, "the crash left the row")
		first := r.markerTTL(t, r.globalKey())
		assert.InDelta(t, float64(time.Hour), float64(first), float64(time.Minute))
		r.assertRefused(t, bob)

		require.NoError(t, r.mover("solo").MoveDue(context.Background()))

		assert.Empty(t, r.storedWindows(t))
		assert.InDelta(t, float64(first), float64(r.markerTTL(t, r.globalKey())), float64(5*time.Second), "the retry changed nothing")
		r.assertRefused(t, bob)
	})
}

func exerciseMoverLease(t *testing.T, r *cooldownRig) {
	t.Run("two job runners share one lease", func(t *testing.T) {
		r.forChannel(t, uniqueChannel(30), step{"global_s": twoDaysSeconds})
		require.NoError(t, r.vc.Do(context.Background(), r.vc.B().Del().Key(moverLeaseKey).Build()).Error())
		t.Cleanup(func() { r.vc.Do(context.Background(), r.vc.B().Del().Key(moverLeaseKey).Build()) })
		var ran atomic.Int64
		var group sync.WaitGroup
		for _, owner := range []string{"replica-a", "replica-b"} {
			group.Go(func() {
				won, err := r.mover(owner).Tick(context.Background())
				assert.NoError(t, err)
				if won {
					ran.Add(1)
				}
			})
		}
		group.Wait()
		assert.EqualValues(t, 1, ran.Load())
		won, err := r.mover("replica-c").Tick(context.Background())
		require.NoError(t, err)
		assert.False(t, won, "the lease spans the hour")
	})
}

func exerciseRecipeState(t *testing.T, r *cooldownRig) {
	t.Run("variables go through the modules service", func(t *testing.T) {
		ctx := context.Background()
		owner := engine.VariableOwner{BroadcasterID: uniqueChannel(31), Scope: automation.VariableScopeChannel}
		require.NoError(t, r.vars.VariableSet(ctx, owner, "Deaths", "{arg:1}"))
		require.NoError(t, r.vars.VariableSet(ctx, engine.VariableOwner{BroadcasterID: owner.BroadcasterID, ViewerID: 7, Scope: automation.VariableScopeViewer}, "mine", "x"))
		values, err := r.vars.VariablesGet(ctx, owner, []string{"deaths", "absent"})
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"deaths": "{arg:1}"}, values, "stored text stays literal")
		require.NoError(t, r.vars.VariableAdd(ctx, owner, "deaths", 3))
		values, err = r.vars.VariablesGet(ctx, owner, []string{"deaths"})
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"deaths": "3"}, values)
		assert.ErrorIs(t, r.vars.VariableSet(ctx, owner, "big", strings.Repeat("é", automation.MaxVariableValueRunes+1)), engine.ErrVariableLimit)
		for i := range automation.MaxChannelVariables - 1 {
			require.NoError(t, r.vars.VariableSet(ctx, owner, "n"+strconv.Itoa(i), "x"))
		}
		assert.ErrorIs(t, r.vars.VariableSet(ctx, owner, "over", "x"), engine.ErrVariableLimit)
		require.NoError(t, r.vars.VariableDelete(ctx, owner, "deaths"))
		var guards int
		require.NoError(t, r.pool.QueryRow("SELECT COUNT(*) FROM variables WHERE user_id = ? AND scope = ? AND name = '#names'", owner.BroadcasterID, automation.VariableScopeChannel).Scan(&guards))
		assert.Equal(t, 1, guards, "the guard row lives beside the variables")
	})

	t.Run("account erasure removes both", func(t *testing.T) {
		const instance = 100
		ctx := context.Background()
		r.forChannel(t, uniqueChannel(32), step{"global_s": twoDaysSeconds})
		r.assertPasses(t, alice)
		owner := engine.VariableOwner{BroadcasterID: r.channel, Scope: automation.VariableScopeChannel}
		require.NoError(t, r.vars.VariableSet(ctx, owner, "x", "y"))
		repo := modulesrepo.NewModules(modulesent.NewClient(modulesent.Driver(entsql.OpenDB(r.dialect, r.pool))), bustest.NewPublisher(), nil, zap.NewNop())
		t.Cleanup(func() { repo.Close(context.Background()) })
		repo.SetAccountInstanceResolver(func(context.Context, uint64) (int64, error) { return instance, nil })

		require.NoError(t, repo.DeleteAccount(ctx, r.channel, instance))

		assert.Empty(t, r.storedWindows(t))
		values, err := r.vars.VariablesGet(ctx, owner, []string{"x"})
		require.NoError(t, err)
		assert.Empty(t, values)
	})
}

func TestLongCooldownsThroughNATSAndModulesOnSQLite(t *testing.T) {
	exerciseLongCooldowns(t, startCooldownRig(t, sqliteRecipeDB(t)))
}

func TestLongCooldownsThroughNATSAndModulesOnMySQL(t *testing.T) {
	exerciseLongCooldowns(t, startCooldownRig(t, recipeDB{Pool: testdb.MySQL(t, "bagel_cooldown_"), Dialect: "mysql"}))
}

func TestHundredClaimersOneWinnerOnMySQL(t *testing.T) {
	r := startCooldownRig(t, recipeDB{Pool: testdb.MySQL(t, "bagel_cooldown_race_"), Dialect: "mysql"})
	r.pool.SetMaxOpenConns(8)
	store := engine.NewMarkedLongCooldowns(r.vc, r.vars, zap.NewNop())
	claim := engine.LongCooldownClaim{BroadcasterID: uniqueChannel(33), Windows: []engine.CooldownWindow{{Key: "race", TTL: 48 * time.Hour}}}
	var winners atomic.Int64
	start := make(chan struct{})
	var group sync.WaitGroup
	for range 100 {
		group.Go(func() {
			<-start
			_, won, err := store.ClaimLongCooldowns(context.Background(), claim)
			assert.NoError(t, err)
			if won {
				winners.Add(1)
			}
		})
	}
	close(start)
	group.Wait()
	assert.EqualValues(t, 1, winners.Load())
}
