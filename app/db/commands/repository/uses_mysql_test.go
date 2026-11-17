// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"testing"

	"ItsBagelBot/app/db/commands/ent"
	"ItsBagelBot/internal/testdb"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/stretchr/testify/require"
)

func TestCommandUseGroupsMySQL(t *testing.T) {
	raw := testdb.MySQL(t, "commands_uses_")
	trace := &useSQLTrace{}
	client := ent.NewClient(ent.Driver(entsql.OpenDB("mysql", raw)), ent.Debug(), ent.Log(trace.log))
	require.NoError(t, client.Schema.Create(t.Context()))
	testCommandUseGroups(t, client, trace, 2)
}
