// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"encoding/json"
	"testing"

	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/app/db/modules/ent/enttest"
	"ItsBagelBot/app/db/modules/repository"
	"ItsBagelBot/internal/domain/automation"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	automationrpc "ItsBagelBot/internal/domain/rpc/automation"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/pkg/bus/bustest"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const manualDef = `{"version":1,"trigger":{"type":"manual","params":{}},"actions":[{"type":"chat","params":{"text":"hi"}}]}`

func newAutomationsRPC(t *testing.T, name testdb.Name) *automationsRPC {
	t.Helper()
	client := testdb.Open(t, name, func(d, dsn string) *ent.Client { return enttest.Open(t, d, dsn) })
	return &automationsRPC{repo: repository.NewAutomations(client, bustest.NewPublisher(), zap.NewNop())}
}

func TestAutomationSummaryAndToggleVerbs(t *testing.T) {
	a := newAutomationsRPC(t, "rpc-automation-summaries")
	ctx := t.Context()
	created, err := a.handleUpsert(ctx, automationrpc.UpsertRequest{Name: "first", Enabled: true, Definition: json.RawMessage(manualDef), Preview: `{"n":[]}`}, 1001)
	require.NoError(t, err)
	require.Empty(t, created.Code)

	listed, err := a.handleSummaries(ctx, automationrpc.ListRequest{}, 1001)
	require.NoError(t, err)
	require.Len(t, listed.Automations, 1)
	require.Equal(t, `{"n":[]}`, listed.Automations[0].Preview)

	toggled, err := a.handleSetEnabled(ctx, automationrpc.SetEnabledRequest{ID: created.Automation.ID, Enabled: false}, 1001)
	require.NoError(t, err)
	require.False(t, toggled.Automation.Enabled)
	require.Equal(t, created.Automation.Revision+1, toggled.Automation.Revision)

	stale := int64(99)
	conflict, err := a.handleSetEnabled(ctx, automationrpc.SetEnabledRequest{ID: created.Automation.ID, Enabled: true, ExpectedRevision: &stale}, 1001)
	require.NoError(t, err)
	require.True(t, conflict.Conflict)

	missing, err := a.handleSetEnabled(ctx, automationrpc.SetEnabledRequest{ID: "nope", Enabled: true}, 1001)
	require.NoError(t, err)
	require.Equal(t, domainrpc.CodeNotFound, missing.Code)
}

func TestAutomationVerbsRoundTrip(t *testing.T) {
	a := newAutomationsRPC(t, "rpc-automation-roundtrip")
	ctx := t.Context()

	created, err := a.handleUpsert(ctx, automationrpc.UpsertRequest{Name: "first", Enabled: true, Definition: json.RawMessage(manualDef)}, 1001)
	require.NoError(t, err)
	require.Empty(t, created.Code)
	require.NotNil(t, created.Automation)
	require.Equal(t, int64(1), created.Automation.Revision)

	got, err := a.handleGet(ctx, automationrpc.GetRequest{ID: created.Automation.ID}, 1001)
	require.NoError(t, err)
	require.Equal(t, "first", got.Automation.Name)

	missing, err := a.handleGet(ctx, automationrpc.GetRequest{ID: "nope"}, 1001)
	require.NoError(t, err)
	require.Equal(t, domainrpc.CodeNotFound, missing.Code)

	listed, err := a.handleList(ctx, automationrpc.ListRequest{}, 1001)
	require.NoError(t, err)
	require.Len(t, listed.Automations, 1)

	projected, err := a.handleProjection(ctx, automationrpc.ProjectionRequest{}, 1001)
	require.NoError(t, err)
	require.Len(t, projected.Automations, 1)

	deleted, err := a.handleDelete(ctx, automationrpc.DeleteRequest{ID: created.Automation.ID}, 1001)
	require.NoError(t, err)
	require.True(t, deleted.Deleted)
}

func TestAutomationUpsertRefusals(t *testing.T) {
	a := newAutomationsRPC(t, "rpc-automation-refusals")
	ctx := t.Context()
	created, err := a.handleUpsert(ctx, automationrpc.UpsertRequest{Name: "taken", Definition: json.RawMessage(manualDef)}, 1001)
	require.NoError(t, err)

	dup, err := a.handleUpsert(ctx, automationrpc.UpsertRequest{Name: "TAKEN", Definition: json.RawMessage(manualDef)}, 1001)
	require.NoError(t, err)
	require.Equal(t, domainrpc.CodeConflict, dup.Code)
	require.True(t, dup.Conflict)
	require.Equal(t, []automation.Issue{{Path: "name", Code: "duplicate"}}, dup.Issues)

	stale := int64(9)
	conflict, err := a.handleUpsert(ctx, automationrpc.UpsertRequest{ID: created.Automation.ID, Name: "taken", Definition: json.RawMessage(manualDef), ExpectedRevision: &stale}, 1001)
	require.NoError(t, err)
	require.Equal(t, domainrpc.CodeConflict, conflict.Code)
	require.True(t, conflict.Conflict)

	invalid, err := a.handleUpsert(ctx, automationrpc.UpsertRequest{Name: "broken", Definition: json.RawMessage(`{`)}, 1001)
	require.NoError(t, err)
	require.Equal(t, domainrpc.CodeInvalid, invalid.Code)
	require.Contains(t, invalid.Issues, automation.Issue{Path: "", Code: "invalid_json"})

	unknown, err := a.handleUpsert(ctx, automationrpc.UpsertRequest{ID: "nope", Name: "x", Definition: json.RawMessage(manualDef)}, 1001)
	require.NoError(t, err)
	require.Equal(t, domainrpc.CodeNotFound, unknown.Code)
}

func TestAutomationValidateVerb(t *testing.T) {
	a := newAutomationsRPC(t, "rpc-automation-validate")
	ok, err := a.handleValidate(t.Context(), automationrpc.ValidateRequest{Name: "fine", Definition: json.RawMessage(manualDef)}, 1001)
	require.NoError(t, err)
	require.Empty(t, ok.Issues)
	require.Empty(t, ok.Code)

	bad, err := a.handleValidate(t.Context(), automationrpc.ValidateRequest{Name: "fine", Definition: json.RawMessage(`[`)}, 1001)
	require.NoError(t, err)
	require.NotEmpty(t, bad.Issues)
}
