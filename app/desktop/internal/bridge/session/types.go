// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package session owns connection policy. Machine is a single-owner reducer:
// callers serialize Handle, Route, and Snapshot; it performs no I/O or sleeps.
package session

import "time"

type Connection uint64

type State uint8

const (
	Stopped State = iota
	Connecting
	Binding
	Online
	Migrating
	Reconnecting
	Failed
	AuthenticationRequired
)

type Problem uint8

const (
	NoProblem Problem = iota
	ConnectionFailed
	MigrationFailed
	Unauthorized
	ProtocolFailure
)

type Config struct {
	MaxAttempts    int
	IdleTimeout    time.Duration
	RetryBudget    time.Duration
	DialTimeout    time.Duration
	WelcomeTimeout time.Duration
	BindTimeout    time.Duration
	BackoffBase    time.Duration
	BackoffMax     time.Duration
	Jitter         func(time.Duration) time.Duration
}

type Kind uint8

const (
	Start Kind = iota
	Dialed
	Welcome
	Bound
	Migrate
	Ready
	Applied
	Broken
	Tick
	Retry
	Stop
	AuthRejected
	Activity
	MigrationRejected
	BindFailed
	MigrationRequestFailed
)

type Event struct {
	Kind       Kind
	Connection Connection
	SessionID  string
	Migration  string
	Within     time.Duration
	Through    uint64
	Sequence   uint64
}

type Action uint8

const (
	Dial Action = iota
	Bind
	Close
	Changed
	ArmTimer
	RequestMigration
)

type Effect struct {
	Action     Action
	Connection Connection
	SessionID  string
	Migration  string
	Cursor     uint64
	At         time.Time
}

// Effects never grows or allocates. Each transition emits at most two closes,
// one dial/bind, one status change, and one timer update.
type Effects struct {
	Items [6]Effect
	Count int
}

func (e *Effects) add(effect Effect) { e.Items[e.Count] = effect; e.Count++ }

type View struct {
	State    State
	Problem  Problem
	Primary  Connection
	Pending  Connection
	Cursor   uint64
	Attempts int
}

type Delivery uint8

const (
	Drop Delivery = iota
	Deliver
	Buffer
	Gap
	Reject
)
