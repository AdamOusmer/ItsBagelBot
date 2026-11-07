// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"time"

	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/valkey-io/valkey-go"
)

const (
	runQueueLease     = 15 * time.Second
	runQueueCapacity  = maxQueueWaiting + 1
	runQueueKeyPrefix = "automation:queue:"
)

type ValkeyRunQueue struct {
	client valkey.Client
	lease  time.Duration
}

func NewValkeyRunQueue(client valkey.Client) *ValkeyRunQueue {
	return newValkeyRunQueue(client, runQueueLease)
}

func newValkeyRunQueue(client valkey.Client, lease time.Duration) *ValkeyRunQueue {
	return &ValkeyRunQueue{client: pkg_valkey.Primary(client), lease: lease}
}

func runQueueKeys(seat RunQueueSeat) []string {
	base := runQueueKeyPrefix + "{" + strconv.FormatUint(seat.BroadcasterID, 10) + "}:" + seat.Queue
	return []string{base + ":order", base + ":lease"}
}

// A seat with a durable record never lapses: its run passes to whichever replica resumes it.
func runQueueSeats(seat RunQueueSeat) pkg_valkey.FIFOSeats {
	keys := runQueueKeys(seat)
	return pkg_valkey.FIFOSeats{Order: keys[0], Leases: keys[1], Pins: durableSeatsKey(seat.BroadcasterID)}
}

func (q *ValkeyRunQueue) request(seat RunQueueSeat) pkg_valkey.SeatRequest {
	return pkg_valkey.SeatRequest{Ticket: seat.Ticket, Lease: q.lease, Capacity: runQueueCapacity, PinnedTTL: durableKeyTTL}
}

func (q *ValkeyRunQueue) Enter(ctx context.Context, seat RunQueueSeat) (int64, error) {
	return runQueueSeats(seat).Enter(ctx, q.client, q.request(seat))
}

func (q *ValkeyRunQueue) Touch(ctx context.Context, seat RunQueueSeat) (int64, error) {
	return runQueueSeats(seat).Touch(ctx, q.client, q.request(seat))
}

// A path still recorded for this ticket keeps the seat: the last one to finish lets it go.
func (q *ValkeyRunQueue) Leave(ctx context.Context, seat RunQueueSeat) error {
	return runQueueSeats(seat).Leave(ctx, q.client, seat.Ticket)
}
