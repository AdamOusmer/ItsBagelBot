package repository_test

import (
	"context"
	"encoding/json"
	"testing"

	"ItsBagelBot/app/modules/ent"
	"ItsBagelBot/app/modules/ent/enttest"
	"ItsBagelBot/app/modules/repository"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/bus/bustest"

	_ "github.com/mattn/go-sqlite3" // Required for the in-memory DB
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.uber.org/zap"
)

func setup(t *testing.T) (*ent.Client, *bustest.Publisher, *repository.Modules) {
	t.Helper()

	client := enttest.Open(t, "sqlite3", "file:modulesent?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	pub := bustest.NewPublisher()

	return client, pub, repository.NewModules(client, pub, zap.NewNop())
}

// Five clicks on the same toggle inside one window must cost one row write
// and announce only the final state.
func TestSetCoalescesIntoOneWrite(t *testing.T) {
	client, pub, repo := setup(t)
	ctx := context.Background()

	repo.Set(1001, "welcome", true, json.RawMessage(`{"message":"hi"}`))
	repo.Set(1001, "welcome", false, json.RawMessage(`{"message":"hey"}`))
	repo.Set(1001, "welcome", true, json.RawMessage(`{"message":"final"}`))

	repo.Close(ctx) // deterministic flush

	rows := client.Modules.Query().AllX(ctx)
	require.Len(t, rows, 1, "coalesced writes must produce a single row")

	assert.True(t, rows[0].IsEnabled)
	assert.JSONEq(t, `{"message":"final"}`, string(rows[0].Configs))

	events := pub.On(data.SubjectModuleChanged)
	require.Len(t, events, 1, "only the final state may be announced")

	var dto data.ModuleChangedDTO
	require.NoError(t, json.Unmarshal(events[0].Payload, &dto))
	assert.Equal(t, uint64(1001), dto.UserID)
	assert.True(t, dto.IsEnabled)
}

func TestFlushUpdatesExistingRow(t *testing.T) {
	client, _, repo := setup(t)
	ctx := context.Background()

	repo.Set(1001, "welcome", true, json.RawMessage(`{}`))
	repo.Close(ctx)

	repo2 := repository.NewModules(client, bustest.NewPublisher(), zap.NewNop())
	repo2.Set(1001, "welcome", false, json.RawMessage(`{"changed":true}`))
	repo2.Close(ctx)

	rows := client.Modules.Query().AllX(ctx)
	require.Len(t, rows, 1, "an update must not create a second row")
	assert.False(t, rows[0].IsEnabled)
}

func TestListServedFromCacheUntilInvalidated(t *testing.T) {
	client, _, repo := setup(t)
	ctx := context.Background()

	client.Modules.Create().
		SetUserID(1001).
		SetName("welcome").
		SetIsEnabled(true).
		ExecX(ctx)

	views, err := repo.List(ctx, 1001)
	require.NoError(t, err)
	require.Len(t, views, 1)

	// Mutate behind the cache's back: List must keep serving the cached view.
	client.Modules.Update().SetIsEnabled(false).SaveX(ctx)

	views, err = repo.List(ctx, 1001)
	require.NoError(t, err)
	assert.True(t, views[0].IsEnabled, "read must come from the in-process cache")

	repo.Invalidate(1001)

	views, err = repo.List(ctx, 1001)
	require.NoError(t, err)
	assert.False(t, views[0].IsEnabled, "invalidation must expose the new state")
}
