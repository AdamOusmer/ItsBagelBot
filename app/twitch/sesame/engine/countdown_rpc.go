// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
package engine

import (
	"context"
	"errors"
	"strconv"

	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"
)

func (c *RecipeStateRPC) countdownWrite(ctx context.Context, verb string, mutation CountdownMutation) (CountdownResult, error) {
	r, err := recipeStateCall[modulesrpc.CountdownReply](ctx, c, "countdown."+verb, modulesrpc.CountdownRequest{UserID: fmtID(mutation.Ref.BroadcasterID), Name: mutation.Ref.Name, Seconds: mutation.Seconds, OperationID: mutation.OperationID})
	if err != nil {
		return CountdownResult{}, err
	}
	if r.LimitExceeded {
		return CountdownResult{}, ErrVariableLimit
	}
	return CountdownResult{Found: r.Found, Capped: r.Capped, ExpiresAtMS: r.ExpiresAtMS}, nil
}
func (c *RecipeStateRPC) StartCountdown(ctx context.Context, mutation CountdownMutation) (CountdownResult, error) {
	return c.countdownWrite(ctx, "start", mutation)
}
func (c *RecipeStateRPC) AddCountdown(ctx context.Context, mutation CountdownMutation) (CountdownResult, error) {
	return c.countdownWrite(ctx, "add", mutation)
}
func (c *RecipeStateRPC) PeekCountdown(ctx context.Context, ref CountdownRef) (CountdownResult, error) {
	return c.countdownWrite(ctx, "peek", CountdownMutation{Ref: ref})
}
func (c *RecipeStateRPC) ClaimCountdown(ctx context.Context) (CountdownDue, bool, error) {
	r, err := recipeStateCall[modulesrpc.CountdownDeliveryReply](ctx, c, "countdown.claim", modulesrpc.CountdownDeliveryRequest{})
	if err != nil || !r.Found {
		return CountdownDue{}, false, err
	}
	id, err := strconv.ParseUint(r.Delivery.UserID, 10, 64)
	if err != nil || id == 0 {
		return CountdownDue{}, false, errors.New("invalid countdown delivery channel")
	}
	d := r.Delivery
	return CountdownDue{BroadcasterID: id, Name: d.Name, Generation: d.Generation, Token: d.Token, ExpiresAtMS: d.ExpiresAtMS, Progress: d.Progress}, true, nil
}
func countdownDeliveryRPC(d CountdownDue) modulesrpc.CountdownDelivery {
	return modulesrpc.CountdownDelivery{UserID: fmtID(d.BroadcasterID), Name: d.Name, Generation: d.Generation, Token: d.Token, ExpiresAtMS: d.ExpiresAtMS}
}
func (c *RecipeStateRPC) countdownDeliveryCall(ctx context.Context, verb string, req any) error {
	r, err := recipeStateCall[modulesrpc.CountdownDeliveryReply](ctx, c, "countdown."+verb, req)
	if err != nil {
		return err
	}
	if !r.Found {
		return errors.New("countdown lease lost")
	}
	return nil
}
func (c *RecipeStateRPC) AckCountdown(ctx context.Context, d CountdownDue) error {
	return c.countdownDeliveryCall(ctx, "ack", modulesrpc.CountdownDeliveryRequest{Delivery: countdownDeliveryRPC(d)})
}
func (c *RecipeStateRPC) ProgressCountdown(ctx context.Context, d CountdownDue, checkpoint CountdownCheckpoint) error {
	return c.countdownDeliveryCall(ctx, "progress", modulesrpc.CountdownProgressRequest{Delivery: countdownDeliveryRPC(d), Key: checkpoint.Key, Status: checkpoint.Status})
}
