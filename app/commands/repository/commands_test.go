package repository_test

import (
	"context"
	"encoding/json"
	"testing"

	"ItsBagelBot/app/commands/ent"
	"ItsBagelBot/app/commands/ent/enttest"
	"ItsBagelBot/app/commands/repository"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/bus/bustest"

	_ "github.com/mattn/go-sqlite3" // Required for the in-memory DB
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.uber.org/zap"
)

func setup(t *testing.T) (*ent.Client, *bustest.Publisher, *repository.Commands) {
	t.Helper()

	client := enttest.Open(t, "sqlite3", "file:commandsent?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	pub := bustest.NewPublisher()

	return client, pub, repository.NewCommands(client, pub, nil, zap.NewNop())
}

// spec builds a CommandSpec with the fields these tests vary.
func spec(name, response string, streamOnlineOnly bool, cooldown uint) repository.CommandSpec {
	return repository.CommandSpec{
		Name:             name,
		Response:         response,
		IsActive:         true,
		StreamOnlineOnly: streamOnlineOnly,
		Perm:             "everyone",
		Cooldown:         cooldown,
	}
}

func TestUpsertCoalescesEdits(t *testing.T) {
	client, pub, repo := setup(t)
	ctx := context.Background()

	repo.Upsert(1001, spec("!hello", "draft one", false, 0))
	repo.Upsert(1001, spec("!hello", "draft two", false, 0))
	repo.Upsert(1001, spec("!hello", "final wording", true, 0))

	repo.Close(ctx) // deterministic flush

	rows := client.Commands.Query().AllX(ctx)
	require.Len(t, rows, 1)
	assert.Equal(t, "final wording", rows[0].Response)
	assert.True(t, rows[0].StreamOnlineOnly)

	require.Len(t, pub.On(data.SubjectCommandChanged), 1)
}

// TestUpsertNormalizesMultiLineResponse proves the response canonicalization:
// CRLF folds to LF, trailing whitespace and blank lines vanish, and what lands
// in the row is the newline-delimited form the worker splits on.
func TestUpsertNormalizesMultiLineResponse(t *testing.T) {
	client, _, repo := setup(t)
	ctx := context.Background()

	require.NoError(t, repo.Upsert(1001, spec("!multi", "line one  \r\n\r\nline two\r\nline three", false, 0)))

	repo.Close(ctx) // deterministic flush

	rows := client.Commands.Query().AllX(ctx)
	require.Len(t, rows, 1)
	assert.Equal(t, "line one\nline two\nline three", rows[0].Response)
}

// TestUpsertRejectsTooManyLines proves the six-line response is refused at the
// trust boundary (five is the ceiling).
func TestUpsertRejectsTooManyLines(t *testing.T) {
	_, _, repo := setup(t)

	err := repo.Upsert(1001, spec("!multi", "1\n2\n3\n4\n5\n6", false, 0))
	require.Error(t, err)

	repo.Close(context.Background())
}

func TestDeleteIsImmediateAndAnnounced(t *testing.T) {
	client, pub, repo := setup(t)
	ctx := context.Background()

	repo.Upsert(1001, spec("!hello", "hi chat", false, 0))
	repo.Close(ctx)

	repo2 := repository.NewCommands(client, pub, nil, zap.NewNop())
	defer repo2.Close(ctx)

	require.NoError(t, repo2.Delete(ctx, 1001, "!hello"))

	assert.Equal(t, 0, client.Commands.Query().CountX(ctx))

	events := pub.On(data.SubjectCommandChanged)
	require.NotEmpty(t, events)

	var dto data.CommandChangedDTO
	require.NoError(t, json.Unmarshal(events[len(events)-1].Payload, &dto))
	assert.True(t, dto.Deleted)
}

func TestRenameUpdatesRowInPlace(t *testing.T) {
	client, pub, repo := setup(t)
	ctx := context.Background()

	repo.Upsert(1001, spec("!old", "the response", false, 7))
	repo.Close(ctx)
	originalID := client.Commands.Query().FirstX(ctx).ID

	repo2 := repository.NewCommands(client, pub, nil, zap.NewNop())
	defer repo2.Close(ctx)

	baseline := len(pub.On(data.SubjectCommandChanged)) // the create flush above

	require.NoError(t, repo2.Rename(ctx, 1001, "!old", spec("!new", "the response", true, 7)))

	// Exactly one row, same primary key (updated in place, not deleted+recreated).
	rows := client.Commands.Query().AllX(ctx)
	require.Len(t, rows, 1)
	assert.Equal(t, "new", rows[0].Name)
	assert.Equal(t, originalID, rows[0].ID, "rename must preserve the row identity")
	assert.True(t, rows[0].StreamOnlineOnly)

	// A delete for the old name and a change for the new name are announced so
	// name-keyed consumers drop the stale key.
	events := pub.On(data.SubjectCommandChanged)
	require.Len(t, events, baseline+2)
	renameEvents := events[baseline:]

	var del, changed data.CommandChangedDTO
	require.NoError(t, json.Unmarshal(renameEvents[0].Payload, &del))
	require.NoError(t, json.Unmarshal(renameEvents[1].Payload, &changed))
	assert.True(t, del.Deleted)
	assert.Equal(t, "old", del.Name)
	assert.False(t, changed.Deleted)
	assert.Equal(t, "new", changed.Name)
	assert.True(t, changed.StreamOnlineOnly)
}

func TestRenameMissingRowFallsBackToCreate(t *testing.T) {
	client, _, repo := setup(t)
	ctx := context.Background()

	require.NoError(t, repo.Rename(ctx, 1001, "!ghost", spec("!new", "resp", true, 0)))
	repo.Close(ctx) // flush the fallback upsert

	rows := client.Commands.Query().AllX(ctx)
	require.Len(t, rows, 1)
	assert.Equal(t, "new", rows[0].Name)
	assert.True(t, rows[0].StreamOnlineOnly)
}

func TestListServedFromCache(t *testing.T) {
	client, _, repo := setup(t)
	ctx := context.Background()

	client.Commands.Create().
		SetUserID(1001).
		SetName("!hello").
		SetResponse("hi").
		ExecX(ctx)

	views, err := repo.List(ctx, 1001)
	require.NoError(t, err)
	require.Len(t, views, 1)

	client.Commands.Update().SetResponse("changed").SaveX(ctx)

	views, err = repo.List(ctx, 1001)
	require.NoError(t, err)
	assert.Equal(t, "hi", views[0].Response, "read must come from the in-process cache")

	repo.Invalidate(1001)

	views, err = repo.List(ctx, 1001)
	require.NoError(t, err)
	assert.Equal(t, "changed", views[0].Response)
}
