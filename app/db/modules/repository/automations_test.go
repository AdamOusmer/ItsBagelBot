// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"fmt"
	"testing"

	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/app/db/modules/ent/enttest"
	"ItsBagelBot/app/db/modules/repository"
	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/rpc"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/pkg/bus/bustest"
	"ItsBagelBot/pkg/codec"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func setupAutomations(t *testing.T, name testdb.Name) (*ent.Client, *bustest.Publisher, *repository.Automations) {
	t.Helper()
	client := testdb.Open(t, name, func(d, dsn string) *ent.Client { return enttest.Open(t, d, dsn) })
	pub := bustest.NewPublisher()
	return client, pub, repository.NewAutomations(client, pub, zap.NewNop())
}

func commandDef(name string) []byte {
	return []byte(fmt.Sprintf(`{"version":1,"trigger":{"type":"command","params":{"name":%q}},"actions":[{"type":"chat","params":{"text":"hi"}}]}`, name))
}

func runDef(targetID string) []byte {
	return []byte(fmt.Sprintf(`{"version":1,"trigger":{"type":"manual","params":{}},"actions":[{"type":"random_pick","params":{"branches":[{"weight":1,"actions":[{"type":"chat","params":{"text":"a"}}]},{"weight":1,"actions":[{"type":"run","params":{"automation_id":%q}}]}]}}]}`, targetID))
}

func refusalOf(t *testing.T, err error) *repository.AutomationRefusal {
	t.Helper()
	var refused *repository.AutomationRefusal
	require.ErrorAs(t, err, &refused)
	return refused
}

func TestAutomationUpsertCreatesAndRevisions(t *testing.T) {
	_, pub, repo := setupAutomations(t, "modautomationcrud")
	ctx := t.Context()

	created, err := repo.Upsert(ctx, 1001, repository.AutomationInput{Name: "  Greeter ", Enabled: true, Definition: commandDef("!hi")})
	require.NoError(t, err)
	assert.Equal(t, "Greeter", created.Name)
	assert.Equal(t, int64(1), created.Revision)
	assert.NotEmpty(t, created.ID)

	rev := created.Revision
	updated, err := repo.Upsert(ctx, 1001, repository.AutomationInput{ID: created.ID, Name: "Greeter", Definition: commandDef("!hello"), ExpectedRevision: &rev})
	require.NoError(t, err)
	assert.Equal(t, int64(2), updated.Revision)
	assert.False(t, updated.Enabled)

	got, found, err := repo.Get(ctx, 1001, created.ID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, updated.Revision, got.Revision)
	assert.Equal(t, "command", got.Definition.Trigger.Type)

	_, found, err = repo.Get(ctx, 2002, created.ID)
	require.NoError(t, err)
	assert.False(t, found)

	list, err := repo.List(ctx, 1001)
	require.NoError(t, err)
	assert.Len(t, list, 1)

	events := pub.On(data.SubjectAutomationChanged)
	require.Len(t, events, 2)
	var dto data.AutomationChangedDTO
	require.NoError(t, codec.Unmarshal(events[1].Payload, &dto))
	assert.Equal(t, created.ID, dto.ID)
	require.NotNil(t, dto.Automation)
	assert.Equal(t, int64(2), dto.Automation.Revision)
}

func TestAutomationUpsertRevisionConflict(t *testing.T) {
	_, pub, repo := setupAutomations(t, "modautomationrev")
	ctx := t.Context()
	created, err := repo.Upsert(ctx, 1001, repository.AutomationInput{Name: "one", Definition: commandDef("!one")})
	require.NoError(t, err)

	stale := int64(7)
	_, err = repo.Upsert(ctx, 1001, repository.AutomationInput{ID: created.ID, Name: "one", Definition: commandDef("!one"), ExpectedRevision: &stale})
	require.ErrorIs(t, err, repository.ErrRevisionConflict)

	_, err = repo.Upsert(ctx, 1001, repository.AutomationInput{ID: "missing", Name: "one", Definition: commandDef("!one")})
	require.ErrorIs(t, err, repository.ErrAutomationNotFound)
	assert.Len(t, pub.On(data.SubjectAutomationChanged), 1)
}

func TestAutomationUpsertRejectsDuplicates(t *testing.T) {
	_, _, repo := setupAutomations(t, "modautomationdup")
	ctx := t.Context()
	_, err := repo.Upsert(ctx, 1001, repository.AutomationInput{Name: "Alpha", Definition: commandDef("!a")})
	require.NoError(t, err)

	_, err = repo.Upsert(ctx, 1001, repository.AutomationInput{Name: "alpha", Definition: commandDef("!b")})
	refused := refusalOf(t, err)
	assert.Equal(t, rpc.CodeConflict, refused.Code)
	assert.Equal(t, []automation.Issue{{Path: "name", Code: "duplicate"}}, refused.Issues)

	_, err = repo.Upsert(ctx, 2002, repository.AutomationInput{Name: "Alpha", Definition: commandDef("!a")})
	require.NoError(t, err)
}

