package action

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"ItsBagelBot/internal/domain/outgress"
)

// Endpoint is a Helix path an action defaults to, so route declarations read
// as domain values rather than bare strings.
type Endpoint string

// Identity is the token identity a call executes under (outgress.AsApp,
// AsBot, AsBroadcaster); empty keeps the client's endpoint-based auto routing.
type Identity string

// Builder is the fluent authoring surface for the worker's action set. The
// worker creates one with NewSet, declares every message type it executes,
// then calls Build to get the immutable Registry it dispatches from:
//
//	b := action.NewSet()
//	b.Action(outgress.TypeChat).Post("/helix/chat/messages").As(outgress.AsApp).Run(w.processChat)
//	b.Action(outgress.TypeAPI).Passthrough().Run(w.processAPI)
//	b.Action(outgress.TypeEventSub).Internal().Run(w.processEventSub)
//	registry := b.Build()
//
// The Builder holds the actions while they are being assembled so the chained
// ActionBuilder setters mutate the action in place; Build copies them into
// the immutable Registry. A Builder is single-use and not safe for concurrent
// use.
type Builder struct {
	acts []*builderAction
}

// builderAction pairs the action under assembly with its route-form
// bookkeeping: routed records that one of Post/Put/Delete, Passthrough or
// Internal was chosen (Build rejects an action without one), redeclared that
// a second form was chained (Build rejects the conflict instead of letting
// the last declaration silently win).
type builderAction struct {
	act        Action
	routed     bool
	redeclared bool
}

// NewSet starts an empty action set.
func NewSet() *Builder {
	return &Builder{}
}

// Action starts the declaration for one message type, returning an
// ActionBuilder to chain its route and handler. The action is not complete
// until Run is called; an ActionBuilder left without Run is reported by Build.
func (b *Builder) Action(messageType string) *ActionBuilder {
	entry := &builderAction{act: Action{Type: messageType}}
	b.acts = append(b.acts, entry)
	return &ActionBuilder{entry: entry}
}

// Build validates the assembled set and returns its immutable form. It panics
// on a programmer error (empty or duplicate type, a missing or conflicting
// route form, an invalid Helix route, a missing Run): these are startup
// misconfigurations, not runtime data, so failing loud at boot is the right
// behavior. Use Validate to check without panicking.
func (b *Builder) Build() Registry {
	if err := b.Validate(); err != nil {
		panic("outgress/action: " + err.Error())
	}
	byType := make(map[string]Action, len(b.acts))
	for _, entry := range b.acts {
		byType[entry.act.Type] = entry.act
	}
	return Registry{byType: byType}
}

// Validate reports the first problem with the assembled set, or nil when it
// is well formed. Build calls it and panics on a non-nil result; tests can
// call it directly.
func (b *Builder) Validate() error {
	claimed := make(map[string]struct{}, len(b.acts))
	for _, entry := range b.acts {
		if err := validateAction(claimed, entry); err != nil {
			return err
		}
	}
	return nil
}

// validateAction checks one action's type, route form, route shape, and Run,
// then claims its type.
func validateAction(claimed map[string]struct{}, entry *builderAction) error {
	a := entry.act
	if a.Type == "" {
		return errors.New("action with an empty type")
	}
	if _, dup := claimed[a.Type]; dup {
		return fmt.Errorf("duplicate action type %q", a.Type)
	}
	claimed[a.Type] = struct{}{}
	if err := validateRouteForm(entry); err != nil {
		return err
	}
	if a.Run == nil {
		return fmt.Errorf("action %q has no Run (chain .Run to finish it)", a.Type)
	}
	return nil
}

// validateRouteForm checks that exactly one route form was declared, then
// hands the shape check to validateRoute.
func validateRouteForm(entry *builderAction) error {
	if !entry.routed {
		return fmt.Errorf("action %q declares no route form (chain Post/Put/Delete, Passthrough, or Internal)", entry.act.Type)
	}
	if entry.redeclared {
		return fmt.Errorf("action %q declares more than one route form", entry.act.Type)
	}
	return validateRoute(entry.act)
}

// validateRoute checks the route shape against the action's kind: a helix
// action must default to a real /helix/ call, the other kinds must not carry
// route defaults (their setters cannot produce a method or endpoint, so only
// a stray As can violate this).
func validateRoute(a Action) error {
	if a.Kind == KindHelix {
		return validateHelixRoute(a)
	}
	if a.As != "" {
		return fmt.Errorf("%s action %q must not carry a token identity", a.Kind, a.Type)
	}
	return nil
}

// validateHelixRoute checks a routed action's defaults: a method, a /helix/
// endpoint, and a known token identity.
func validateHelixRoute(a Action) error {
	if a.Method == "" || !strings.HasPrefix(a.Endpoint, "/helix/") {
		return fmt.Errorf("helix action %q has an invalid route %s %q", a.Type, a.Method, a.Endpoint)
	}
	switch a.As {
	case "", outgress.AsApp, outgress.AsBot, outgress.AsBroadcaster:
		return nil
	default:
		return fmt.Errorf("helix action %q has an unknown identity %q", a.Type, a.As)
	}
}

// ActionBuilder chains a single action's route and handler. Every setter
// returns the same ActionBuilder so a declaration reads as one line; Run is
// the terminal that finishes the action.
type ActionBuilder struct {
	entry *builderAction
}

// Post declares a routed Helix call defaulting to POST endpoint.
func (a *ActionBuilder) Post(endpoint Endpoint) *ActionBuilder {
	return a.helix(http.MethodPost, endpoint)
}

// Put declares a routed Helix call defaulting to PUT endpoint.
func (a *ActionBuilder) Put(endpoint Endpoint) *ActionBuilder {
	return a.helix(http.MethodPut, endpoint)
}

// Delete declares a routed Helix call defaulting to DELETE endpoint.
func (a *ActionBuilder) Delete(endpoint Endpoint) *ActionBuilder {
	return a.helix(http.MethodDelete, endpoint)
}

func (a *ActionBuilder) helix(method string, endpoint Endpoint) *ActionBuilder {
	a.claimRouteForm(KindHelix)
	a.entry.act.Method = method
	a.entry.act.Endpoint = string(endpoint)
	return a
}

// As sets the default token identity the call executes under.
func (a *ActionBuilder) As(identity Identity) *ActionBuilder {
	a.entry.act.As = string(identity)
	return a
}

// Passthrough declares a generic Helix call that must bring its own endpoint.
func (a *ActionBuilder) Passthrough() *ActionBuilder {
	a.claimRouteForm(KindPassthrough)
	return a
}

// Internal declares a job with no Helix route of its own; Run owns every
// Twitch interaction itself.
func (a *ActionBuilder) Internal() *ActionBuilder {
	a.claimRouteForm(KindInternal)
	return a
}

// claimRouteForm records the chosen route form; a second claim marks the
// action redeclared so Validate rejects the conflict.
func (a *ActionBuilder) claimRouteForm(kind Kind) {
	if a.entry.routed {
		a.entry.redeclared = true
	}
	a.entry.routed = true
	a.entry.act.Kind = kind
}

// Run sets the action's handler and finishes it. It is terminal: it returns
// nothing so a declaration cannot accidentally continue past it.
func (a *ActionBuilder) Run(fn RunFunc) { a.entry.act.Run = fn }
