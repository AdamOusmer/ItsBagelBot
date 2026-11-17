// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/db/commands/ent"
	"ItsBagelBot/app/db/commands/repository"
	"ItsBagelBot/internal/analytics/analyticstest"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/bus/bustest"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestCustomCommandCountersFollowCreatesAndRowsDeleted(t *testing.T) {
	client, _, repo := setup(t)
	capture := analyticstest.New(t)
	repo.SetAnalytics(capture.Recorder)
	ctx := t.Context()
	for _, name := range []string{"!one", "!two", "!three", "!one"} {
		require.NoError(t, repo.Upsert(1, spec(name, "r", false, 0)))
	}
	repo.Close(ctx)
	for _, name := range []string{"!one", "!one"} {
		require.NoError(t, repo.Delete(ctx, 1, name))
	}
	require.NoError(t, repo.DeleteAllForUser(ctx, 1, time.Now().Add(time.Hour)))
	require.NoError(t, repo.DeleteAllForUser(ctx, 1, time.Now().Add(time.Hour)))
	require.Equal(t, map[string]int64{"custom.commands|created": 3, "custom.commands|deleted": 3}, capture.Counters())
	next := repository.NewCommands(client, bustest.NewPublisher(), nil, zap.NewNop())
	next.SetAnalytics(capture.Recorder)
	defer next.Close(ctx)
	for _, name := range []string{"!four", "!four"} {
		_, err := next.Restore(ctx, 1, spec(name, "r", false, 0), 0)
		require.NoError(t, err)
	}
	require.NoError(t, next.Rename(ctx, 1, "!four", spec("!five", "changed", false, 0)))
	require.Equal(t, map[string]int64{"custom.commands|created": 4, "custom.commands|deleted": 3}, capture.Counters())
}

func TestCustomCommandBatchCountsOnlyNewCommittedRows(t *testing.T) {
	cases := []struct {
		name  string
		items []string
	}{
		{"committed batch", []string{"!existing", "!new"}},
		{"rejected edit", []string{"!existing", "!new", "!reject"}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			client, pub, repo := setup(t)
			capture := analyticstest.New(t)
			repo.SetAnalytics(capture.Recorder)
			client.Commands.Create().SetUserID(1).SetName("existing").SetResponse("old").ExecX(t.Context())
			client.Commands.Use(func(next ent.Mutator) ent.Mutator {
				return ent.MutateFunc(func(ctx context.Context, mutation ent.Mutation) (ent.Value, error) {
					name, _ := mutation.(*ent.CommandsMutation).Name()
					if name == "reject" {
						mutation.(*ent.CommandsMutation).SetResponse("")
					}
					return next.Mutate(ctx, mutation)
				})
			})
			for _, name := range tt.items {
				err := repo.Upsert(1, spec(name, "new", false, 0))
				require.Equal(t, name == "!reject", err != nil, name)
			}
			repo.Close(t.Context())
			require.Equal(t, map[string]int64{"custom.commands|created": 1}, capture.Counters())
			require.Equal(t, 2, client.Commands.Query().CountX(t.Context()))
			require.Len(t, pub.On(data.SubjectCommandChanged), 2)
		})
	}
}

func TestCustomCommandBulkDeleteHonorsCutoff(t *testing.T) {
	client, _, repo := setup(t)
	capture := analyticstest.New(t)
	repo.SetAnalytics(capture.Recorder)
	cutoff := time.Now()
	for i, created := range []time.Time{cutoff.Add(-time.Hour), cutoff, cutoff.Add(time.Hour)} {
		client.Commands.Create().SetUserID(1).SetName([]string{"!old", "!edge", "!new"}[i]).
			SetResponse("r").SetCreatedAt(created).ExecX(t.Context())
	}
	require.NoError(t, repo.DeleteAllForUser(t.Context(), 1, cutoff))
	require.Equal(t, map[string]int64{"custom.commands|deleted": 2}, capture.Counters())
	require.Equal(t, 1, client.Commands.Query().CountX(t.Context()))
}
