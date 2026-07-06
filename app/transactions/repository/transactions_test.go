package repository_test

import (
	"context"
	"testing"

	"ItsBagelBot/app/transactions/ent/enttest"
	"ItsBagelBot/app/transactions/ent/tebexwebhookevents"
	"ItsBagelBot/app/transactions/repository"

	_ "github.com/mattn/go-sqlite3" // Required for the in-memory DB
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSaveWebhookEventUpsertsState(t *testing.T) {

	client := enttest.Open(t, "sqlite3", "file:webhook-events?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	repo := repository.NewTransactions(client)
	ctx := context.Background()

	require.NoError(t, repo.SaveWebhookEvent(ctx, repository.WebhookEvent{
		ID:            "evt-1234",
		Type:          "payment.completed",
		Status:        repository.WebhookFailed,
		TransactionID: "tbx-1234",
		Error:         "payment user id missing",
	}))

	row := client.TebexWebhookEvents.GetX(ctx, "evt-1234")
	assert.Equal(t, tebexwebhookevents.StatusFailed, row.Status)
	assert.Equal(t, "payment user id missing", row.Error)
	assert.Equal(t, "tbx-1234", row.TransactionID)
	assert.Zero(t, row.UserID)

	require.NoError(t, repo.SaveWebhookEvent(ctx, repository.WebhookEvent{
		ID:            "evt-1234",
		Type:          "payment.completed",
		Status:        repository.WebhookProcessed,
		TransactionID: "tbx-1234",
		UserID:        1001,
	}))

	row = client.TebexWebhookEvents.GetX(ctx, "evt-1234")
	assert.Equal(t, tebexwebhookevents.StatusProcessed, row.Status)
	assert.Empty(t, row.Error)
	assert.Equal(t, "tbx-1234", row.TransactionID)
	assert.Equal(t, uint64(1001), row.UserID)
}

func TestProcessedProof(t *testing.T) {

	client := enttest.Open(t, "sqlite3", "file:proof?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	repo := repository.NewTransactions(client)
	ctx := context.Background()

	// A processed payment leaves proof.
	require.NoError(t, repo.SaveWebhookEvent(ctx, repository.WebhookEvent{
		ID: "evt-ok", Type: "payment.completed", Status: repository.WebhookProcessed,
		TransactionID: "tbx-ok", UserID: 4242,
	}))
	// A non-processed event for a different transaction is not proof.
	require.NoError(t, repo.SaveWebhookEvent(ctx, repository.WebhookEvent{
		ID: "evt-ignored", Type: "payment.declined", Status: repository.WebhookIgnored,
		TransactionID: "tbx-ignored", UserID: 4242,
	}))

	proof, ok, err := repo.ProcessedProof(ctx, "tbx-ok")
	require.NoError(t, err)
	require.True(t, ok, "a processed transaction must have proof")
	assert.Equal(t, "tbx-ok", proof.TransactionID)
	assert.Equal(t, uint64(4242), proof.UserID)
	assert.Equal(t, "evt-ok", proof.WebhookID)
	assert.Equal(t, "payment.completed", proof.EventType)
	assert.False(t, proof.ProcessedAt.IsZero())

	// An ignored-only transaction is not proof of processing.
	_, ok, err = repo.ProcessedProof(ctx, "tbx-ignored")
	require.NoError(t, err)
	assert.False(t, ok, "a non-processed transaction must not read as proof")

	// Unknown transaction: no proof, no error.
	_, ok, err = repo.ProcessedProof(ctx, "tbx-missing")
	require.NoError(t, err)
	assert.False(t, ok)

	// Empty id is rejected.
	_, _, err = repo.ProcessedProof(ctx, "")
	assert.Error(t, err)
}
