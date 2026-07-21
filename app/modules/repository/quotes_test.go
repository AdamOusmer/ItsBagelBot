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

func TestQuoteAddUsesChosenDate(t *testing.T) {
	repo := setupQuotes(t)
	ctx := context.Background()
	chosen := time.Date(2024, time.February, 29, 12, 0, 0, 0, time.UTC)

	saved, err := repo.Add(ctx, 1001, repository.QuoteDraft{Text: "a leap-day quote", AddedBy: "dashboard", CreatedAt: chosen})
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

	first, err := repo.Add(ctx, 1001, repository.QuoteDraft{Text: "never trust a ferret", AddedBy: "mod_amy"})
	require.NoError(t, err)
	assert.Equal(t, uint64(1), first.Number)
	assert.Equal(t, "mod_amy", first.AddedBy)
	assert.NotEmpty(t, first.CreatedAt)

	second, err := repo.Add(ctx, 1001, repository.QuoteDraft{Text: "the bagels are sentient", AddedBy: "mod_amy"})
	require.NoError(t, err)
	assert.Equal(t, uint64(2), second.Number)

	// Another channel starts its own numbering.
	other, err := repo.Add(ctx, 2002, repository.QuoteDraft{Text: "hello from elsewhere", AddedBy: "mod_bob"})
	require.NoError(t, err)
	assert.Equal(t, uint64(1), other.Number)
}

func TestQuoteAddValidates(t *testing.T) {
	repo := setupQuotes(t)
	ctx := context.Background()

	_, err := repo.Add(ctx, 1001, repository.QuoteDraft{Text: "   ", AddedBy: "mod_amy"})
	assert.ErrorIs(t, err, repository.ErrQuoteEmpty)

	_, err = repo.Add(ctx, 1001, repository.QuoteDraft{
		Text:    strings.Repeat("x", repository.QuoteTextMaxLen+1),
		AddedBy: "mod_amy",
	})
	assert.ErrorIs(t, err, repository.ErrQuoteTooLong)
}

func TestQuoteGet(t *testing.T) {
	repo := setupQuotes(t)
	ctx := context.Background()

	saved, err := repo.Add(ctx, 1001, repository.QuoteDraft{Text: "never trust a ferret", AddedBy: "mod_amy"})
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

	_, err = repo.Add(ctx, 1001, repository.QuoteDraft{Text: "one", AddedBy: "m"})
	require.NoError(t, err)
	_, err = repo.Add(ctx, 1001, repository.QuoteDraft{Text: "two", AddedBy: "m"})
	require.NoError(t, err)
	_, err = repo.Add(ctx, 1001, repository.QuoteDraft{Text: "three", AddedBy: "m"})
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

	_, err = repo.Add(ctx, 1001, repository.QuoteDraft{Text: "only one saved", AddedBy: "mod_amy"})
	require.NoError(t, err)

	got, found, err := repo.Random(ctx, 1001)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "only one saved", got.Text)
}

func TestQuoteSearch(t *testing.T) {
	repo := setupQuotes(t)
	ctx := context.Background()

	_, err := repo.Add(ctx, 1001, repository.QuoteDraft{Text: "Never trust a FERRET", AddedBy: "mod_amy"})
	require.NoError(t, err)
	_, err = repo.Add(ctx, 1001, repository.QuoteDraft{Text: "the bagels are sentient", AddedBy: "mod_amy"})
	require.NoError(t, err)

	// Case-insensitive substring match.
	got, found, err := repo.Search(ctx, 1001, "ferret")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, uint64(1), got.Number)

	// Multi-word terms match across word boundaries.
	got, found, err = repo.Search(ctx, 1001, "are sentient")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, uint64(2), got.Number)

	// No match, blank term, and other channels all come back empty.
	_, found, err = repo.Search(ctx, 1001, "walrus")
	require.NoError(t, err)
	assert.False(t, found)

	_, found, err = repo.Search(ctx, 1001, "   ")
	require.NoError(t, err)
	assert.False(t, found)

	_, found, err = repo.Search(ctx, 2002, "ferret")
	require.NoError(t, err)
	assert.False(t, found)
}

func TestQuoteUpdate(t *testing.T) {
	repo := setupQuotes(t)
	ctx := context.Background()

	saved, err := repo.Add(ctx, 1001, repository.QuoteDraft{Text: "teh bagels", AddedBy: "mod_amy"})
	require.NoError(t, err)

	got, found, err := repo.Update(ctx, 1001, saved.Number, repository.QuoteUpdate{Text: "  the bagels  "})
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "the bagels", got.Text)
	assert.Equal(t, saved.Number, got.Number)
	// A zero CreatedAt keeps the saved date.
	assert.Equal(t, saved.CreatedAt, got.CreatedAt)

	// A chosen CreatedAt rewrites the day.
	chosen := time.Date(2025, time.December, 24, 12, 0, 0, 0, time.UTC)
	got, found, err = repo.Update(ctx, 1001, saved.Number, repository.QuoteUpdate{Text: "the bagels", CreatedAt: chosen})
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "2025-12-24T12:00:00Z", got.CreatedAt)

	// Missing number and other channels report found=false without writing.
	_, found, err = repo.Update(ctx, 1001, 99, repository.QuoteUpdate{Text: "nope"})
	require.NoError(t, err)
	assert.False(t, found)

	_, found, err = repo.Update(ctx, 2002, saved.Number, repository.QuoteUpdate{Text: "nope"})
	require.NoError(t, err)
	assert.False(t, found)

	// Same validation boundary as Add.
	_, _, err = repo.Update(ctx, 1001, saved.Number, repository.QuoteUpdate{Text: "   "})
	assert.ErrorIs(t, err, repository.ErrQuoteEmpty)
	_, _, err = repo.Update(ctx, 1001, saved.Number, repository.QuoteUpdate{Text: strings.Repeat("x", repository.QuoteTextMaxLen+1)})
	assert.ErrorIs(t, err, repository.ErrQuoteTooLong)
}

func TestQuoteRemoveLeavesHole(t *testing.T) {
	repo := setupQuotes(t)
	ctx := context.Background()

	_, err := repo.Add(ctx, 1001, repository.QuoteDraft{Text: "one", AddedBy: "m"})
	require.NoError(t, err)
	_, err = repo.Add(ctx, 1001, repository.QuoteDraft{Text: "two", AddedBy: "m"})
	require.NoError(t, err)
	_, err = repo.Add(ctx, 1001, repository.QuoteDraft{Text: "three", AddedBy: "m"})
	require.NoError(t, err)

	found, err := repo.Remove(ctx, 1001, 2)
	require.NoError(t, err)
	assert.True(t, found)

	// #2 stays a hole; the next add continues past the highest number.
	_, found, err = repo.Get(ctx, 1001, 2)
	require.NoError(t, err)
	assert.False(t, found)

	next, err := repo.Add(ctx, 1001, repository.QuoteDraft{Text: "four", AddedBy: "m"})
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

	_, err := repo.Add(ctx, 1001, repository.QuoteDraft{Text: "mine", AddedBy: "m"})
	require.NoError(t, err)
	kept, err := repo.Add(ctx, 2002, repository.QuoteDraft{Text: "theirs", AddedBy: "m"})
	require.NoError(t, err)

	require.NoError(t, repo.DeleteAllForUser(ctx, 1001))

	_, found, err := repo.Get(ctx, 1001, 1)
	require.NoError(t, err)
	assert.False(t, found)

	_, found, err = repo.Get(ctx, 2002, kept.Number)
	require.NoError(t, err)
	assert.True(t, found)
}
