// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"ItsBagelBot/app/db/discord/ent/enttest"
	"ItsBagelBot/app/db/discord/repository"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	_ "github.com/mattn/go-sqlite3"
)

func TestUserDeletedEventErasesOnlyThatIncarnationsGuilds(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:discordconsumer?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })
	repo := repository.New(client, dialect.SQLite)
	ctx := context.Background()
	require.NoError(t, repo.BindingSet(ctx, repository.BindParams{GuildID: "g1", BroadcasterID: 42}))
	handle := eraseDeletedAccount(repo, zap.NewNop())

	deliver := func(dto any) error {
		payload, err := codec.Marshal(dto)
		require.NoError(t, err)
		return handle(bus.NewMessage("m", payload))
	}

	require.NoError(t, deliver(data.UserDeletedDTO{UserID: 42, AccountCreatedAt: 1, DeletedAt: time.Now().Add(-time.Hour)}))
	_, found, err := repo.BindingGet(ctx, "g1")
	require.NoError(t, err)
	assert.True(t, found, "a redelivered deletion must not remove a re-registered account's binding")

	require.NoError(t, deliver(data.UserDeletedDTO{UserID: 42, AccountCreatedAt: 1, DeletedAt: time.Now()}))
	_, found, err = repo.BindingGet(ctx, "g1")
	require.NoError(t, err)
	assert.False(t, found)

	assert.NoError(t, handle(bus.NewMessage("bad", []byte("{"))), "an undecodable payload is acknowledged, not retried")
	assert.NoError(t, deliver(data.UserDeletedDTO{DeletedAt: time.Now()}), "an event without a user is acknowledged, not retried")
}
