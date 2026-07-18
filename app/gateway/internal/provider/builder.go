package provider

import (
	"errors"
	"fmt"
	"time"
)

// Builder is the fluent authoring surface for one provider, the gateway's twin
// of sesame's module.Builder. A provider's New creates a Builder with
// NewProvider, declares its endpoints, then calls Build to get the immutable
// Provider the engine serves:
//
//	b := provider.NewProvider("mcsr", d)
//	b.Endpoint("user").Timeout(15 * time.Second).Handle(p.user)
//	b.Endpoint("session").Timeout(15 * time.Second).Handle(p.session)
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

// Build validates the assembled provider and returns its immutable form. It
// panics on a programmer error (empty or duplicate endpoint name, an endpoint
// with no terminal, an unfinished flow): these are startup misconfigurations,
// not runtime data, so failing loud at boot is the right behavior. Use
// Validate to check without panicking.
func (b *Builder) Build() Provider {
	if err := b.Validate(); err != nil {
		panic("gateway/provider: " + err.Error())
	}
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

// built is the immutable Provider Build returns.
type built struct {
	name      string
	endpoints []Endpoint
}

func (p built) Name() string          { return p.name }
func (p built) Endpoints() []Endpoint { return p.endpoints }
