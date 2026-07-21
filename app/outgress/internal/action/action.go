// Package action is outgress's action authoring surface, mirroring sesame's
// module builder. Every message type the worker executes is declared as one
// Action built by a fluent Builder: it names its type, declares the Helix
// route it defaults to (or that it is internal / a passthrough), and registers
// its Run handler. The Builder produces an immutable Registry the worker
// dispatches from with one lock-free map lookup per message.
//
// This package is intentionally standalone: it holds only the authoring value
// types and the Builder. It carries no runtime wiring (no worker, limiter,
// registry, or Twitch client), so a Run handler captures whatever services it
// needs by closure and this package never has to know about them.
package action

import (
	"context"
	"strings"

	"ItsBagelBot/internal/domain/outgress"
)

// Kind classifies how an action's request routing is resolved before Run.
type Kind int

const (
	// KindHelix is a routed Helix call: the action carries the default
	// method/endpoint/identity, filled onto the message when the producer left
	// them empty. An explicit field always wins.
	KindHelix Kind = iota
	// KindPassthrough is a generic Helix call ("api"): the action carries no
	// route of its own, so the message must bring a valid /helix/ endpoint.
	KindPassthrough
	// KindInternal is a job that never resolves a Helix route here (EventSub
	// management, stream re-checks, typed client calls); its Run owns every
	// Twitch interaction itself.
	KindInternal
)

// String renders the kind for validation errors.
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

// RunFunc executes one admitted, route-resolved message. The handler owns its
// rate-bucket takes and the Twitch call; the returned error follows the lane
// ack discipline (nil acks, an error nacks for paced redelivery).
type RunFunc func(ctx context.Context, m *outgress.Message) error

// Action is the immutable artifact Build returns for one message type: the
// route defaults FillRoute applies plus the Run handler the worker invokes.
type Action struct {
	Type     string
	Kind     Kind
	Method   string
	Endpoint string
	As       string
	Run      RunFunc
}

// FillRoute fills the message's empty routing fields from the action's
// declared route and reports whether the resulting request is executable. An
// explicit field set by the producer always wins. Internal actions carry no
// route and always pass; helix and passthrough actions must end up with a
// /helix/ endpoint and a method, or the message must be dropped.
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

// Registry is the immutable action set the worker dispatches from. It is
// built once at startup and never mutated after, so lookups on the hot path
// are lock-free.
type Registry struct {
	byType map[string]Action
}

// Lookup returns the action registered for one message type.
func (r Registry) Lookup(messageType string) (Action, bool) {
	a, ok := r.byType[messageType]
	return a, ok
}
