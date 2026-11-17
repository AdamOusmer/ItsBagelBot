// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package idempotency_test

import (
	"context"
	"errors"
	"testing"

	"ItsBagelBot/pkg/idempotency"
	"github.com/stretchr/testify/require"
)

func TestWorkTracksEachGenerationAndTransferredWork(t *testing.T) {
	ctx, root := idempotency.NewWork(context.Background())
	first, second := &idempotency.Handoff{}, &idempotency.Handoff{}
	idempotency.Join(ctx, first)
	child, transfer := idempotency.NewWork(ctx)
	idempotency.Join(ctx, &transfer.Handoff)
	idempotency.Join(child, second)
	var got []idempotency.Result
	root.Watch(func(r idempotency.Result) { got = append(got, r) })
	root.Seal(nil)
	transfer.Seal(nil)
	first.Notify(errors.New("first generation failed"))
	second.Resolve(nil)
	require.Empty(t, got)
	first.Resolve(nil)
	require.Equal(t, []idempotency.Result{{Final: true}}, got)
}

func TestWorkRetainsTerminalFailure(t *testing.T) {
	ctx, root := idempotency.NewWork(context.Background())
	generation := &idempotency.Handoff{}
	idempotency.Join(ctx, generation)
	failure := errors.New("synchronous effect failed")
	idempotency.Fail(ctx, failure)
	root.Seal(nil)
	generation.Resolve(nil)
	var got idempotency.Result
	root.Watch(func(r idempotency.Result) { got = r })
	require.Equal(t, idempotency.Result{Err: failure, Final: true}, got)
}

func TestHandoffCancellationRemovesGenerationWatcher(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(map[bool]string{false: "watching", true: "expired"}[cancel], func(t *testing.T) {
			h := &idempotency.Handoff{}
			var got []idempotency.Result
			subscription := h.Watch(func(r idempotency.Result) { got = append(got, r) })
			if cancel {
				subscription.Cancel()
				subscription.Cancel()
			}
			failure := errors.New("unconfirmed")
			h.Notify(failure)
			h.Resolve(nil)
			want := []idempotency.Result{{Err: failure}, {Final: true}}
			if cancel {
				want = nil
			}
			require.Equal(t, want, got)
		})
	}
}
