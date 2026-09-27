// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"context"
	"strings"
)

// NamespaceSource batches the requested public facts of one enabled module.
type NamespaceSource interface {
	Has(field string) bool
	Read(context.Context, []string) map[string]string
}

// Namespaced wraps the usual command scopes so an existing name such as
// {time} can keep working beside {time:date}. Unsupported fields fall through
// to the usual scopes and remain literal if neither resolver knows them.
type Namespaced struct {
	Base    Chain
	Sources map[string]NamespaceSource
	// Denied reserves published fields of disabled modules from legacy scopes.
	Denied  map[string]NamespaceSource
	OnError func(error)
}

func (n Namespaced) Owns(tok Var) bool {
	if source := n.Sources[tok.Name]; source != nil && source.Has(NamespaceField(tok)) {
		return true
	}
	if source := n.Denied[tok.Name]; source != nil && source.Has(NamespaceField(tok)) {
		return false
	}
	for _, s := range n.Base {
		if s.Owns(tok) {
			return true
		}
	}
	return false
}

func NamespaceField(tok Var) string {
	if !tok.HasPayload {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(tok.Payload))
}

func (n Namespaced) Plan(ctx context.Context, wants []Var) (Values, error) {
	byModule := make(map[string][]string)
	var base []Var
	for _, want := range wants {
		field := NamespaceField(want)
		if source := n.Sources[want.Name]; source != nil && source.Has(field) {
			byModule[want.Name] = append(byModule[want.Name], field)
		} else if source := n.Denied[want.Name]; source == nil || !source.Has(field) {
			base = append(base, want)
		}
	}
	values := namespaceValues{base: n.Base.Plan(ctx, base, n.OnError), modules: make(map[string]map[string]string)}
	for name, fields := range byModule {
		values.modules[name] = n.Sources[name].Read(ctx, fields)
	}
	return values, nil
}

type namespaceValues struct {
	base    Values
	modules map[string]map[string]string
}

func (v namespaceValues) Get(tok Var) (string, bool) {
	if fields, ok := v.modules[tok.Name]; ok {
		if value, found := fields[NamespaceField(tok)]; found {
			return value, true
		}
	}
	return v.base.Get(tok)
}
