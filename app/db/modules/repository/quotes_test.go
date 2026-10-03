// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"ItsBagelBot/app/db/modules/ent/enttest"
	"ItsBagelBot/app/db/modules/repository"
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"

	"ItsBagelBot/internal/testdb"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.uber.org/zap"
)

func setupQuotes(t *testing.T) *repository.Quotes {
	t.Helper()

	client := enttest.Open(t, testdb.Driver, testdb.MemDSN("modquotesent"))
	t.Cleanup(func() { _ = client.Close() })

	return repository.NewQuotes(client, zap.NewNop())
}

func addQuotes(t *testing.T, repo *repository.Quotes, userID uint64, texts ...string) []*modulesrpc.Quote {
	t.Helper()
	saved := make([]*modulesrpc.Quote, 0, len(texts))
	for _, text := range texts {
		quote, err := repo.Add(context.Background(), userID, repository.QuoteDraft{Text: text, AddedBy: "mod_amy"})
		require.NoError(t, err)
		saved = append(saved, quote)
	}
	return saved
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

func TestQuoteAddNumbersSequentiallyPerUser(t *testing.T) {
	repo := setupQuotes(t)

	mine := addQuotes(t, repo, 1001, "never trust a ferret", "the bagels are sentient")
	other := addQuotes(t, repo, 2002, "hello from elsewhere")

	assert.Equal(t, []uint64{1, 2, 1}, []uint64{mine[0].Number, mine[1].Number, other[0].Number})
	assert.Equal(t, "mod_amy", mine[0].AddedBy)
	assert.NotEmpty(t, mine[0].CreatedAt)
}

func TestQuoteAddValidates(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
		want error
	}{
		{"rejects blank text", "   ", repository.ErrQuoteEmpty},
		{"rejects text over the limit", strings.Repeat("x", repository.QuoteTextMaxLen+1), repository.ErrQuoteTooLong},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := setupQuotes(t).Add(context.Background(), 1001, repository.QuoteDraft{Text: tc.text, AddedBy: "mod_amy"})

			assert.ErrorIs(t, err, tc.want)
		})
	}
}

func TestQuoteReadsAreScopedToTheUser(t *testing.T) {
	repo := setupQuotes(t)
	ctx := context.Background()
	empty, err := repo.List(ctx, 1001)
	require.NoError(t, err)
	assert.Empty(t, empty)
	_, found, err := repo.Random(ctx, 1001)
	require.NoError(t, err)
	assert.False(t, found)

	saved := addQuotes(t, repo, 1001, "Never trust a FERRET", "the bagels are sentient", "three")
	removed, err := repo.Remove(ctx, 1001, 2)
	require.NoError(t, err)
	require.True(t, removed)

	list, err := repo.List(ctx, 1001)
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, []uint64{1, 3}, []uint64{list[0].Number, list[1].Number})
	assert.Equal(t, "Never trust a FERRET", list[0].Text)
	other, err := repo.List(ctx, 2002)
	require.NoError(t, err)
	assert.Empty(t, other)

	for _, tc := range []struct {
		name   string
		user   uint64
		number uint64
		found  bool
	}{
		{"gets a saved quote", 1001, saved[0].Number, true},
		{"misses an unknown number", 1001, 99, false},
		{"misses another user's number", 2002, saved[0].Number, false},
	} {
		got, found, err := repo.Get(ctx, tc.user, tc.number)
		require.NoError(t, err, tc.name)
		assert.Equal(t, tc.found, found, tc.name)
		assert.Equal(t, tc.found, got != nil, tc.name)
	}

	random, found, err := repo.Random(ctx, 1001)
	require.NoError(t, err)
	require.True(t, found)
	assert.Contains(t, []uint64{1, 3}, random.Number)
}

func TestQuoteSearch(t *testing.T) {
	repo := setupQuotes(t)
	ctx := context.Background()
	addQuotes(t, repo, 1001, "Never trust a FERRET", "the bagels are sentient")

	for _, tc := range []struct {
		name   string
		user   uint64
		query  string
		number uint64
	}{
		{"matches case-insensitively", 1001, "ferret", 1},
		{"matches a phrase", 1001, "are sentient", 2},
		{"misses unknown text", 1001, "walrus", 0},
		{"ignores a blank query", 1001, "   ", 0},
		{"stays inside the user's quotes", 2002, "ferret", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, found, err := repo.Search(ctx, tc.user, tc.query)

			require.NoError(t, err)
			assert.Equal(t, tc.number != 0, found)
			if found {
				assert.Equal(t, tc.number, got.Number)
			}
		})
	}
}

func TestQuoteUpdate(t *testing.T) {
	repo := setupQuotes(t)
	ctx := context.Background()
	saved := addQuotes(t, repo, 1001, "teh bagels")[0]

	got, found, err := repo.Update(ctx, 1001, saved.Number, repository.QuoteUpdate{Text: "  the bagels  "})
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "the bagels", got.Text)
	assert.Equal(t, saved.Number, got.Number)
	assert.Equal(t, saved.CreatedAt, got.CreatedAt)

	chosen := time.Date(2025, time.December, 24, 12, 0, 0, 0, time.UTC)
	got, found, err = repo.Update(ctx, 1001, saved.Number, repository.QuoteUpdate{Text: "the bagels", CreatedAt: chosen})
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "2025-12-24T12:00:00Z", got.CreatedAt)

	for _, tc := range []struct {
		name   string
		user   uint64
		number uint64
		text   string
		found  bool
		err    error
	}{
		{"misses an unknown number", 1001, 99, "nope", false, nil},
		{"misses another user's number", 2002, saved.Number, "nope", false, nil},
		{"rejects blank text", 1001, saved.Number, "   ", false, repository.ErrQuoteEmpty},
		{"rejects text over the limit", 1001, saved.Number, strings.Repeat("x", repository.QuoteTextMaxLen+1), false, repository.ErrQuoteTooLong},
	} {
		_, found, err := repo.Update(ctx, tc.user, tc.number, repository.QuoteUpdate{Text: tc.text})
		assert.Equal(t, tc.found, found, tc.name)
		assert.ErrorIs(t, err, tc.err, tc.name)
	}
}

func TestQuoteRemoveLeavesHole(t *testing.T) {
	repo := setupQuotes(t)
	ctx := context.Background()
	addQuotes(t, repo, 1001, "one", "two", "three")

	found, err := repo.Remove(ctx, 1001, 2)
	require.NoError(t, err)
	assert.True(t, found)
	_, found, err = repo.Get(ctx, 1001, 2)
	require.NoError(t, err)
	assert.False(t, found)

	next := addQuotes(t, repo, 1001, "four")[0]
	assert.Equal(t, uint64(4), next.Number)

	found, err = repo.Remove(ctx, 1001, 2)
	require.NoError(t, err)
	assert.False(t, found)
}

func TestQuoteDeleteAllForUser(t *testing.T) {
	repo := setupQuotes(t)
	ctx := context.Background()
	addQuotes(t, repo, 1001, "mine")
	kept := addQuotes(t, repo, 2002, "theirs")[0]

	require.NoError(t, repo.DeleteAllForUser(ctx, 1001))

	_, found, err := repo.Get(ctx, 1001, 1)
	require.NoError(t, err)
	assert.False(t, found)
	_, found, err = repo.Get(ctx, 2002, kept.Number)
	require.NoError(t, err)
	assert.True(t, found)
}
