// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package provider

import (
	"errors"
	"fmt"
	"time"

	"ItsBagelBot/app/gossip/internal/core"
)

// Builder is the fluent authoring surface for one provider, gossip's twin
// of sesame's module.Builder. A provider's New creates a Builder with
// NewProvider, declares its endpoints, then calls Build to get the immutable
// Provider the engine serves:
//
//	b := provider.NewProvider("mcsr", d).Trusted()
//	p := newAPI(cfg, d, b) // constructs clients via b.Client
//	b.Endpoint("user").Timeout(15 * time.Second).Handle(p.user)
//	return b.Build()
//
// Byte-flow endpoints (the cached fetch-and-shape skeleton every stats
// provider shares) chain Cached instead of Handle; see FlowBuilder.
//
// The Builder holds *endpointSpec pointers while it is being assembled so the
// chained setters mutate in place; Build copies them into the immutable
// Provider. A Builder is single-use and not safe for concurrent use.
type Builder struct {
	name string
	deps Deps
	eps  []*endpointSpec

	// trusted marks every client this builder constructs as trusted-direct.
	// The default is inverted on purpose — unmarked providers construct
	// WARP-lane clients — so a forgotten declaration fails toward hidden
	// egress, never toward exposing production IPs. See Trusted.
	trusted bool
	// clients records every construction that went through Client, in order.
	// It backs the boot-time tally log and the dead-flag validation.
	clients []clientSpec
}

// clientSpec is one outbound client a provider constructed through the
// Builder: the lane it dials. It exists so boot can account for every client
// and Validate can catch a trust flag that guards nothing.
type clientSpec struct {
	lane core.Lane
}

// endpointSpec is one endpoint under assembly: exactly one of handle (a
// bespoke handler) or flow (a declared byte-flow) finishes it.
type endpointSpec struct {
	name    string
	timeout time.Duration
	handle  HandlerFunc
	flow    *flowSpec
}

// NewProvider starts a provider of the given name. Unlike sesame's module
// package, Deps rides in here: cached flow endpoints need the shared cache and
// logger when Build assembles their handlers. Bespoke Handle endpoints still
// capture their services by closure, exactly like sesame modules.
func NewProvider(name string, d Deps) *Builder {
	d.Log = d.Logger()
	return &Builder{name: name, deps: d}
}

// Endpoint starts one RPC verb, returning an EndpointBuilder to chain its
// timeout and terminal. The endpoint is not complete until Handle or a
// Cached flow's Fetch is called; an unfinished endpoint is reported by Build.
func (b *Builder) Endpoint(name string) *EndpointBuilder {
	s := &endpointSpec{name: name}
	b.eps = append(b.eps, s)
	return &EndpointBuilder{s: s}
}

// Trusted marks every client this provider constructs as trusted-direct:
// egress from the pod's own IP on the shared transport. It returns the Builder
// so the declaration chains off NewProvider.
//
// The default is INVERTED — a builder without Trusted constructs WARP-lane
// clients — so the safe path is the default path and a forgotten flag fails
// toward hidden egress. Trust is positional by construction: calling this
// after any Client construction panics, because a client already built would
// have picked its lane from a flag that did not exist yet. Declare it first,
// immediately after NewProvider.
func (b *Builder) Trusted() *Builder {
	if len(b.clients) > 0 {
		panic("gossip/provider: " + b.name + ".Trusted() called after Client(): trust is positional, declare it before constructing clients")
	}
	b.trusted = true
	return b
}

// Client constructs one outbound HTTP client for this provider on the lane
// its trust declaration chose, and records the construction for boot-time
// accounting. It replaces every direct core constructor call — after that
// constructor went unexported, this is the only way a provider gets a client,
// which is what makes wrong-lane egress a reviewable diff instead of a silent
// default.
func (b *Builder) Client(base string, headers map[string]string, timeout time.Duration) *core.HTTPClient {
	lane := core.LaneWARP
	if b.trusted {
		lane = core.LaneDirect
	}
	b.clients = append(b.clients, clientSpec{lane: lane})
	return core.ProviderClient(lane, base, headers, timeout)
}

