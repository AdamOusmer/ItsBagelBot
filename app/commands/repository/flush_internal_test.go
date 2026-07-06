package repository

import (
	"context"
	"testing"

	"ItsBagelBot/app/commands/ent/enttest"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/bus/bustest"

	_ "github.com/mattn/go-sqlite3" // Required for the in-memory DB
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.uber.org/zap"
)

// A row the database will never accept (empty response violates the schema
// validator) must be dropped by the per-item fallback while the rest of the
// window lands — the poison item must not wedge the whole batch.
func TestUpsertEachIsolatesPoisonItem(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:commandsflush?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	r := NewCommands(client, bustest.NewPublisher(), nil, zap.NewNop())

	ctx := context.Background()
	good := data.CommandChangedDTO{UserID: 1001, Name: "ok", Response: "fine", IsActive: true, Perm: "everyone"}
	poison := data.CommandChangedDTO{UserID: 1001, Name: "bad", Response: "", IsActive: true, Perm: "everyone"}

	landed := r.upsertEach(ctx, nil, []data.CommandChangedDTO{good, poison})

	require.Len(t, landed, 1)
	assert.Equal(t, "ok", landed[0].Name)

	rows := client.Commands.Query().AllX(ctx)
	require.Len(t, rows, 1)
	assert.Equal(t, "ok", rows[0].Name)

	// A permanent failure must not be requeued: the next flush window would
	// hit the same validation error forever. Close drains anything pending,
	// so the row count must not change.
	r.Close(ctx)
	assert.Len(t, client.Commands.Query().AllX(ctx), 1)
}

// The bulk fast path must not touch the uses counter when an edit updates an
// existing row: the counter belongs to the counter flush alone.
func TestBulkUpsertPreservesUses(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:commandsuses?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	ctx := context.Background()
	client.Commands.Create().
		SetUserID(1001).
		SetName("hello").
		SetResponse("old wording").
		SetUses(42).
		SaveX(ctx)

	edit := data.CommandChangedDTO{UserID: 1001, Name: "hello", Response: "new wording", IsActive: true, Perm: "everyone"}
	require.NoError(t, bulkUpsertCommands(ctx, client, []data.CommandChangedDTO{edit}))

	row := client.Commands.Query().OnlyX(ctx)
	assert.Equal(t, "new wording", row.Response)
	assert.Equal(t, uint64(42), row.Uses)
}
