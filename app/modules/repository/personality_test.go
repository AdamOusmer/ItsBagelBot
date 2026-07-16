package repository_test

import (
	"context"
	"testing"

	"ItsBagelBot/app/modules/ent"
	"ItsBagelBot/app/modules/ent/enttest"
	"ItsBagelBot/app/modules/repository"

	_ "github.com/mattn/go-sqlite3" // Required for the in-memory DB
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupPersonality(t *testing.T) (*ent.Client, *repository.Personality) {
	t.Helper()

	client := enttest.Open(t, "sqlite3", "file:modpersonalityent?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	return client, repository.NewPersonality(client)
}

// The very first feeding must create the single global row; every one after
// that increments it. (True concurrency on the first feed is covered by the
// retry loop and MySQL's atomic UPDATE; sqlite serializes writers, so this
// test keeps to the deterministic paths.)
func TestFeedBumpCreatesThenCounts(t *testing.T) {
	_, repo := setupPersonality(t)
	ctx := context.Background()

	for want := uint64(1); want <= 3; want++ {
		total, err := repo.FeedBump(ctx)
		require.NoError(t, err)
		assert.Equal(t, want, total)
	}
}

func TestFeedBumpIncrementsExistingRow(t *testing.T) {
	client, repo := setupPersonality(t)
	ctx := context.Background()

	require.NoError(t, client.FeedCounter.Create().SetID(1).SetCount(41).Exec(ctx))

	total, err := repo.FeedBump(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(42), total, "bump must ride the existing permanent row")
}
