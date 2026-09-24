// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"testing"

	"ItsBagelBot/app/db/commands/ent/commands"
	"ItsBagelBot/app/db/commands/repository"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestBackfillSetsBumpCounterFromBareToken(t *testing.T) {
	client, pub, repo := setup(t)
	ctx := context.Background()

	require.NoError(t, repo.Upsert(1001, spec("!so", "died {counter:deaths} times", false, 0)))
	repo.Close(ctx)
	baseline := len(pub.On(data.SubjectCommandChanged))

	repo2 := repository.NewCommands(client, pub, nil, zap.NewNop())
	defer repo2.Close(ctx)
	require.NoError(t, repo2.BackfillBumpCounterFromTokens(ctx))

	row := client.Commands.Query().Where(commands.NameEQ("so")).OnlyX(ctx)
	assert.Equal(t, "deaths", row.BumpCounter)

	events := pub.On(data.SubjectCommandChanged)
	require.Len(t, events, baseline+1)
	var dto data.CommandChangedDTO
	require.NoError(t, codec.Unmarshal(events[baseline].Payload, &dto))
	assert.Equal(t, "deaths", dto.BumpCounter)
}

func TestBackfillTakesTheFirstBareTokenInWrittenOrder(t *testing.T) {
	client, pub, repo := setup(t)
	ctx := context.Background()

	require.NoError(t, repo.Upsert(1001, spec("!so", "{counter:wins} and {counter:deaths}", false, 0)))
	repo.Close(ctx)

	repo2 := repository.NewCommands(client, pub, nil, zap.NewNop())
	defer repo2.Close(ctx)
	require.NoError(t, repo2.BackfillBumpCounterFromTokens(ctx))

	row := client.Commands.Query().Where(commands.NameEQ("so")).OnlyX(ctx)
	assert.Equal(t, "wins", row.BumpCounter)
}

func TestBackfillSkipsTargetAddressedCounters(t *testing.T) {
	client, pub, repo := setup(t)
	ctx := context.Background()

	require.NoError(t, repo.Upsert(1001, spec("!so", "{counter:target:shutups} times", false, 0)))
	repo.Close(ctx)
	baseline := len(pub.On(data.SubjectCommandChanged))

	repo2 := repository.NewCommands(client, pub, nil, zap.NewNop())
	defer repo2.Close(ctx)
	require.NoError(t, repo2.BackfillBumpCounterFromTokens(ctx))

	row := client.Commands.Query().Where(commands.NameEQ("so")).OnlyX(ctx)
	assert.Empty(t, row.BumpCounter)
	assert.Len(t, pub.On(data.SubjectCommandChanged), baseline, "an untouched row is never announced")
}

func TestBackfillSkipsAddressedThenUsesALaterBareToken(t *testing.T) {
	client, pub, repo := setup(t)
	ctx := context.Background()

	require.NoError(t, repo.Upsert(1001, spec("!so", "{counter:target:shutups} then {counter:deaths}", false, 0)))
	repo.Close(ctx)

	repo2 := repository.NewCommands(client, pub, nil, zap.NewNop())
	defer repo2.Close(ctx)
	require.NoError(t, repo2.BackfillBumpCounterFromTokens(ctx))

	row := client.Commands.Query().Where(commands.NameEQ("so")).OnlyX(ctx)
	assert.Equal(t, "deaths", row.BumpCounter)
}

func TestBackfillLeavesAnAlreadySetOptionAlone(t *testing.T) {
	client, pub, repo := setup(t)
	ctx := context.Background()

	s := spec("!so", "{counter:deaths} times", false, 0)
	s.BumpCounter = "hugs"
	require.NoError(t, repo.Upsert(1001, s))
	repo.Close(ctx)

	repo2 := repository.NewCommands(client, pub, nil, zap.NewNop())
	defer repo2.Close(ctx)
	require.NoError(t, repo2.BackfillBumpCounterFromTokens(ctx))

	row := client.Commands.Query().Where(commands.NameEQ("so")).OnlyX(ctx)
	assert.Equal(t, "hugs", row.BumpCounter, "an existing choice is never overwritten")
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
