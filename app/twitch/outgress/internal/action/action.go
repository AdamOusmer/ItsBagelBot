// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package action

import (
	"context"
	"strings"

	"ItsBagelBot/internal/domain/outgress"
)

type Kind int

const (
	KindHelix Kind = iota
	KindPassthrough
	KindInternal
)

func (k Kind) String() string {
	switch k {
	case KindHelix:
		return "helix"
	case KindPassthrough:
		return "passthrough"
	case KindInternal:
		return "internal"
	default:
		return "unknown"
	}
}

type RunFunc func(ctx context.Context, m *outgress.Message) error

type Action struct {
	Type     string
	Kind     Kind
	Method   string
	Endpoint string
	As       string
	Run      RunFunc
}

func (a Action) FillRoute(m *outgress.Message) bool {
	if a.Kind == KindInternal {
		return true
	}
	if m.Endpoint == "" {
		m.Endpoint = a.Endpoint
	}
	if m.Method == "" {
		m.Method = a.Method
	}
	if m.As == "" {
		m.As = a.As
	}
	return strings.HasPrefix(m.Endpoint, "/helix/") && m.Method != ""
}

type Registry struct {
	byType map[string]Action
}

func (r Registry) Lookup(messageType string) (Action, bool) {
	a, ok := r.byType[messageType]
	return a, ok
}
