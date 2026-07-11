package repository

import (
	"testing"

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
		{Name: "hugs", Scope: data.CounterScopeViewer, ViewerID: 7, Delta: 1},                         // viewer scope
		{Name: "hugs", Scope: data.CounterScopeViewer, Delta: 1},                                      // viewer scope without viewer: dropped
		{Name: "uses", Scope: data.CounterScopeViewerCommand, ViewerID: 7, Command: "!Hug", Delta: 2}, // command normalized
		{Name: "", Delta: 1},     // no name: dropped
		{Name: "noop", Delta: 0}, // no delta: dropped
	}})

	_, bumps := r.drain()
	require.Len(t, bumps, 3)
	deaths := bumps[bumpKey{userID: 1, name: "deaths"}]
	require.NotNil(t, deaths)
	assert.Equal(t, int64(3), deaths.delta)
	assert.Equal(t, data.CounterScopeChannel, deaths.scope)
	hugs := bumps[bumpKey{userID: 1, name: "hugs", viewerID: 7}]
	require.NotNil(t, hugs)
	assert.Equal(t, int64(1), hugs.delta)
	assert.Equal(t, data.CounterScopeViewer, hugs.scope)
	uses := bumps[bumpKey{userID: 1, name: "uses", command: "hug", viewerID: 7}]
	require.NotNil(t, uses)
	assert.Equal(t, int64(2), uses.delta)
	assert.Equal(t, data.CounterScopeViewerCommand, uses.scope)
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
