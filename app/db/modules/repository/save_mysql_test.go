// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"testing"

	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/app/db/modules/repository"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/pkg/bus/bustest"
	"ItsBagelBot/pkg/codec"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestModuleSaveMySQL(t *testing.T) {
	raw := testdb.MySQL(t, "modules_save_")
	client := ent.NewClient(ent.Driver(entsql.OpenDB("mysql", raw)))
	require.NoError(t, client.Schema.Create(t.Context()))
	pub := bustest.NewPublisher()
	repo := repository.NewModules(client, pub, nil, zap.NewNop())
	t.Cleanup(func() { repo.Close(context.Background()) })
	_, err := repo.List(t.Context(), 1001)
	require.NoError(t, err)
	for i, enabled := range []bool{false, true} {
		views, err := repo.SetNow(t.Context(), 1001, "welcome", enabled, codec.RawMessage(`{"message":"saved"}`))
		require.NoError(t, err)
		require.Len(t, views, 1)
		row := client.Modules.Query().OnlyX(t.Context())
		assert.Equal(t, []any{i + 1, enabled, row.Revision}, []any{row.Revision, views[0].IsEnabled, views[0].Revision})
		events := pub.On(data.SubjectModuleChanged)
		require.Len(t, events, i+1)
		var event data.ModuleChangedDTO
		require.NoError(t, codec.Unmarshal(events[i].Payload, &event))
		assert.Equal(t, []any{row.Revision, enabled, string(row.Configs)}, []any{event.Revision, event.IsEnabled, string(event.Configs)})
	}
}
