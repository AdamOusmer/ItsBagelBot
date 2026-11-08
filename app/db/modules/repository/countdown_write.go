// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
package repository

import (
	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/app/db/modules/ent/countdown"
	"ItsBagelBot/app/db/modules/ent/countdownoperation"
	"ItsBagelBot/internal/domain/automation"
	"context"
	"crypto/rand"
	"fmt"
	"time"
)

func (req *CountdownWriteRequest) validate() error {
	name, err := countdownName(req.UserID, req.Name)
	if err != nil {
		return err
	}
	req.Name = name
	if req.Verb != "start" && req.Verb != "add" {
		return fmt.Errorf("%w: countdown operation", ErrInvalidInput)
	}
	if req.Seconds <= 0 || req.Seconds > int64(automation.MaxCooldownSeconds) {
		return fmt.Errorf("%w: countdown operation", ErrInvalidInput)
	}
	if len(req.OperationID) == 0 || len(req.OperationID) > 256 {
		return fmt.Errorf("%w: countdown operation", ErrInvalidInput)
	}
	return nil
}

// CountdownWrite serializes names and deduplicates effects per channel and operation ID.
func (r *RecipeState) CountdownWrite(ctx context.Context, req CountdownWriteRequest) (CountdownResult, error) {
	if err := req.validate(); err != nil {
		return CountdownResult{}, err
	}
	var out CountdownResult
	err := r.countdownTx(ctx, func(ctx context.Context, tx *ent.Tx) error {
		out = CountdownResult{}
		var err error
		out, err = writeCountdownTx(ctx, tx, req)
		return err
	})
	return out, err
}

func writeCountdownTx(ctx context.Context, tx *ent.Tx, req CountdownWriteRequest) (CountdownResult, error) {
	if err := lockCountdownChannel(ctx, tx, req.UserID); err != nil {
		return CountdownResult{}, err
	}
	prev, err := tx.CountdownOperation.Query().Where(countdownoperation.UserID(req.UserID), countdownoperation.OperationID(req.OperationID)).Only(ctx)
	if err == nil {
		return replayCountdownOperation(prev, req)
	}
	if !ent.IsNotFound(err) {
		return CountdownResult{}, err
	}
	now := time.Now().UnixMilli()
	state := countdownMutation{request: req, now: now}
	out, err := applyCountdownWrite(ctx, tx, state)
	if err != nil {
		return CountdownResult{}, err
	}
	return out, recordCountdownOperation(ctx, tx, countdownOperation{request: req, result: out, now: now})
}

func replayCountdownOperation(prev *ent.CountdownOperation, req CountdownWriteRequest) (CountdownResult, error) {
	if !sameCountdownOperation(prev, req) {
		return CountdownResult{}, fmt.Errorf("%w: operation ID reused", ErrInvalidInput)
	}
	return CountdownResult{Found: prev.Found, Capped: prev.Capped, ExpiresAtMS: prev.ExpiresAtMs}, nil
}
func sameCountdownOperation(prev *ent.CountdownOperation, req CountdownWriteRequest) bool {
	previous := CountdownWriteRequest{Name: prev.Name, Verb: prev.Verb, Seconds: prev.Seconds}
	return previous == (CountdownWriteRequest{Name: req.Name, Verb: req.Verb, Seconds: req.Seconds})
}

type countdownMutation struct {
	request CountdownWriteRequest
	row     *ent.Countdown
	now     int64
}
type countdownOperation struct {
	request CountdownWriteRequest
	result  CountdownResult
	now     int64
}

func applyCountdownWrite(ctx context.Context, tx *ent.Tx, state countdownMutation) (CountdownResult, error) {
	req := state.request
	row, err := tx.Countdown.Query().Where(countdown.UserID(req.UserID), countdown.Name(req.Name)).Only(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return CountdownResult{}, err
	}
	if req.Verb == "start" {
		state.row = row
		return startCountdown(ctx, tx, state)
	}
	state.row = row
	return addCountdown(ctx, tx, state)
}
func startCountdown(ctx context.Context, tx *ent.Tx, state countdownMutation) (CountdownResult, error) {
	req, row := state.request, state.row
	deadline := state.now + req.Seconds*1000
	if row != nil {
		err := tx.Countdown.UpdateOneID(row.ID).SetExpiresAtMs(deadline).SetGeneration(rand.Text()).Exec(ctx)
		return CountdownResult{Found: true, ExpiresAtMS: deadline}, err
	}
	if err := checkCountdownRoom(ctx, tx, req.UserID); err != nil {
		return CountdownResult{}, err
	}
	err := tx.Countdown.Create().SetUserID(req.UserID).SetName(req.Name).SetExpiresAtMs(deadline).SetGeneration(rand.Text()).Exec(ctx)
	return CountdownResult{Found: true, ExpiresAtMS: deadline}, err
}
func checkCountdownRoom(ctx context.Context, tx *ent.Tx, userID uint64) error {
	count, err := tx.Countdown.Query().Where(countdown.UserID(userID)).Count(ctx)
	if err != nil {
		return err
	}
	if count >= maxCountdownNames {
		return ErrVariableLimit
	}
	return nil
}
func addCountdown(ctx context.Context, tx *ent.Tx, state countdownMutation) (CountdownResult, error) {
	req, row, now := state.request, state.row, state.now
	if row == nil || row.ExpiresAtMs <= now {
		return CountdownResult{}, nil
	}
	deadline := row.ExpiresAtMs + req.Seconds*1000
	ceiling := now + int64(automation.MaxCooldownSeconds)*1000
	capped := deadline > ceiling
	if capped {
		deadline = ceiling
	}
	err := tx.Countdown.UpdateOneID(row.ID).SetExpiresAtMs(deadline).Exec(ctx)
	return CountdownResult{Found: true, Capped: capped, ExpiresAtMS: deadline}, err
}
func recordCountdownOperation(ctx context.Context, tx *ent.Tx, operation countdownOperation) error {
	req, out, now := operation.request, operation.result, operation.now
	return tx.CountdownOperation.Create().SetUserID(req.UserID).SetOperationID(req.OperationID).SetName(req.Name).SetVerb(req.Verb).SetSeconds(req.Seconds).SetFound(out.Found).SetCapped(out.Capped).SetExpiresAtMs(out.ExpiresAtMS).SetCreatedAtMs(now).Exec(ctx)
}