func TestAutomationUpsertChecksRunTargets(t *testing.T) {
	_, _, repo := setupAutomations(t, "modautomationrun")
	ctx := t.Context()
	target, err := repo.Upsert(ctx, 1001, repository.AutomationInput{Name: "target", Definition: commandDef("!t")})
	require.NoError(t, err)

	_, err = repo.Upsert(ctx, 1001, repository.AutomationInput{Name: "caller", Definition: runDef(target.ID)})
	require.NoError(t, err)

	_, err = repo.Upsert(ctx, 1001, repository.AutomationInput{Name: "broken", Definition: runDef("nope")})
	refused := refusalOf(t, err)
	assert.Equal(t, rpc.CodeInvalid, refused.Code)
	assert.Equal(t, []automation.Issue{{Path: "actions[0].params.branches[1].actions[0].params.automation_id", Code: "unknown_automation"}}, refused.Issues)

	_, err = repo.Upsert(ctx, 2002, repository.AutomationInput{Name: "foreign", Definition: runDef(target.ID)})
	assert.Equal(t, rpc.CodeInvalid, refusalOf(t, err).Code)
}

func TestAutomationUpsertEnforcesChannelCap(t *testing.T) {
	_, _, repo := setupAutomations(t, "modautomationcap")
	ctx := t.Context()
	for i := range automation.MaxAutomationsPerChannel {
		_, err := repo.Upsert(ctx, 1001, repository.AutomationInput{Name: fmt.Sprintf("auto %d", i), Definition: commandDef(fmt.Sprintf("!c%d", i))})
		require.NoError(t, err)
	}
	_, err := repo.Upsert(ctx, 1001, repository.AutomationInput{Name: "overflow", Definition: commandDef("!over")})
	refused := refusalOf(t, err)
	assert.Equal(t, rpc.CodeInvalid, refused.Code)
	assert.Equal(t, []automation.Issue{{Path: "", Code: "too_many"}}, refused.Issues)
}

func TestAutomationUpsertRejectsInvalidDefinition(t *testing.T) {
	_, pub, repo := setupAutomations(t, "modautomationinvalid")
	_, err := repo.Upsert(t.Context(), 1001, repository.AutomationInput{Name: "bad", Definition: []byte(`{not json`)})
	refused := refusalOf(t, err)
	assert.Equal(t, rpc.CodeInvalid, refused.Code)
	assert.Contains(t, refused.Issues, automation.Issue{Path: "", Code: "invalid_json"})
	assert.Empty(t, pub.On(data.SubjectAutomationChanged))
}

func TestAutomationValidateWritesNothing(t *testing.T) {
	client, pub, repo := setupAutomations(t, "modautomationvalidate")
	issues, err := repo.Validate(t.Context(), 1001, "ok", commandDef("!ok"))
	require.NoError(t, err)
	assert.Empty(t, issues)

	issues, err = repo.Validate(t.Context(), 1001, "caller", runDef("nope"))
	require.NoError(t, err)
	assert.Equal(t, []automation.Issue{{Path: "actions[0].params.branches[1].actions[0].params.automation_id", Code: "unknown_automation"}}, issues)
	assert.Zero(t, client.Automation.Query().CountX(t.Context()))
	assert.Empty(t, pub.On(data.SubjectAutomationChanged))
}

func TestAutomationDeletePublishesOnce(t *testing.T) {
	_, pub, repo := setupAutomations(t, "modautomationdelete")
	ctx := t.Context()
	created, err := repo.Upsert(ctx, 1001, repository.AutomationInput{Name: "gone", Definition: commandDef("!gone")})
	require.NoError(t, err)

	deleted, err := repo.Delete(ctx, 2002, created.ID)
	require.NoError(t, err)
	assert.False(t, deleted)

	deleted, err = repo.Delete(ctx, 1001, created.ID)
	require.NoError(t, err)
	assert.True(t, deleted)

	events := pub.On(data.SubjectAutomationChanged)
	require.Len(t, events, 2)
	var dto data.AutomationChangedDTO
	require.NoError(t, codec.Unmarshal(events[1].Payload, &dto))
	assert.True(t, dto.Deleted)
	assert.Nil(t, dto.Automation)
}
