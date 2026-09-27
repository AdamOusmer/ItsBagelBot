// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"ItsBagelBot/app/twitch/sesame/engine/scope"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/tmpl"
	"context"
	"go.uber.org/zap"
	"strings"
)

func (p *Pipeline) namespaceChain(ctx context.Context, run commandRun, toks []tmpl.Token, base scope.Chain) scope.Chain {
	sources := make(map[string]scope.NamespaceSource)
	denied := make(map[string]scope.NamespaceSource)
	checked := make(map[string]bool)
	for _, tok := range toks {
		if checked[tok.Name] {
			continue
		}
		m, known := p.namespaceModule(tok)
		if !known {
			continue
		}
		checked[tok.Name] = true
		reader := newNamespaceReads(p, nil, m.Variables)
		c, enabled := p.namespaceContext(ctx, run.c, m)
		if !enabled {
			denied[m.Name] = reader
			continue
		}
		reader.c = c
		sources[m.Name] = reader
	}
	if len(sources)+len(denied) == 0 {
		return base
	}
	return scope.Chain{scope.Namespaced{Base: base, Sources: sources, Denied: denied, OnError: func(err error) {
		p.log.Warn("command variables: read failed", module.BIDField(run.c.BroadcasterID), zap.Error(err))
	}}}
}

// namespaceModule recognizes published fields before legacy scopes claim a head.
func (p *Pipeline) namespaceModule(tok tmpl.Token) (module.Module, bool) {
	if tok.Kind != tmpl.KindVar || !tok.HasPayload {
		return module.Module{}, false
	}
	m, found := p.registry.variableModules[tok.Name]
	if !found {
		return module.Module{}, false
	}
	return m, newNamespaceReads(p, nil, m.Variables).Has(scope.NamespaceField(tok))
}

func (p *Pipeline) namespaceContext(ctx context.Context, original *module.Context, m module.Module) (*module.Context, bool) {
	views, available := p.namespaceViews(ctx, original.BroadcasterID, m)
	if !available {
		return nil, false
	}
	c := *original
	// Share native gates so default, trial and premium restrictions cannot drift.
	if !p.enabled(m, views, &c) {
		return nil, false
	}
	if !p.namespaceParentEnabled(ctx, c.BroadcasterID, m.Name) {
		return nil, false
	}
	return &c, true
}

func (p *Pipeline) namespaceViews(ctx context.Context, id uint64, m module.Module) (map[string]projection.ModuleView, bool) {
	views := make(map[string]projection.ModuleView, 1)
	if m.Kind == module.KindCore || m.Trial {
		return views, true
	}
	if p.proj == nil {
		return nil, false
	}
	view, found, err := p.proj.Module(ctx, id, m.Name)
	if err != nil {
		return nil, false
	}
	if found {
		views[m.Name] = view
	}
	return views, true
}

func (p *Pipeline) namespaceParentEnabled(ctx context.Context, id uint64, name string) bool {
	switch name {
	case "gamble", "duel":
	default:
		return true
	}
	if p.proj == nil {
		return false
	}
	parent, found, err := p.proj.Module(ctx, id, LoyaltyModuleName)
	if err != nil {
		return false
	}
	if !found {
		return false
	}
	return parent.IsEnabled
}

type namespaceReads struct {
	p      *Pipeline
	c      *module.Context
	groups []module.VariableGroup
	fields map[string]int
}

func newNamespaceReads(p *Pipeline, c *module.Context, groups []module.VariableGroup) namespaceReads {
	s := namespaceReads{p: p, c: c, groups: groups, fields: make(map[string]int)}
	for i, group := range groups {
		for _, field := range group.Fields {
			field = strings.ToLower(field)
			if _, claimed := s.fields[field]; !claimed {
				s.fields[field] = i
			}
			s.fields[strings.ToLower(group.Name)+":"+field] = i
		}
	}
	return s
}

func (s namespaceReads) Has(field string) bool {
	_, found := s.fields[field]
	return found
}

func (s namespaceReads) Read(ctx context.Context, fields []string) map[string]string {
	loaded := make(map[int]map[string]string)
	out := make(map[string]string, len(fields))
	for _, field := range fields {
		i := s.fields[field]
		group := s.groups[i]
		values, read := loaded[i]
		if !read {
			values = s.readGroup(ctx, group)
			loaded[i] = values
		}
		key := field
		if prefix := strings.ToLower(group.Name) + ":"; strings.HasPrefix(field, prefix) {
			key = field[len(prefix):]
		}
		out[field] = values[key]
	}
	return out
}

func (s namespaceReads) readGroup(ctx context.Context, group module.VariableGroup) map[string]string {
	if group.Read == nil {
		return nil
	}
	values, err := group.Read(ctx, s.c)
	if err != nil {
		s.p.log.Warn("module variables: read failed", module.BIDField(s.c.BroadcasterID), zap.String("view", group.Name), zap.Error(err))
		return nil
	}
	return values
}
