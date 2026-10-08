// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"testing"

	"ItsBagelBot/app/db/commands/ent/commands"
	"ItsBagelBot/app/db/commands/repository"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/bus/bustest"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func announcedBumpCounters(t *testing.T, pub *bustest.Publisher, after int) []string {
	t.Helper()
	var counters []string
	for _, msg := range pub.On(data.SubjectCommandChanged)[after:] {
		var dto data.CommandChangedDTO
		require.NoError(t, codec.Unmarshal(msg.Payload, &dto))
		counters = append(counters, dto.BumpCounter)
	}
	return counters
}

func TestBackfillSetsBumpCounterFromTheFirstBareToken(t *testing.T) {
	for _, tc := range []struct {
		name      string
		response  string
		preset    string
		want      string
		announced []string
	}{
		{name: "a bare token", response: "died {counter:deaths} times", want: "deaths", announced: []string{"deaths"}},
		{name: "the first bare token in written order", response: "{counter:wins} and {counter:deaths}", want: "wins", announced: []string{"wins"}},
		{name: "target addressed counters are skipped", response: "{counter:target:shutups} times"},
		{name: "a later bare token after an addressed one", response: "{counter:target:shutups} then {counter:deaths}", want: "deaths", announced: []string{"deaths"}},
		{name: "an existing choice is never overwritten", response: "{counter:deaths} times", preset: "hugs", want: "hugs"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, pub, repo := setup(t)
			ctx := context.Background()
			s := spec("!so", tc.response, false, 0)
			s.BumpCounter = tc.preset
			require.NoError(t, repo.Upsert(1001, s))
			repo.Close(ctx)
			baseline := len(pub.On(data.SubjectCommandChanged))

			repo2 := repository.NewCommands(client, pub, nil, zap.NewNop())
			defer repo2.Close(ctx)
			require.NoError(t, repo2.BackfillBumpCounterFromTokens(ctx))

			row := client.Commands.Query().Where(commands.NameEQ("so")).OnlyX(ctx)
			assert.Equal(t, tc.want, row.BumpCounter)
			assert.Equal(t, tc.announced, announcedBumpCounters(t, pub, baseline), "an untouched row is never announced")
		})
	}
}

func TestBackfillRunsExactlyOnce(t *testing.T) {
	client, pub, repo := setup(t)
	ctx := context.Background()

	require.NoError(t, repo.Upsert(1001, spec("!so", "{counter:deaths} times", false, 0)))
	repo.Close(ctx)

	repo2 := repository.NewCommands(client, pub, nil, zap.NewNop())
	require.NoError(t, repo2.BackfillBumpCounterFromTokens(ctx))
	repo2.Close(ctx)

	assert.Equal(t, 1, client.Migrations.Query().CountX(ctx), "the migration records one marker row")

	client.Commands.Update().Where(commands.NameEQ("so")).SetBumpCounter("").SaveX(ctx)

	repo3 := repository.NewCommands(client, pub, nil, zap.NewNop())
	defer repo3.Close(ctx)
	require.NoError(t, repo3.BackfillBumpCounterFromTokens(ctx))

	row := client.Commands.Query().Where(commands.NameEQ("so")).OnlyX(ctx)
	assert.Empty(t, row.BumpCounter, "a second run must not re-apply once the marker exists")
	assert.Equal(t, 1, client.Migrations.Query().CountX(ctx), "still exactly one marker row")
}