// Build validates the assembled provider and returns its immutable form. It
// panics on a programmer error (empty or duplicate endpoint name, an endpoint
// with no terminal, an unfinished flow): these are startup misconfigurations,
// not runtime data, so failing loud at boot is the right behavior. Use
// Validate to check without panicking.
//
// After validating it logs one line per provider tallying the clients it
// constructed and their lanes ("govee: 1 client (trusted)") — the honest boot
// record of who dials where. It cannot introspect Handle closures, so a
// provider could still smuggle a raw net/http client past this; the tally
// narrows that to a deliberate act, and WARP-lane external segments carrying
// lane=warp make wrong-lane egress visible at runtime.
// logClientTally records at boot who dials on which lane — the reviewable
// audit line the Builder chokepoint exists to produce.
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

// laneLabel names the single lane every client shares, or "mixed".
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

// Validate reports the first problem with the assembled provider, or nil when
// it is well formed. Build calls it and panics on a non-nil result; tests can
// call it directly.
func (b *Builder) Validate() error {
	if b.name == "" {
		return errors.New("provider must have a non-empty name")
	}
	// A dead trust flag is a misconfiguration, not a style issue: someone
	// declared trusted-direct egress and then constructed nothing through the
	// Builder — either the flag is stale or the clients were built somewhere
	// the chokepoint cannot see. Both deserve a boot failure.
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

// validateEndpoint checks one endpoint's name uniqueness and its terminal.
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

// validateTerminal enforces that exactly one terminal finished the endpoint
// and, for a flow, that the flow itself is complete.
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

// handler resolves the endpoint's HandlerFunc: the bespoke Handle, or the one
// its declared flow assembles.
func (s *endpointSpec) handler(b *Builder) HandlerFunc {
	if s.handle != nil {
		return s.handle
	}
	return s.flow.handler(b.deps, endpointRef{provider: b.name, endpoint: s.name})
}

// EndpointBuilder chains a single endpoint's settings. Handle is the terminal
// that finishes a bespoke endpoint; Cached opens the byte-flow chain whose own
// terminal is Fetch.
type EndpointBuilder struct {
	s *endpointSpec
}

// Timeout bounds one handler run; zero (the default) means the bus default.
func (e *EndpointBuilder) Timeout(d time.Duration) *EndpointBuilder {
	e.s.timeout = d
	return e
}

// Handle sets the endpoint's bespoke handler and finishes it. It is terminal:
// it returns nothing so a declaration cannot accidentally continue past it.
func (e *EndpointBuilder) Handle(fn HandlerFunc) {
	e.s.handle = fn
}

// Cached declares the endpoint as a byte-flow: successes cache for ttl,
// negatively cacheable friendly failures (player not found) for negativeTTL.
// The returned FlowBuilder chains the flow's identity, reply shaping and
// terminal Fetch.
func (e *EndpointBuilder) Cached(ttl, negativeTTL time.Duration) *FlowBuilder {
	f := &flowSpec{ttl: ttl, negativeTTL: negativeTTL, id: Account}
	e.s.flow = f
	return &FlowBuilder{f: f}
}

// CachedUntil is Cached for content that turns over on a clock instead of
// aging: deadline reports the instant the answer stops being true, and the flow
// sizes every stored reply against the time remaining to it. Failures still
// cache for the flat negativeTTL, which is about our upstream and not about the
// content's own schedule.
//
// Reach for it when an interval would be a guess. The Fortnite item shop swaps
// at 00:00 UTC and is byte-identical the rest of the day, so any interval is
// simultaneously too short (re-downloading a payload that provably did not
// change) and too long (serving yesterday's shop after the swap). A deadline is
// neither.
func (e *EndpointBuilder) CachedUntil(deadline DeadlineFunc, negativeTTL time.Duration) *FlowBuilder {
	f := &flowSpec{deadline: deadline, negativeTTL: negativeTTL, id: Account}
	e.s.flow = f
	return &FlowBuilder{f: f}
}

// built is the immutable Provider Build returns.
type built struct {
	name      string
	endpoints []Endpoint
}

func (p built) Name() string          { return p.name }
func (p built) Endpoints() []Endpoint { return p.endpoints }
