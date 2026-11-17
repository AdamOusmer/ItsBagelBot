// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"testing"

	"ItsBagelBot/app/db/modules/repository"
	"ItsBagelBot/pkg/bus"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestModuleCleanupEvictsEveryBoundRepository(t *testing.T) {
	for _, account := range []bool{false, true} {
		t.Run(map[bool]string{false: "all rows", true: "account incarnation"}[account], func(t *testing.T) {
			client, pub, unused := setup(t)
			t.Cleanup(func() { unused.Close(t.Context()) })
			h := bus.NewInvalidations(nil, "", nil)
			bound := h.Publisher(pub)
			repo := repository.NewModules(client, bound, nil, zap.NewNop())
			t.Cleanup(func() { repo.Close(t.Context()) })
			reader := repository.NewModules(client, bound, nil, zap.NewNop())
			t.Cleanup(func() { reader.Close(t.Context()) })
			repo.SetAccountInstanceResolver(func(context.Context, uint64) (int64, error) { return 0, nil })
			quotes := repository.NewQuotes(client, bound, zap.NewNop())
			t.Cleanup(quotes.Close)
			client.Modules.Create().SetUserID(1001).SetName("welcome").SetConfigs([]byte(`{}`)).SaveX(t.Context())
			client.Quote.Create().SetUserID(1001).SetNumber(1).SetText("hello").SetAddedBy("viewer").SaveX(t.Context())
			primeDeletedContent(t, reader, quotes)
			if account {
				require.NoError(t, repo.DeleteAccount(t.Context(), 1001, 100))
			} else {
				require.NoError(t, repo.DeleteAllForUser(t.Context(), 1001))
				require.NoError(t, quotes.DeleteAllForUser(t.Context(), 1001))
			}
			modules, err := reader.List(t.Context(), 1001)
			require.NoError(t, err)
			require.Empty(t, modules)
			rows, err := quotes.List(t.Context(), 1001)
			require.NoError(t, err)
			require.Empty(t, rows)
		})
	}
}

func primeDeletedContent(t *testing.T, repo *repository.Modules, quotes *repository.Quotes) {
	t.Helper()
	views, err := repo.List(t.Context(), 1001)
	require.NoError(t, err)
	require.Len(t, views, 1)
	rows, err := quotes.List(t.Context(), 1001)
	require.NoError(t, err)
	require.Len(t, rows, 1)
}

func TestQuoteDeleteEvictsOtherRepository(t *testing.T) {
	client, pub, _ := setupQuotesWithStore(t)
	h := bus.NewInvalidations(nil, "", nil)
	bound := h.Publisher(pub)
	writer := repository.NewQuotes(client, bound, zap.NewNop())
	t.Cleanup(writer.Close)
	reader := repository.NewQuotes(client, bound, zap.NewNop())
	t.Cleanup(reader.Close)
	_, err := writer.Add(t.Context(), 1001, repository.QuoteDraft{Text: "hello", AddedBy: "viewer"})
	require.NoError(t, err)
	rows, err := reader.List(t.Context(), 1001)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.NoError(t, writer.DeleteAllForUser(t.Context(), 1001))
	rows, err = reader.List(t.Context(), 1001)
	require.NoError(t, err)
	require.Empty(t, rows)
}
