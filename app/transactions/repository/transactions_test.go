package repository_test

import (
	"context"
	"testing"

	"ItsBagelBot/app/transactions/ent/enttest"
	"ItsBagelBot/app/transactions/repository"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/bus/bustest"

	_ "github.com/mattn/go-sqlite3" // Required for the in-memory DB
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tebex retries webhooks, so recording the same transaction twice must be a
// silent no-op: one row, one event, no error.
func TestRecordIsIdempotent(t *testing.T) {

	client := enttest.Open(t, "sqlite3", "file:txent?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	pub := bustest.NewPublisher()
	repo := repository.NewTransactions(client, pub)
	ctx := context.Background()

	require.NoError(t, repo.Record(ctx, "tbx-1234", 1001))
	require.NoError(t, repo.Record(ctx, "tbx-1234", 1001), "webhook retry must not error")

	assert.Equal(t, 1, client.TebexTransactions.Query().CountX(ctx))
	assert.Len(t, pub.On(data.SubjectTransactionRecorded), 1, "a retry must not re-announce")

	userID, err := repo.UserOf(ctx, "tbx-1234")
	require.NoError(t, err)
	assert.Equal(t, uint64(1001), userID)
}
