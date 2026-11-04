// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"slices"

	"ItsBagelBot/app/db/modules/ent"
	entautomation "ItsBagelBot/app/db/modules/ent/automation"
	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/internal/domain/event/data"
)

var runtimeColumns = slices.DeleteFunc(slices.Clone(entautomation.Columns), func(c string) bool {
	return c == entautomation.FieldPreview
})

func (a *Automations) ListSummaries(ctx context.Context, userID uint64) ([]automation.Summary, error) {
	rows, err := a.client.Automation.Query().
		Where(entautomation.UserID(userID)).
		Order(ent.Asc(entautomation.FieldNameKey)).
		Select(
			entautomation.FieldID,
			entautomation.FieldName,
			entautomation.FieldEnabled,
			entautomation.FieldRevision,
			entautomation.FieldTriggerType,
			entautomation.FieldPreview,
			entautomation.FieldUpdatedAt,
		).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]automation.Summary, len(rows))
	for i, row := range rows {
		out[i] = automation.Summary{
			ID: row.ID, Name: row.Name, Enabled: row.Enabled, Revision: row.Revision,
			TriggerType: row.TriggerType, UpdatedAt: row.UpdatedAt.UTC(),
		}
		if row.Preview != nil {
			out[i].Preview = *row.Preview
		}
	}
	return out, nil
}

type EnabledChange struct {
	ID               string
	Enabled          bool
	ExpectedRevision *int64
}

// SetEnabled leaves the edit time alone: a toggle is not an edit of the flow.
func (a *Automations) SetEnabled(ctx context.Context, userID uint64, change EnabledChange) (automation.Automation, error) {
	row, err := a.client.Automation.Query().
		Where(entautomation.UserID(userID), entautomation.ID(change.ID)).
		Select(runtimeColumns...).
		Only(ctx)
	if ent.IsNotFound(err) {
		return automation.Automation{}, ErrAutomationNotFound
	}
	if err != nil {
		return automation.Automation{}, err
	}
	if change.ExpectedRevision != nil && *change.ExpectedRevision != row.Revision {
		return automation.Automation{}, ErrRevisionConflict
	}
	if row.Enabled == change.Enabled {
		return automationView(row)
	}
	return a.writeEnabled(ctx, userID, row, change.Enabled)
}

func (a *Automations) writeEnabled(ctx context.Context, userID uint64, row *ent.Automation, enabled bool) (automation.Automation, error) {
	affected, err := a.client.Automation.Update().
		Where(entautomation.ID(row.ID), entautomation.Revision(row.Revision)).
		SetEnabled(enabled).
		SetRevision(row.Revision + 1).
		SetUpdatedAt(row.UpdatedAt).
		Save(ctx)
	if err != nil {
		return automation.Automation{}, err
	}
	if affected == 0 {
		return automation.Automation{}, ErrRevisionConflict
	}
	row.Enabled, row.Revision = enabled, row.Revision+1
	view, err := automationView(row)
	if err != nil {
		return automation.Automation{}, err
	}
	a.announce(ctx, data.AutomationChangedDTO{UserID: userID, ID: view.ID, Automation: &view})
	return view, nil
}
