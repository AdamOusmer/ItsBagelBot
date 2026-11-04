// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automation

import (
	"time"

	"ItsBagelBot/pkg/codec"
)

const Version = 1

type Automation struct {
	ID         string     `json:"id"`
	UserID     uint64     `json:"user_id"`
	Name       string     `json:"name"`
	Enabled    bool       `json:"enabled"`
	Revision   int64      `json:"revision"`
	Definition Definition `json:"definition"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// Summary is the dashboard's list row. Preview lives only here, so it can never reach
// the projection or the change event, which both carry Automation.
type Summary struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Enabled     bool      `json:"enabled"`
	Revision    int64     `json:"revision"`
	TriggerType string    `json:"trigger_type"`
	Preview     string    `json:"preview,omitempty"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Definition struct {
	Version    int         `json:"version"`
	Trigger    Trigger     `json:"trigger"`
	Conditions []Condition `json:"conditions"`
	Actions    []Action    `json:"actions"`
}

type Trigger struct {
	Type   string           `json:"type"`
	Params codec.RawMessage `json:"params,omitempty"`
}

type Condition struct {
	Type   string           `json:"type"`
	Params codec.RawMessage `json:"params,omitempty"`
}

type Action struct {
	Type    string           `json:"type"`
	Enabled *bool            `json:"enabled,omitempty"`
	Params  codec.RawMessage `json:"params,omitempty"`
}

func (a Action) IsEnabled() bool { return a.Enabled == nil || *a.Enabled }

type Issue struct {
	Path string `json:"path"`
	Code string `json:"code"`
}
