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

	return client, pub, repository.NewCommands(client, pub, zap.NewNop())
}

func TestUpsertCoalescesEdits(t *testing.T) {
	client, pub, repo := setup(t)
	ctx := context.Background()

	repo.Upsert(1001, "!hello", "draft one", true)
	repo.Upsert(1001, "!hello", "draft two", true)
	repo.Upsert(1001, "!hello", "final wording", true)

	repo.Close(ctx) // deterministic flush

	rows := client.Commands.Query().AllX(ctx)
	require.Len(t, rows, 1)
	assert.Equal(t, "final wording", rows[0].Response)

	require.Len(t, pub.On(data.SubjectCommandChanged), 1)
}

func TestDeleteIsImmediateAndAnnounced(t *testing.T) {
	client, pub, repo := setup(t)
	ctx := context.Background()

	repo.Upsert(1001, "!hello", "hi chat", true)
	repo.Close(ctx)

	repo2 := repository.NewCommands(client, pub, zap.NewNop())
	defer repo2.Close(ctx)

	require.NoError(t, repo2.Delete(ctx, 1001, "!hello"))

	assert.Equal(t, 0, client.Commands.Query().CountX(ctx))

	events := pub.On(data.SubjectCommandChanged)
	require.NotEmpty(t, events)

	var dto data.CommandChangedDTO
	require.NoError(t, json.Unmarshal(events[len(events)-1].Payload, &dto))
	assert.True(t, dto.Deleted)
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
