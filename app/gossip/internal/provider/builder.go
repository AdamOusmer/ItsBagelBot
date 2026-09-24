// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package provider

import (
	"errors"
	"fmt"
	"time"

	"ItsBagelBot/app/gossip/internal/core"
)

type Builder struct {
	name string
	deps Deps
	eps  []*endpointSpec

	// Default must stay WARP: a missed Trusted() then hides egress instead of exposing pod IPs.
	trusted bool
	clients []clientSpec
}

type clientSpec struct {
	lane core.Lane
}

type endpointSpec struct {
	name    string
	timeout time.Duration
	handle  HandlerFunc
	flow    *flowSpec
}

func NewProvider(name string, d Deps) *Builder {
	d.Log = d.Logger()
	return &Builder{name: name, deps: d}
}

func (b *Builder) Endpoint(name string) *EndpointBuilder {
	s := &endpointSpec{name: name}
	b.eps = append(b.eps, s)
	return &EndpointBuilder{s: s}
}

func (b *Builder) Trusted() *Builder {
	if len(b.clients) > 0 {
		panic("gossip/provider: " + b.name + ".Trusted() called after Client(): trust is positional, declare it before constructing clients")
	}
	b.trusted = true
	return b
}

func (b *Builder) Client(base string, headers map[string]string, timeout time.Duration) *core.HTTPClient {
	lane := core.LaneWARP
	if b.trusted {
		lane = core.LaneDirect
	}
	b.clients = append(b.clients, clientSpec{lane: lane})
	return core.ProviderClient(lane, base, headers, timeout)
}

func (b *Builder) logClientTally() {
	log := b.deps.Log
	if log == nil {
		return
	}
	noun := "clients"
	if len(b.clients) == 1 {
		noun = "client"
	}
	log.Info(fmt.Sprintf("%s: %d %s (%s)", b.name, len(b.clients), noun, b.laneLabel()))
}

func (b *Builder) laneLabel() string {
	if len(b.clients) == 0 {
		return "mixed"
	}
	first := b.clients[0].lane
	for _, c := range b.clients[1:] {
		if c.lane != first {
			return "mixed"
		}
	}
	return first.String()
}

func (b *Builder) Build() Provider {
	if err := b.Validate(); err != nil {
		panic("gossip/provider: " + err.Error())
	}
	b.logClientTally()
	eps := make([]Endpoint, len(b.eps))
	for i, s := range b.eps {
		eps[i] = Endpoint{Name: s.name, Timeout: s.timeout, Handle: s.handler(b)}
	}
	return built{name: b.name, endpoints: eps}
}

func (b *Builder) Validate() error {
	if b.name == "" {
		return errors.New("provider must have a non-empty name")
	}
	if b.trusted && len(b.clients) == 0 {
		return fmt.Errorf("provider %q declares .Trusted() but constructed no clients through the builder", b.name)
	}
	if len(b.eps) == 0 {
		return fmt.Errorf("provider %q declares no endpoints", b.name)
	}
	claimed := make(map[string]struct{}, len(b.eps))
	for _, s := range b.eps {
		if err := b.validateEndpoint(claimed, s); err != nil {
			return err
		}
	}
	return nil
}

func (b *Builder) validateEndpoint(claimed map[string]struct{}, s *endpointSpec) error {
	if s.name == "" {
		return fmt.Errorf("provider %q has an endpoint with an empty name", b.name)
	}
	if _, dup := claimed[s.name]; dup {
		return fmt.Errorf("provider %q declares endpoint %q twice", b.name, s.name)
	}
	claimed[s.name] = struct{}{}
	return b.validateTerminal(s)
}

func (b *Builder) validateTerminal(s *endpointSpec) error {
	switch {
	case s.handle != nil && s.flow != nil:
		return fmt.Errorf("endpoint %q chains both Handle and Cached", s.name)
	case s.handle == nil && s.flow == nil:
		return fmt.Errorf("endpoint %q has no terminal (chain .Handle or .Cached(...).Fetch to finish it)", s.name)
	case s.flow != nil:
		return s.flow.validate(b.deps, endpointRef{provider: b.name, endpoint: s.name})
	}
	return nil
}

func (s *endpointSpec) handler(b *Builder) HandlerFunc {
	if s.handle != nil {
		return s.handle
	}
	return s.flow.handler(b.deps, endpointRef{provider: b.name, endpoint: s.name})
}

type EndpointBuilder struct {
	s *endpointSpec
}

func (e *EndpointBuilder) Timeout(d time.Duration) *EndpointBuilder {
	e.s.timeout = d
	return e
}

func (e *EndpointBuilder) Handle(fn HandlerFunc) {
	e.s.handle = fn
}

func (e *EndpointBuilder) Cached(ttl, negativeTTL time.Duration) *FlowBuilder {
	f := &flowSpec{ttl: ttl, negativeTTL: negativeTTL, id: Account}
	e.s.flow = f
	return &FlowBuilder{f: f}
}

func (e *EndpointBuilder) CachedUntil(deadline DeadlineFunc, negativeTTL time.Duration) *FlowBuilder {
	f := &flowSpec{deadline: deadline, negativeTTL: negativeTTL, id: Account}
	e.s.flow = f
	return &FlowBuilder{f: f}
}

type built struct {
	name      string
	endpoints []Endpoint
}

func (p built) Name() string          { return p.name }
func (p built) Endpoints() []Endpoint { return p.endpoints }
