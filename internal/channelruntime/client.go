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
	current, found, err := c.cached(ctx, id)
	if err != nil || found {
		return current, err
	}
	return c.loadOwner(ctx, id)
}

func (c *Client) cached(ctx context.Context, id uint64) (Snapshot, bool, error) {
	u, found, err := c.store.GetUserRuntime(ctx, id)
	if err != nil || !found {
		return Snapshot{}, false, err
	}
	if u.Lifecycle == nil {
		return Snapshot{}, false, nil
	}
	return snapshot(u), true, nil
}

func (c *Client) loadOwner(ctx context.Context, id uint64) (Snapshot, error) {
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
	current, found, err := c.cached(ctx, id)
	if err == nil && !found {
		err = errors.New("lifecycle projection unavailable")
	}
	return current, err
}

func snapshot(u projection.User) Snapshot {
	s := Snapshot{AccountCreatedAt: u.AccountCreatedAt, Registered: true, ServiceBanned: u.Banned}
	if u.Lifecycle != nil {
		s.State = *u.Lifecycle
	}
	return s
}

func (c *Client) Apply(ctx context.Context, id uint64, reply usersrpc.LifecycleReply) error {
	return c.store.SetLifecycle(ctx, id, projection.LifecycleProjection{State: reply.State, AccountCreatedAt: reply.AccountCreatedAt, StateRevision: reply.StateRevision})
}

type Activity struct {
	EventID string
	At      time.Time
}

// Engage is called for useful work, never for passive reads or scheduled ticks.
// Users coalesces ordinary timestamps and makes the first dormant wake immediate.
func (c *Client) Engage(ctx context.Context, id uint64, current Snapshot, activity Activity) (Snapshot, error) {
	if !current.AllowsRuntime() {
		return current, nil
	}
	if current.State.RuntimeState != lifecycle.Dormant && activity.At.Sub(current.State.LastEngagedAt) < 15*time.Minute {
		return current, nil
	}
	next, applied, err := c.recordActivity(ctx, id, current, activity)
	if err != nil || applied {
		return next, err
	}
	// Retry once when a healthy dormancy transition won the generation race.
	// Ban, owner disable and account replacement cannot qualify for this retry.
	if !next.racedDormancy(current) {
		return next, nil
	}
	next, _, err = c.recordActivity(ctx, id, next, activity)
	return next, err
}

func (s Snapshot) racedDormancy(prior Snapshot) bool {
	if !s.AllowsRuntime() {
		return false
	}
	if s.AccountCreatedAt != prior.AccountCreatedAt {
		return false
	}
	if s.State.RuntimeState != lifecycle.Dormant {
		return false
	}
	return s.State.AdmissionGeneration != prior.State.AdmissionGeneration
}

func (c *Client) recordActivity(ctx context.Context, id uint64, current Snapshot, activity Activity) (Snapshot, bool, error) {
	req := usersrpc.LifecycleRequest{UserID: id, AccountCreatedAt: current.AccountCreatedAt, AdmissionGeneration: current.State.AdmissionGeneration, Action: "engage", EventID: activity.EventID, ObservedAt: activity.At}
	r, err := bus.RequestJSONTimeout[usersrpc.LifecycleReply](ctx, c.nc, usersrpc.LifecycleSubject, req, 1500*time.Millisecond)
	if err != nil {
		return Snapshot{}, false, err
	}
	if err = c.Apply(ctx, id, r); err != nil {
		return Snapshot{}, false, err
	}
	next, err := c.Get(ctx, id)
	return next, r.Applied, err
}

// GetStreamLive reads both projected and flat live facts using their original versions.
func (c *Client) GetStreamLive(ctx context.Context, id uint64) (projection.StreamLive, error) {
	return c.store.GetStreamLive(ctx, id)
}
