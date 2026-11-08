// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
package repository

import (
	"context"
	"fmt"
	"time"

	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/app/db/modules/ent/countdown"
	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/pkg/db"
)

const CountdownLease = time.Minute
const maxCountdownNames = 128

type CountdownResult struct {
	Found, Capped bool
	ExpiresAtMS   int64
}
type CountdownDelivery struct {
	UserID                  uint64
	Name, Generation, Token string
	ExpiresAtMS             int64
	Progress                map[string]string
}
type CountdownWriteRequest struct {
	UserID                  uint64
	Name, Verb, OperationID string
	Seconds                 int64
}
type CountdownProgressRequest struct {
	Delivery    CountdownDelivery
	Key, Status string
}

func countdownName(userID uint64, raw string) (string, error) {
	name, ok := automation.VariableName(raw)
	if userID == 0 || !ok {
		return "", fmt.Errorf("%w: countdown identity", ErrInvalidInput)
	}
	return name, nil
}

func (r *RecipeState) CountdownPeek(ctx context.Context, userID uint64, raw string) (CountdownResult, error) {
	name, err := countdownName(userID, raw)
	if err != nil {
		return CountdownResult{}, err
	}
	row, err := db.WithRead(ctx, func(ctx context.Context) (*ent.Countdown, error) {
		return r.client.Countdown.Query().Where(countdown.UserID(userID), countdown.Name(name), countdown.ExpiresAtMsGT(time.Now().UnixMilli())).Only(ctx)
	})
	if ent.IsNotFound(err) {
		return CountdownResult{}, nil
	}
	if err != nil {
		return CountdownResult{}, err
	}
	return CountdownResult{Found: true, ExpiresAtMS: row.ExpiresAtMs}, nil
}

// countdownTx applies the transaction retry policy to countdown changes.
func (r *RecipeState) countdownTx(ctx context.Context, fn func(context.Context, *ent.Tx) error) error {
	return db.RetryTx(ctx, func(ctx context.Context) error {
		return db.WithExec(ctx, func(ctx context.Context) error {
			return db.WithTx(ctx, r.client, func(tx *ent.Tx) error { return fn(ctx, tx) })
		})
	})
}
func lockCountdownChannel(ctx context.Context, tx *ent.Tx, userID uint64) error {
	return lockVariableNames(ctx, tx, VariableOwner{UserID: userID, Scope: automation.VariableScopeChannel})
}
