package repository_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"ItsBagelBot/app/modules/ent/enttest"
	"ItsBagelBot/app/modules/repository"

	_ "github.com/mattn/go-sqlite3" // Required for the in-memory DB
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.uber.org/zap"
)

func setupQuotes(t *testing.T) *repository.Quotes {
	t.Helper()

	client := enttest.Open(t, "sqlite3", "file:modquotesent?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	return repository.NewQuotes(client, zap.NewNop())
}

func TestQuoteAddAtUsesChosenDate(t *testing.T) {
	repo := setupQuotes(t)
	ctx := context.Background()
	chosen := time.Date(2024, time.February, 29, 12, 0, 0, 0, time.UTC)

	saved, err := repo.AddAt(ctx, 1001, "a leap-day quote", "dashboard", chosen)
	require.NoError(t, err)
	assert.Equal(t, "2024-02-29T12:00:00Z", saved.CreatedAt)

	got, found, err := repo.Get(ctx, 1001, saved.Number)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, saved.CreatedAt, got.CreatedAt)
}

func TestQuoteAddNumbersSequentially(t *testing.T) {
	repo := setupQuotes(t)
	ctx := context.Background()

	first, err := repo.Add(ctx, 1001, "never trust a ferret", "mod_amy")
	require.NoError(t, err)
	assert.Equal(t, uint64(1), first.Number)
	assert.Equal(t, "mod_amy", first.AddedBy)
	assert.NotEmpty(t, first.CreatedAt)

	second, err := repo.Add(ctx, 1001, "the bagels are sentient", "mod_amy")
	require.NoError(t, err)
	assert.Equal(t, uint64(2), second.Number)

	// Another channel starts its own numbering.
	other, err := repo.Add(ctx, 2002, "hello from elsewhere", "mod_bob")
	require.NoError(t, err)
	assert.Equal(t, uint64(1), other.Number)
}

func TestQuoteAddValidates(t *testing.T) {
	repo := setupQuotes(t)
	ctx := context.Background()

	_, err := repo.Add(ctx, 1001, "   ", "mod_amy")
	assert.ErrorIs(t, err, repository.ErrQuoteEmpty)

	_, err = repo.Add(ctx, 1001, strings.Repeat("x", repository.QuoteTextMaxLen+1), "mod_amy")
	assert.ErrorIs(t, err, repository.ErrQuoteTooLong)
}

func TestQuoteGet(t *testing.T) {
	repo := setupQuotes(t)
	ctx := context.Background()

	saved, err := repo.Add(ctx, 1001, "never trust a ferret", "mod_amy")
	require.NoError(t, err)

	got, found, err := repo.Get(ctx, 1001, saved.Number)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "never trust a ferret", got.Text)

	_, found, err = repo.Get(ctx, 1001, 99)
	require.NoError(t, err)
	assert.False(t, found)

	// Numbers are channel-scoped: another channel does not see #1.
	_, found, err = repo.Get(ctx, 2002, saved.Number)
	require.NoError(t, err)
	assert.False(t, found)
}

func TestQuoteList(t *testing.T) {
	repo := setupQuotes(t)
	ctx := context.Background()

	list, err := repo.List(ctx, 1001)
	require.NoError(t, err)
	assert.Empty(t, list)

	_, err = repo.Add(ctx, 1001, "one", "m")
	require.NoError(t, err)
	_, err = repo.Add(ctx, 1001, "two", "m")
	require.NoError(t, err)
	_, err = repo.Add(ctx, 1001, "three", "m")
	require.NoError(t, err)
	removed, err := repo.Remove(ctx, 1001, 2)
	require.NoError(t, err)
	require.True(t, removed)

	// Lowest number first, with the removed #2 absent (its hole preserved).
	list, err = repo.List(ctx, 1001)
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, uint64(1), list[0].Number)
	assert.Equal(t, "one", list[0].Text)
	assert.Equal(t, uint64(3), list[1].Number)

	// Scoped per channel.
	other, err := repo.List(ctx, 2002)
	require.NoError(t, err)
	assert.Empty(t, other)
}

func TestQuoteRandom(t *testing.T) {
	repo := setupQuotes(t)
	ctx := context.Background()

	_, found, err := repo.Random(ctx, 1001)
	require.NoError(t, err)
	assert.False(t, found)

	_, err = repo.Add(ctx, 1001, "only one saved", "mod_amy")
	require.NoError(t, err)

	got, found, err := repo.Random(ctx, 1001)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "only one saved", got.Text)
}

func TestQuoteRemoveLeavesHole(t *testing.T) {
	repo := setupQuotes(t)
	ctx := context.Background()

	_, err := repo.Add(ctx, 1001, "one", "m")
	require.NoError(t, err)
	_, err = repo.Add(ctx, 1001, "two", "m")
	require.NoError(t, err)
	_, err = repo.Add(ctx, 1001, "three", "m")
	require.NoError(t, err)

	found, err := repo.Remove(ctx, 1001, 2)
	require.NoError(t, err)
	assert.True(t, found)

	// #2 stays a hole; the next add continues past the highest number.
	_, found, err = repo.Get(ctx, 1001, 2)
	require.NoError(t, err)
	assert.False(t, found)

	next, err := repo.Add(ctx, 1001, "four", "m")
	require.NoError(t, err)
	assert.Equal(t, uint64(4), next.Number)

	// Removing an already-gone number reports not found.
	found, err = repo.Remove(ctx, 1001, 2)
	require.NoError(t, err)
	assert.False(t, found)
}

func TestQuoteDeleteAllForUser(t *testing.T) {
	repo := setupQuotes(t)
	ctx := context.Background()

	_, err := repo.Add(ctx, 1001, "mine", "m")
	require.NoError(t, err)
	kept, err := repo.Add(ctx, 2002, "theirs", "m")
	require.NoError(t, err)

	require.NoError(t, repo.DeleteAllForUser(ctx, 1001))

	_, found, err := repo.Get(ctx, 1001, 1)
	require.NoError(t, err)
	assert.False(t, found)

	_, found, err = repo.Get(ctx, 2002, kept.Number)
	require.NoError(t, err)
	assert.True(t, found)
}
