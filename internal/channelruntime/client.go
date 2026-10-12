// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Package channelruntime shares lifecycle admission and owner fallback across
// Twitch consumers. Durable state remains owned by Users.
package channelruntime

import (
	"context"
	"errors"
	"strconv"
	"time"

	"ItsBagelBot/internal/domain/lifecycle"
	contract "ItsBagelBot/internal/domain/rpc/projection"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"github.com/nats-io/nats.go"
)

type Snapshot struct {
	State            lifecycle.State
	AccountCreatedAt int64
	Registered       bool
	ServiceBanned    bool
}

func (s Snapshot) AllowsRuntime() bool {
	return s.Registered && !s.ServiceBanned && s.State.AllowsRuntime()
}
func (s Snapshot) AllowsBackground() bool {
	return s.Registered && !s.ServiceBanned && s.State.AllowsBackground()
}

type Reader interface {
	Get(context.Context, uint64) (Snapshot, error)
}

type Client struct {
	store        *projection.Store
	nc           *nats.Conn
	usersSubject string
}

func NewClient(store *projection.Store, nc *nats.Conn, usersSubject string) *Client {
	if usersSubject == "" {
		usersSubject = "bagel.rpc.internal.projection.users.get"
	}
	return &Client{store: store, nc: nc, usersSubject: usersSubject}
}

func (c *Client) Get(ctx context.Context, id uint64) (Snapshot, error) {
	u, found, err := c.store.GetUserRuntime(ctx, id)
	if err != nil {
		return Snapshot{}, err
	}
	if found && u.Lifecycle != nil {
		return snapshot(u), nil
	}
	if c.nc == nil {
		return Snapshot{}, errors.New("lifecycle authority unavailable")
	}
	r, err := bus.RequestJSONTimeout[contract.UserReply](ctx, c.nc, c.usersSubject, contract.Request{UserID: strconv.FormatUint(id, 10)}, 1500*time.Millisecond)
	if bus.NotFoundReply(err) {
		return Snapshot{}, nil
	}
	if err != nil {
		return Snapshot{}, err
	}
	if r.Lifecycle == nil {
		return Snapshot{}, errors.New("lifecycle snapshot unavailable")
	}
	if err := c.store.SetUser(ctx, id, projection.UserFromReply(r)); err != nil {
		return Snapshot{}, err
	}
	// Re-read after the fenced write: a concurrent ban may have won while the
	// owner RPC was in flight. Never return the stale response in that case.
	u, found, err = c.store.GetUserRuntime(ctx, id)
	if err != nil {
		return Snapshot{}, err
	}
	if !found || u.Lifecycle == nil {
		return Snapshot{}, errors.New("lifecycle projection unavailable")
	}
	return snapshot(u), nil
}

func snapshot(u projection.User) Snapshot {
	s := Snapshot{AccountCreatedAt: u.AccountCreatedAt, Registered: true, ServiceBanned: u.Banned}
	if u.Lifecycle != nil {
		s.State = *u.Lifecycle
	}
	return s
}

func (c *Client) Apply(ctx context.Context, id uint64, reply usersrpc.LifecycleReply) error {
	return c.store.SetLifecycle(ctx, id, reply.State, reply.AccountCreatedAt, reply.StateRevision)
}

// Engage is called for useful work, never for passive reads or scheduled ticks.
// Users coalesces ordinary timestamps and makes the first dormant wake immediate.
func (c *Client) Engage(ctx context.Context, id uint64, current Snapshot, eventID string, at time.Time) (Snapshot, error) {
	if !current.AllowsRuntime() {
		return current, nil
	}
	if current.State.RuntimeState != lifecycle.Dormant && at.Sub(current.State.LastEngagedAt) < 15*time.Minute {
		return current, nil
	}
	initialIncarnation := current.AccountCreatedAt
	for attempt := 0; attempt < 2; attempt++ {
		r, err := bus.RequestJSONTimeout[usersrpc.LifecycleReply](ctx, c.nc, usersrpc.LifecycleSubject, usersrpc.LifecycleRequest{UserID: id, AccountCreatedAt: current.AccountCreatedAt, AdmissionGeneration: current.State.AdmissionGeneration, Action: "engage", EventID: eventID, ObservedAt: at}, 1500*time.Millisecond)
		if err != nil {
			return Snapshot{}, err
		}
		if err = c.Apply(ctx, id, r); err != nil {
			return Snapshot{}, err
		}
		next, err := c.Get(ctx, id)
		if err != nil {
			return Snapshot{}, err
		}
		// A healthy dormant transition can win after a caller captured active
		// admission. Retry exactly once against that current generation; a ban,
		// owner disable or replacement account is never a wake retry.
		if attempt == 0 && !r.Applied && next.AllowsRuntime() && next.AccountCreatedAt == initialIncarnation && next.State.RuntimeState == lifecycle.Dormant && next.State.AdmissionGeneration != current.State.AdmissionGeneration {
			current = next
			continue
		}
		return next, nil
	}
	return Snapshot{}, errors.New("dormant wake did not finish")

}

// GetStreamLive reads both projected and flat live facts using their original versions.
func (c *Client) GetStreamLive(ctx context.Context, id uint64) (projection.StreamLive, error) {
	return c.store.GetStreamLive(ctx, id)
}
