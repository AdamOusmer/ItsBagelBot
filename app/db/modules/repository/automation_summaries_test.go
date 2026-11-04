// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"encoding/json"
	"strings"
	"testing"

	"ItsBagelBot/app/db/modules/repository"
	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/rpc"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const previewBlob = `{"n":[[0,0,3,0],[12,0,4,2]],"e":[[0,1,0]]}`

func TestAutomationPreviewOnlyReachesSummaries(t *testing.T) {
	_, pub, repo := setupAutomations(t, "modautopreview")
	ctx := t.Context()

	created, err := repo.Upsert(ctx, 1001, repository.AutomationInput{Name: "one", Enabled: true, Definition: commandDef("!one"), Preview: previewBlob})
	require.NoError(t, err)

	summaries, err := repo.ListSummaries(ctx, 1001)
	require.NoError(t, err)
	require.Len(t, summaries, 1)
	assert.Equal(t, automation.Summary{
		ID: created.ID, Name: "one", Enabled: true, Revision: 1,
		TriggerType: "command", Preview: previewBlob, UpdatedAt: summaries[0].UpdatedAt,
	}, summaries[0])

	listed, err := repo.List(ctx, 1001)
	require.NoError(t, err)
	body, err := json.Marshal(listed)
	require.NoError(t, err)
	assert.NotContains(t, string(body), `"n":[[`)
	assert.Contains(t, string(body), "!one")

	events := pub.On(data.SubjectAutomationChanged)
	require.Len(t, events, 1)
	assert.NotContains(t, string(events[0].Payload), "preview")
	assert.NotContains(t, string(events[0].Payload), `"n":[[`)
}

func TestAutomationPreviewIsOpaqueAndCapped(t *testing.T) {
	_, _, repo := setupAutomations(t, "modautopreviewcap")
	ctx := t.Context()

	_, err := repo.Upsert(ctx, 1001, repository.AutomationInput{Name: "fits", Definition: commandDef("!fits"), Preview: strings.Repeat("x", automation.MaxPreviewBytes)})
	require.NoError(t, err, "the service does not parse the preview")

	_, err = repo.Upsert(ctx, 1001, repository.AutomationInput{Name: "big", Definition: commandDef("!big"), Preview: strings.Repeat("x", automation.MaxPreviewBytes+1)})
	refused := refusalOf(t, err)
	assert.Equal(t, rpc.CodeInvalid, refused.Code)
	assert.Equal(t, []automation.Issue{{Path: "preview", Code: "too_long"}}, refused.Issues)
}

func TestAutomationSaveWithoutPreviewClearsIt(t *testing.T) {
	_, _, repo := setupAutomations(t, "modautopreviewclear")
	ctx := t.Context()
	created, err := repo.Upsert(ctx, 1001, repository.AutomationInput{Name: "one", Definition: commandDef("!one"), Preview: previewBlob})
	require.NoError(t, err)

	_, err = repo.Upsert(ctx, 1001, repository.AutomationInput{ID: created.ID, Name: "one", Definition: commandDef("!two")})
	require.NoError(t, err)

	summaries, err := repo.ListSummaries(ctx, 1001)
	require.NoError(t, err)
	assert.Empty(t, summaries[0].Preview)
}

func TestAutomationSetEnabledTouchesOnlyEnabledAndRevision(t *testing.T) {
	client, pub, repo := setupAutomations(t, "modautotoggle")
	ctx := t.Context()
	created, err := repo.Upsert(ctx, 1001, repository.AutomationInput{Name: "one", Enabled: true, Definition: commandDef("!one"), Preview: previewBlob})
	require.NoError(t, err)
	before := client.Automation.GetX(ctx, created.ID)

	rev := created.Revision
	off, err := repo.SetEnabled(ctx, 1001, repository.EnabledChange{ID: created.ID, ExpectedRevision: &rev})
	require.NoError(t, err)
	assert.False(t, off.Enabled)
	assert.Equal(t, created.Revision+1, off.Revision)
	assert.Equal(t, created.Definition, off.Definition)

	after := client.Automation.GetX(ctx, created.ID)
	assert.False(t, after.Enabled)
	assert.Equal(t, before.Revision+1, after.Revision)
	assert.Equal(t, before.Definition, after.Definition)
	assert.Equal(t, before.Name, after.Name)
	assert.Equal(t, before.Preview, after.Preview)
	assert.Equal(t, before.UpdatedAt, after.UpdatedAt)
	assert.Len(t, pub.On(data.SubjectAutomationChanged), 2)
}

func TestAutomationSetEnabledToSameValueWritesNothing(t *testing.T) {
	_, pub, repo := setupAutomations(t, "modautotogglesame")
	ctx := t.Context()
	created, err := repo.Upsert(ctx, 1001, repository.AutomationInput{Name: "one", Enabled: true, Definition: commandDef("!one")})
	require.NoError(t, err)

	same, err := repo.SetEnabled(ctx, 1001, repository.EnabledChange{ID: created.ID, Enabled: true})
	require.NoError(t, err)
	assert.Equal(t, created.Revision, same.Revision)
	assert.Len(t, pub.On(data.SubjectAutomationChanged), 1)
}

func TestAutomationSetEnabledRefusals(t *testing.T) {
	_, _, repo := setupAutomations(t, "modautotogglerefuse")
	ctx := t.Context()
	created, err := repo.Upsert(ctx, 1001, repository.AutomationInput{Name: "one", Enabled: true, Definition: commandDef("!one")})
	require.NoError(t, err)

	stale := created.Revision + 5
	_, err = repo.SetEnabled(ctx, 1001, repository.EnabledChange{ID: created.ID, ExpectedRevision: &stale})
	require.ErrorIs(t, err, repository.ErrRevisionConflict)

	_, err = repo.SetEnabled(ctx, 2002, repository.EnabledChange{ID: created.ID})
	require.ErrorIs(t, err, repository.ErrAutomationNotFound)

	_, err = repo.SetEnabled(ctx, 1001, repository.EnabledChange{ID: "missing"})
	require.ErrorIs(t, err, repository.ErrAutomationNotFound)
}
