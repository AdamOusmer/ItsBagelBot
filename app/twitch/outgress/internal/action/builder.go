// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package action

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"ItsBagelBot/internal/domain/outgress"
)

type Endpoint string

type Identity string

type Builder struct {
	acts []*builderAction
}

type builderAction struct {
	act        Action
	routed     bool
	redeclared bool
}

func NewSet() *Builder {
	return &Builder{}
}

func (b *Builder) Action(messageType string) *ActionBuilder {
	entry := &builderAction{act: Action{Type: messageType}}
	b.acts = append(b.acts, entry)
	return &ActionBuilder{entry: entry}
}

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

func (b *Builder) Validate() error {
	claimed := make(map[string]struct{}, len(b.acts))
	for _, entry := range b.acts {
		if err := validateAction(claimed, entry); err != nil {
			return err
		}
	}
	return nil
}

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

func validateRouteForm(entry *builderAction) error {
	if !entry.routed {
		return fmt.Errorf("action %q declares no route form (chain Post/Put/Patch/Delete, Passthrough, or Internal)", entry.act.Type)
	}
	if entry.redeclared {
		return fmt.Errorf("action %q declares more than one route form", entry.act.Type)
	}
	return validateRoute(entry.act)
}

func validateRoute(a Action) error {
	if a.Kind == KindHelix {
		return validateHelixRoute(a)
	}
	if a.As != "" {
		return fmt.Errorf("%s action %q must not carry a token identity", a.Kind, a.Type)
	}
	return nil
}

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

type ActionBuilder struct {
	entry *builderAction
}

func (a *ActionBuilder) Post(endpoint Endpoint) *ActionBuilder {
	return a.helix(http.MethodPost, endpoint)
}

func (a *ActionBuilder) Put(endpoint Endpoint) *ActionBuilder {
	return a.helix(http.MethodPut, endpoint)
}

func (a *ActionBuilder) Patch(endpoint Endpoint) *ActionBuilder {
	return a.helix(http.MethodPatch, endpoint)
}

func (a *ActionBuilder) Delete(endpoint Endpoint) *ActionBuilder {
	return a.helix(http.MethodDelete, endpoint)
}

func (a *ActionBuilder) helix(method string, endpoint Endpoint) *ActionBuilder {
	a.claimRouteForm(KindHelix)
	a.entry.act.Method = method
	a.entry.act.Endpoint = string(endpoint)
	return a
}

func (a *ActionBuilder) As(identity Identity) *ActionBuilder {
	a.entry.act.As = string(identity)
	return a
}

func (a *ActionBuilder) Passthrough() *ActionBuilder {
	a.claimRouteForm(KindPassthrough)
	return a
}

func (a *ActionBuilder) Internal() *ActionBuilder {
	a.claimRouteForm(KindInternal)
	return a
}

func (a *ActionBuilder) claimRouteForm(kind Kind) {
	if a.entry.routed {
		a.entry.redeclared = true
	}
	a.entry.routed = true
	a.entry.act.Kind = kind
}

func (a *ActionBuilder) Run(fn RunFunc) { a.entry.act.Run = fn }
