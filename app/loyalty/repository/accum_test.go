package repository

import (
	"testing"

	"ItsBagelBot/app/loyalty/ent"
	"ItsBagelBot/internal/domain/event/data"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bare returns a repository with only the accumulators usable — enough for the
// fold/drain logic, which never touches the DB.
func bare() *Loyalty {
	return &Loyalty{
		earnPend: map[balKey]*earnSum{},
		bumpPend: map[bumpKey]*bumpSum{},
	}
}

func TestRecordEarnedFolds(t *testing.T) {
	r := bare()
	r.RecordEarned(data.LoyaltyEarnedDTO{UserID: 1, Entries: []data.LoyaltyEarnEntry{
		{ViewerID: 7, ViewerLogin: "cool", Points: 100, WatchSeconds: 300},
		{ViewerID: 7, ViewerName: "Cool", Points: 50},
		{ViewerID: 8, WatchSeconds: 300},
		{ViewerID: 0, Points: 10}, // no viewer: dropped
		{ViewerID: 9},             // nothing earned: dropped
	}})

	earn, bumps := r.drain()
	assert.Empty(t, bumps)
	require.Len(t, earn, 2)
	seven := earn[balKey{userID: 1, viewerID: 7}]
	require.NotNil(t, seven)
	assert.Equal(t, int64(150), seven.points)
	assert.Equal(t, uint64(300), seven.watchSeconds)
	assert.Equal(t, "cool", seven.login)
	assert.Equal(t, "Cool", seven.name)

	// drain swapped the map out.
	earn, _ = r.drain()
	assert.Empty(t, earn)
}

func TestRecordBumpsFoldsAndValidates(t *testing.T) {
	r := bare()
	r.RecordBumps(data.CounterBumpedDTO{UserID: 1, Bumps: []data.CounterBumpEntry{
		{Name: "!Deaths", Delta: 1}, // normalized to "deaths"
		{Name: "deaths", Delta: 2},  // folds with the one above
		{Name: "hugs", Scope: data.CounterScopeViewer, ViewerID: 7, ViewerLogin: "cool", Delta: 1},     // viewer scope, identity carried
		{Name: "hugs", Scope: data.CounterScopeViewer, ViewerID: 7, ViewerName: "Cool", Delta: 1},     // folds; name fills in, login kept
		{Name: "hugs", Scope: data.CounterScopeViewer, Delta: 1},                                      // viewer scope without viewer: dropped
		{Name: "uses", Scope: data.CounterScopeViewerCommand, ViewerID: 7, Command: "!Hug", Delta: 2}, // command normalized
		{Name: "raids", Scope: data.CounterScopeCommand, Command: "!Raid", Delta: 3},                  // pooled command scope
		{Name: "pulls", Scope: data.CounterScopeCommand, Delta: 2},                                   // nameless source: pools on the row (channel shape)
		{Name: "feeds", Scope: data.CounterScopeBot, Delta: 1},                                        // bot outside bot namespace: dropped
		{Name: "bot:x", Delta: 1}, // reserved ':' name: dropped
		{Name: "", Delta: 1},      // no name: dropped
		{Name: "noop", Delta: 0},  // no delta: dropped
	}})
	r.RecordBumps(data.CounterBumpedDTO{UserID: 0, Bumps: []data.CounterBumpEntry{
		{Name: "feeds", Scope: data.CounterScopeBot, Delta: 4},                // bot namespace
		{Name: "deaths", Delta: 1},                                            // channel bump without broadcaster: dropped
		{Name: "hugs", Scope: data.CounterScopeViewer, ViewerID: 7, Delta: 1}, // viewer bump without broadcaster: dropped
	}})

	_, bumps := r.drain()
	require.Len(t, bumps, 6)
	deaths := bumps[bumpKey{userID: 1, name: "deaths"}]
	require.NotNil(t, deaths)
	assert.Equal(t, int64(3), deaths.delta)
	assert.Equal(t, data.CounterScopeChannel, deaths.scope)
	hugs := bumps[bumpKey{userID: 1, name: "hugs", viewerID: 7}]
	require.NotNil(t, hugs)
	assert.Equal(t, int64(2), hugs.delta)
	assert.Equal(t, data.CounterScopeViewer, hugs.scope)
	assert.Equal(t, "cool", hugs.login)
	assert.Equal(t, "Cool", hugs.name)
	uses := bumps[bumpKey{userID: 1, name: "uses", command: "hug", viewerID: 7}]
	require.NotNil(t, uses)
	assert.Equal(t, int64(2), uses.delta)
	assert.Equal(t, data.CounterScopeViewerCommand, uses.scope)
	raids := bumps[bumpKey{userID: 1, name: "raids", command: "raid"}]
	require.NotNil(t, raids)
	assert.Equal(t, int64(3), raids.delta)
	assert.Equal(t, data.CounterScopeCommand, raids.scope)
	pulls := bumps[bumpKey{userID: 1, name: "pulls"}]
	require.NotNil(t, pulls)
	assert.Equal(t, int64(2), pulls.delta)
	assert.Equal(t, data.CounterScopeChannel, pulls.scope) // nameless source pools on the row
	feeds := bumps[bumpKey{name: "feeds"}]
	require.NotNil(t, feeds)
	assert.Equal(t, int64(4), feeds.delta)
	assert.Equal(t, data.CounterScopeBot, feeds.scope)
}

func TestSplitBumpsRouting(t *testing.T) {
	bumps := map[bumpKey]*bumpSum{
		{userID: 1, name: "deaths"}:                            {scope: data.CounterScopeChannel},
		{name: "feeds"}:                                        {scope: data.CounterScopeBot},
		{userID: 1, name: "hugs", viewerID: 7}:                 {scope: data.CounterScopeViewer},
		{userID: 1, name: "raids", command: "raid"}:            {scope: data.CounterScopeCommand},
		{userID: 1, name: "uses", command: "hug", viewerID: 7}: {scope: data.CounterScopeViewerCommand},
	}
	channel, entries := splitBumps(bumps)
	assert.Len(t, channel, 2) // channel + bot land on the counter row
	assert.Len(t, entries, 3) // viewer, command, viewer+command land in counter_entries
}

func TestEntryTarget(t *testing.T) {
	scoped := func(scope string) *ent.Counter { return &ent.Counter{Scope: scope} }

	v, cmd, ok := entryTarget(scoped(data.CounterScopeCommand), 7, "!Raid")
	require.True(t, ok)
	assert.Equal(t, uint64(0), v) // pooled: viewer never keys a command bucket
	assert.Equal(t, "raid", cmd)

	// A command scope addressed without a command is untargeted: reads answer
	// with the row value, sets reset the counter (never a hidden "" bucket).
	_, _, ok = entryTarget(scoped(data.CounterScopeCommand), 7, "")
	assert.False(t, ok)

	v, cmd, ok = entryTarget(scoped(data.CounterScopeViewerCommand), 7, "!Raid")
	require.True(t, ok)
	assert.Equal(t, uint64(7), v)
	assert.Equal(t, "raid", cmd)

	_, _, ok = entryTarget(scoped(data.CounterScopeViewer), 0, "")
	assert.False(t, ok) // viewer scope without a viewer answers with the row value

	_, _, ok = entryTarget(scoped(data.CounterScopeBot), 7, "x")
	assert.False(t, ok) // row-scoped
}

func TestValidCounterName(t *testing.T) {
	n, err := ValidCounterName("  !Deaths ")
	require.NoError(t, err)
	assert.Equal(t, "deaths", n)

	_, err = ValidCounterName("   ")
	assert.ErrorIs(t, err, ErrInvalidInput)

	long := make([]byte, maxCounterName+1)
	for i := range long {
		long[i] = 'a'
	}
	_, err = ValidCounterName(string(long))
	assert.ErrorIs(t, err, ErrInvalidInput)

	// ':' is reserved for the worker's {counter:bot:name} token prefix.
	_, err = ValidCounterName("bot:feeds")
	assert.ErrorIs(t, err, ErrInvalidInput)
}

func TestValidScope(t *testing.T) {
	s, err := ValidScope("")
	require.NoError(t, err)
	assert.Equal(t, data.CounterScopeChannel, s)

	s, err = ValidScope(data.CounterScopeViewer)
	require.NoError(t, err)
	assert.Equal(t, data.CounterScopeViewer, s)

	_, err = ValidScope("global")
	assert.ErrorIs(t, err, ErrInvalidInput)
}
