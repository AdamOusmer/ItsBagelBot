// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
package engine

import (
	"context"
	"errors"
	"strconv"

	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"
)

func (c *RecipeStateRPC) countdownWrite(ctx context.Context, verb string, channel uint64, name string, seconds int64, operation string) (CountdownResult, error) {
	r, err := recipeStateCall[modulesrpc.CountdownReply](ctx, c, "countdown."+verb, modulesrpc.CountdownRequest{UserID: fmtID(channel), Name: name, Seconds: seconds, OperationID: operation})
	if err != nil {
		return CountdownResult{}, err
	}
	if r.LimitExceeded {
		return CountdownResult{}, ErrVariableLimit
	}
	return CountdownResult{Found: r.Found, Capped: r.Capped, ExpiresAtMS: r.ExpiresAtMS}, nil
}
func (c *RecipeStateRPC) StartCountdown(ctx context.Context, channel uint64, name string, seconds int64, operation string) (CountdownResult, error) {
	return c.countdownWrite(ctx, "start", channel, name, seconds, operation)
}
func (c *RecipeStateRPC) AddCountdown(ctx context.Context, channel uint64, name string, seconds int64, operation string) (CountdownResult, error) {
	return c.countdownWrite(ctx, "add", channel, name, seconds, operation)
}
func (c *RecipeStateRPC) PeekCountdown(ctx context.Context, channel uint64, name string) (CountdownResult, error) {
	return c.countdownWrite(ctx, "peek", channel, name, 0, "")
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
func (c *RecipeStateRPC) AckCountdown(ctx context.Context, d CountdownDue) error {
	req := modulesrpc.CountdownDeliveryRequest{Delivery: modulesrpc.CountdownDelivery{UserID: fmtID(d.BroadcasterID), Name: d.Name, Generation: d.Generation, Token: d.Token, ExpiresAtMS: d.ExpiresAtMS}}
	r, err := recipeStateCall[modulesrpc.CountdownDeliveryReply](ctx, c, "countdown.ack", req)
	if err != nil {
		return err
	}
	if !r.Found {
		return errors.New("countdown lease lost")
	}
	return nil
}

func (c *RecipeStateRPC) ProgressCountdown(ctx context.Context, d CountdownDue, key, status string) error {
	req := modulesrpc.CountdownProgressRequest{Delivery: modulesrpc.CountdownDelivery{UserID: fmtID(d.BroadcasterID), Name: d.Name, Generation: d.Generation, Token: d.Token, ExpiresAtMS: d.ExpiresAtMS}, Key: key, Status: status}
	r, err := recipeStateCall[modulesrpc.CountdownDeliveryReply](ctx, c, "countdown.progress", req)
	if err != nil {
		return err
	}
	if !r.Found {
		return errors.New("countdown lease lost")
	}
	return nil
}
