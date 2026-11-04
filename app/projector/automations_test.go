// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"sort"
	"testing"

	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/projection"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type fakeOwner struct {
	automations []automation.Automation
	reads       int
}

func (f *fakeOwner) Automations(context.Context, uint64) ([]automation.Automation, error) {
	f.reads++
	return f.automations, nil
}

func automationIDs(list []automation.Automation) []string {
	ids := make([]string, 0, len(list))
	for _, a := range list {
		ids = append(ids, a.ID)
	}
	sort.Strings(ids)
	return ids
}

func TestAutomationChangesHydrateOnceThenApplyEachChange(t *testing.T) {
	store := projection.NewStore(testValkey(t))
	owner := &fakeOwner{automations: []automation.Automation{{ID: "a1", UserID: 7, Name: "one"}, {ID: "a2", UserID: 7, Name: "two"}}}
	p := NewProjector(Deps{Store: store, Owner: owner, Log: zap.NewNop()})
	ctx := t.Context()

	require.NoError(t, p.HandleAutomationChanged(message(t, "m1", data.AutomationChangedDTO{UserID: 7, ID: "a2", Automation: &automation.Automation{ID: "a2", UserID: 7, Name: "two"}})))
	list, projected, err := store.GetAutomations(ctx, 7)
	require.NoError(t, err)
	assert.True(t, projected)
	assert.Equal(t, []string{"a1", "a2"}, automationIDs(list))

	require.NoError(t, p.HandleAutomationChanged(message(t, "m2", data.AutomationChangedDTO{UserID: 7, ID: "a3", Automation: &automation.Automation{ID: "a3", UserID: 7, Name: "three"}})))
	require.NoError(t, p.HandleAutomationChanged(message(t, "m3", data.AutomationChangedDTO{UserID: 7, ID: "a1", Deleted: true})))
	list, _, err = store.GetAutomations(ctx, 7)
	require.NoError(t, err)
	assert.Equal(t, []string{"a2", "a3"}, automationIDs(list))
	assert.Equal(t, 1, owner.reads, "only the first change hydrates from the owner")

	require.NoError(t, p.HandleAutomationChanged(message(t, "m4", data.AutomationChangedDTO{UserID: 7})), "an event without an id is dropped")
}
