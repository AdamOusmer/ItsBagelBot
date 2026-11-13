// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
package repository

import (
	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/app/db/modules/ent/countdown"
	"ItsBagelBot/app/db/modules/ent/countdowndelivery"
	"ItsBagelBot/pkg/db"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"
)

// CountdownClaim promotes expired countdowns into immutable deliveries, then leases one.
func (r *RecipeState) CountdownClaim(ctx context.Context) (CountdownDelivery, bool, error) {
	if err := r.promoteCountdowns(ctx, time.Now().UnixMilli()); err != nil {
		return CountdownDelivery{}, false, err
	}
	return r.claimCountdownDelivery(ctx, time.Now().UnixMilli())
}
func (r *RecipeState) promoteCountdowns(ctx context.Context, now int64) error {
	rows, err := db.WithRead(ctx, func(ctx context.Context) ([]*ent.Countdown, error) {
		return r.client.Countdown.Query().Where(countdown.ExpiresAtMsLTE(now)).Order(ent.Asc(countdown.FieldExpiresAtMs)).Limit(32).All(ctx)
	})
	if err != nil {
		return err
	}
	for _, row := range rows {
		if err := r.promoteCountdown(ctx, row); err != nil {
			return err
		}
	}
	return nil
}
func (r *RecipeState) promoteCountdown(ctx context.Context, row *ent.Countdown) error {
	return r.countdownTx(ctx, func(ctx context.Context, tx *ent.Tx) error { return promoteCountdownTx(ctx, tx, row) })
}
func promoteCountdownTx(ctx context.Context, tx *ent.Tx, row *ent.Countdown) error {
	if err := lockCountdownChannel(ctx, tx, row.UserID); err != nil {
		return err
	}
	n, err := tx.Countdown.Delete().Where(countdown.ID(row.ID), countdown.Generation(row.Generation), countdown.ExpiresAtMs(row.ExpiresAtMs)).Exec(ctx)
	if err != nil || n == 0 {
		return err
	}
	return tx.CountdownDelivery.Create().SetUserID(row.UserID).SetName(row.Name).SetGeneration(row.Generation).SetExpiresAtMs(row.ExpiresAtMs).SetProgress(map[string]string{}).Exec(ctx)
}
func (r *RecipeState) claimCountdownDelivery(ctx context.Context, now int64) (CountdownDelivery, bool, error) {
	rows, err := db.WithRead(ctx, func(ctx context.Context) ([]*ent.CountdownDelivery, error) {
		return r.client.CountdownDelivery.Query().Where(countdowndelivery.LeaseUntilMsLTE(now)).Order(ent.Asc(countdowndelivery.FieldExpiresAtMs)).Limit(32).All(ctx)
	})
	if err != nil {
		return CountdownDelivery{}, false, err
	}
	for _, row := range rows {
		delivery, won, err := r.leaseCountdownDelivery(ctx, row, now)
		if err != nil || won {
			return delivery, won, err
		}
	}
	return CountdownDelivery{}, false, nil
}
func (r *RecipeState) leaseCountdownDelivery(ctx context.Context, row *ent.CountdownDelivery, now int64) (CountdownDelivery, bool, error) {
	token := rand.Text()
	until := time.Now().UnixMilli() + CountdownLease.Milliseconds()
	var n int
	err := db.WithExec(ctx, func(ctx context.Context) error {
		var err error
		n, err = r.client.CountdownDelivery.Update().Where(countdowndelivery.ID(row.ID), countdowndelivery.LeaseUntilMsLTE(now)).SetLeaseToken(token).SetLeaseUntilMs(until).Save(ctx)
		return err
	})
	if err != nil || n != 1 {
		return CountdownDelivery{}, false, err
	}
	return CountdownDelivery{UserID: row.UserID, Name: row.Name, Generation: row.Generation, Token: token, ExpiresAtMS: row.ExpiresAtMs, Progress: row.Progress}, true, nil
}
func (d *CountdownDelivery) validate() error {
	name, err := countdownName(d.UserID, d.Name)
	if err != nil {
		return err
	}
	d.Name = name
	if d.Generation == "" {
		return errors.Join(ErrInvalidInput, errors.New("countdown claim"))
	}
	if d.Token == "" || d.ExpiresAtMS <= 0 {
		return errors.Join(ErrInvalidInput, errors.New("countdown claim"))
	}
	return nil
}

// CountdownAck is fenced by generation, token, deadline and the unexpired lease.
func (r *RecipeState) CountdownAck(ctx context.Context, d CountdownDelivery) (bool, error) {
	if err := d.validate(); err != nil {
		return false, err
	}
	var n int
	err := db.WithExec(ctx, func(ctx context.Context) error {
		var err error
		n, err = r.client.CountdownDelivery.Delete().Where(countdownLeasePredicates(d)...).Exec(ctx)
		return err
	})
	return n == 1, err
}

func (req *CountdownProgressRequest) validate() error {
	if err := req.Delivery.validate(); err != nil {
		return err
	}
	if len(req.Key) == 0 || len(req.Key) > 256 {
		return fmt.Errorf("%w: countdown progress", ErrInvalidInput)
	}
	if req.Status != "conditions_passed" && req.Status != "completed" {
		return fmt.Errorf("%w: countdown progress", ErrInvalidInput)
	}
	return nil
}
