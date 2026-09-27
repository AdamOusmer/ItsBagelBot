// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package module

import (
	"context"
	"time"
)

type Kind int

const (
	KindCore Kind = iota
	KindDefault
	KindOptIn
)

func (k Kind) String() string {
	switch k {
	case KindCore:
		return "core"
	case KindDefault:
		return "default"
	case KindOptIn:
		return "opt-in"
	default:
		return "unknown"
	}
}

// Pooled by the engine: a module must not retain it after Emit.
type Output struct {
	Type          string
	BroadcasterID string
	Locale        string
	Text          string
	Color         string
	To            string
	BatchID       string
	Items         []Output
	Duration      float64
	Template      string
	TargetUserID  string
	Reason        string
	MsgID         string
	RewardID      string
	RedemptionID  string
	Status        string
}

type Emit func(o *Output)

type RunFunc func(ctx context.Context, c *Context, args string, emit Emit) error

type EventHandler func(ctx context.Context, c *Context, emit Emit) error

type Command struct {
	Name          string
	Aliases       []string
	Perm          Role
	Cooldown      time.Duration
	LiveOnly      bool
	AllowedUserID string
	NumericSuffix bool
	Run           RunFunc
}

// VariableGroup publishes a read-only public reply view for custom commands.
type VariableGroup struct {
	Name   string
	Fields []string
	Read   func(context.Context, *Context) (map[string]string, error)
}

type Module struct {
	Variables []VariableGroup
	Name      string
	Kind      Kind
	Events    map[string]EventHandler
	Commands  []Command
	Beta      bool
	Trial     bool
}
