// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"strings"

	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/app/db/modules/ent/modules"
	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/rpc"
	"ItsBagelBot/pkg/codec"
)

type AlertUpgrade struct {
	EnableKey         string
	ExpectedModuleRev *int
}

type alertFlip struct {
	enabled bool
	blob    []byte
}

type alertUpgradeWrite struct {
	userID uint64
	input  AutomationInput
	def    automation.Definition
	alert  AlertUpgrade
}

func (a *Automations) BindModules(m *Modules) { a.modules = m }

// UpgradeAlert commits the alert switching off and the automation insert together,
// so a channel never has both firing nor neither.
func (a *Automations) UpgradeAlert(ctx context.Context, userID uint64, in AutomationInput, alert AlertUpgrade) (automation.Automation, error) {
	in.ID, in.ExpectedRevision = "", nil
	in.Name = strings.TrimSpace(in.Name)
	def, issues := parseAutomationInput(in)
	if !automation.IsAlertEnableKey(alert.EnableKey) {
		issues = append(issues, automation.Issue{Path: "enable_key", Code: automation.CodeInvalidEnum})
	}
	if len(issues) > 0 {
		return automation.Automation{}, &AutomationRefusal{Code: rpc.CodeInvalid, Issues: issues}
	}
	row, flip, err := a.commitAlertUpgrade(ctx, alertUpgradeWrite{userID: userID, input: in, def: def, alert: alert})
	if err != nil {
		return automation.Automation{}, mapAutomationConstraint(err)
	}
	view, err := automationView(row)
	if err != nil {
		return automation.Automation{}, err
	}
	a.announce(ctx, data.AutomationChangedDTO{UserID: userID, ID: view.ID, Automation: &view})
	a.announceAlertFlip(ctx, userID, flip)
	return view, nil
}

func (a *Automations) commitAlertUpgrade(ctx context.Context, w alertUpgradeWrite) (*ent.Automation, *alertFlip, error) {
	tx, err := a.client.Tx(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	flip, err := turnAlertOff(ctx, tx, w.userID, w.alert)
	if err != nil {
		return nil, nil, err
	}
	w.input.lockChannelCap = a.rowLocks
	row, err := writeAutomation(ctx, tx, automationDraft{userID: w.userID, in: w.input, def: w.def})
	if err != nil {
		return nil, nil, err
	}
	return row, flip, tx.Commit()
}

func turnAlertOff(ctx context.Context, tx *ent.Tx, userID uint64, alert AlertUpgrade) (*alertFlip, error) {
	row, err := tx.Modules.Query().
		Where(modules.UserIDEQ(userID), modules.NameEQ(automation.AlertsModuleName)).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, moduleRevMatches(alert.ExpectedModuleRev, 0)
	}
	if err != nil {
		return nil, err
	}
	if err := moduleRevMatches(alert.ExpectedModuleRev, row.Revision); err != nil {
		return nil, err
	}
	return writeAlertOff(ctx, tx, row, alert.EnableKey)
}

func moduleRevMatches(expected *int, current int) error {
	if expected != nil && *expected != current {
		return ErrRevisionConflict
	}
	return nil
}

func writeAlertOff(ctx context.Context, tx *ent.Tx, row *ent.Modules, enableKey string) (*alertFlip, error) {
	blob, err := mergedBlob(decodeConfig(row.Configs), map[string]codec.RawMessage{enableKey: codec.RawMessage(`"off"`)}, row.Revision+1)
	if err != nil {
		return nil, err
	}
	affected, err := tx.Modules.Update().
		Where(modules.IDEQ(row.ID), modules.RevisionEQ(row.Revision)).
		SetConfigs(blob).AddRevision(1).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, ErrRevisionConflict
	}
	return &alertFlip{enabled: row.IsEnabled, blob: blob}, nil
}

func (a *Automations) announceAlertFlip(ctx context.Context, userID uint64, flip *alertFlip) {
	if flip != nil && a.modules != nil {
		a.modules.announcePatch(ctx, userID, automation.AlertsModuleName, flip.enabled, flip.blob)
	}
}
