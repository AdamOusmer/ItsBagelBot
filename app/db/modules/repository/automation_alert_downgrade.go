// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"strings"

	"ItsBagelBot/app/db/modules/ent"
	entautomation "ItsBagelBot/app/db/modules/ent/automation"
	"ItsBagelBot/app/db/modules/ent/modules"
	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/rpc"
	"ItsBagelBot/pkg/codec"
)

type AlertDowngrade struct {
	EnableKey         string
	Message           string
	Enabled           bool
	ExpectedRevision  *int64
	ExpectedModuleRev *int
}

type alertDowngradeWrite struct {
	userID uint64
	id     string
	alert  AlertDowngrade
}

// DowngradeToAlert commits the alert line and the row delete together, so a channel
// never has both firing nor neither.
func (a *Automations) DowngradeToAlert(ctx context.Context, userID uint64, id string, alert AlertDowngrade) error {
	if !automation.IsAlertEnableKey(alert.EnableKey) {
		return &AutomationRefusal{Code: rpc.CodeInvalid, Issues: []automation.Issue{{Path: "enable_key", Code: automation.CodeInvalidEnum}}}
	}
	flip, err := a.commitAlertDowngrade(ctx, alertDowngradeWrite{userID: userID, id: id, alert: alert})
	if err != nil {
		return err
	}
	a.announce(ctx, data.AutomationChangedDTO{UserID: userID, ID: id, Deleted: true})
	a.announceAlertFlip(ctx, userID, flip)
	return nil
}

func (a *Automations) commitAlertDowngrade(ctx context.Context, w alertDowngradeWrite) (*alertFlip, error) {
	tx, err := a.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	row, err := loadOwnedAutomation(ctx, tx, w.userID, AutomationInput{ID: w.id, ExpectedRevision: w.alert.ExpectedRevision})
	if err != nil {
		return nil, err
	}
	flip, err := writeAlertLine(ctx, tx, w.userID, w.alert)
	if err != nil {
		return nil, err
	}
	deleted, err := tx.Automation.Delete().Where(entautomation.ID(row.ID), entautomation.Revision(row.Revision)).Exec(ctx)
	if err != nil {
		return nil, err
	}
	if deleted == 0 {
		return nil, ErrRevisionConflict
	}
	return flip, tx.Commit()
}

func alertLinePatch(alert AlertDowngrade) (map[string]codec.RawMessage, error) {
	message, err := codec.Marshal(alert.Message)
	if err != nil {
		return nil, err
	}
	state := codec.RawMessage(`"off"`)
	if alert.Enabled {
		state = codec.RawMessage(`"on"`)
	}
	return map[string]codec.RawMessage{
		alert.EnableKey: state,
		strings.TrimSuffix(alert.EnableKey, "Enabled") + "Message": message,
	}, nil
}

func writeAlertLine(ctx context.Context, tx *ent.Tx, userID uint64, alert AlertDowngrade) (*alertFlip, error) {
	row, err := tx.Modules.Query().
		Where(modules.UserIDEQ(userID), modules.NameEQ(automation.AlertsModuleName)).
		Only(ctx)
	if ent.IsNotFound(err) {
		return insertAlertLine(ctx, tx, userID, alert)
	}
	if err != nil {
		return nil, err
	}
	if err := moduleRevMatches(alert.ExpectedModuleRev, row.Revision); err != nil {
		return nil, err
	}
	return updateAlertLine(ctx, tx, row, alert)
}

func insertAlertLine(ctx context.Context, tx *ent.Tx, userID uint64, alert AlertDowngrade) (*alertFlip, error) {
	if err := moduleRevMatches(alert.ExpectedModuleRev, 0); err != nil {
		return nil, err
	}
	patch, err := alertLinePatch(alert)
	if err != nil {
		return nil, err
	}
	blob, err := mergedBlob(map[string]codec.RawMessage{}, patch, 1)
	if err != nil {
		return nil, err
	}
	_, err = tx.Modules.Create().
		SetUserID(userID).SetName(automation.AlertsModuleName).SetIsEnabled(alert.Enabled).
		SetConfigs(blob).SetRevision(1).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	return &alertFlip{enabled: alert.Enabled, blob: blob}, nil
}

func updateAlertLine(ctx context.Context, tx *ent.Tx, row *ent.Modules, alert AlertDowngrade) (*alertFlip, error) {
	patch, err := alertLinePatch(alert)
	if err != nil {
		return nil, err
	}
	blob, err := mergedBlob(decodeConfig(row.Configs), patch, row.Revision+1)
	if err != nil {
		return nil, err
	}
	enabled := row.IsEnabled || alert.Enabled
	affected, err := tx.Modules.Update().
		Where(modules.IDEQ(row.ID), modules.RevisionEQ(row.Revision)).
		SetIsEnabled(enabled).SetConfigs(blob).AddRevision(1).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, ErrRevisionConflict
	}
	return &alertFlip{enabled: enabled, blob: blob}, nil
}
