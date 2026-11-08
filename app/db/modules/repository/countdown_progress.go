// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
package repository

import (
	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/app/db/modules/ent/countdowndelivery"
	"ItsBagelBot/app/db/modules/ent/predicate"
	"context"
	"time"
)

func countdownLeasePredicates(d CountdownDelivery) []predicate.CountdownDelivery {
	return []predicate.CountdownDelivery{countdowndelivery.UserID(d.UserID), countdowndelivery.Name(d.Name), countdowndelivery.Generation(d.Generation), countdowndelivery.ExpiresAtMs(d.ExpiresAtMS), countdowndelivery.LeaseToken(d.Token), countdowndelivery.LeaseUntilMsGT(time.Now().UnixMilli())}
}

// CountdownProgress records a monotonic checkpoint only while the delivery lease is held.
func (r *RecipeState) CountdownProgress(ctx context.Context, req CountdownProgressRequest) (bool, error) {
	if err := req.validate(); err != nil {
		return false, err
	}
	var found bool
	err := r.countdownTx(ctx, func(ctx context.Context, tx *ent.Tx) error {
		found = false
		var err error
		found, err = r.saveCountdownProgress(ctx, tx, req)
		return err
	})
	return found, err
}
func (r *RecipeState) saveCountdownProgress(ctx context.Context, tx *ent.Tx, req CountdownProgressRequest) (bool, error) {
	query := tx.CountdownDelivery.Query().Where(countdownLeasePredicates(req.Delivery)...)
	if r.dialect != "sqlite3" {
		query.ForUpdate()
	}
	row, err := query.Only(ctx)
	if ent.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	progress, err := nextCountdownProgress(row.Progress, req)
	if err != nil {
		return false, err
	}
	n, err := tx.CountdownDelivery.Update().Where(countdowndelivery.ID(row.ID), countdowndelivery.LeaseToken(req.Delivery.Token), countdowndelivery.LeaseUntilMsGT(time.Now().UnixMilli())).SetProgress(progress).Save(ctx)
	return n == 1, err
}
func nextCountdownProgress(progress map[string]string, req CountdownProgressRequest) (map[string]string, error) {
	if progress == nil {
		progress = make(map[string]string)
	}
	if _, exists := progress[req.Key]; !exists && len(progress) >= 1024 {
		return nil, ErrVariableLimit
	}
	if progress[req.Key] != "completed" {
		progress[req.Key] = req.Status
	}
	return progress, nil
}
