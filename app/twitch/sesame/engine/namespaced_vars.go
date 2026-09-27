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
		if tok.Kind != tmpl.KindVar || !tok.HasPayload || checked[tok.Name] {
			continue
		}
		m, found := p.registry.variableModules[tok.Name]
		if !found {
			continue
		}
		reader := newNamespaceReads(p, nil, m.Variables)
		if !reader.Has(scope.NamespaceField(tok)) {
			continue
		}
		checked[tok.Name] = true
		denied[m.Name] = reader
		c := *run.c
		views := make(map[string]projection.ModuleView, 1)
		if m.Kind != module.KindCore && !m.Trial {
			if p.proj == nil {
				continue
			}
			view, found, err := p.proj.Module(ctx, c.BroadcasterID, m.Name)
			if err != nil {
				continue
			}
			if found {
				views[m.Name] = view
			}
		}
		// Reuse the command/event gate so defaults, trials and premium restrictions
		// cannot drift between native commands and custom-command variables.
		if !p.enabled(m, views, &c) {
			continue
		}
		if m.Name == "gamble" || m.Name == "duel" {
			if p.proj == nil {
				continue
			}
			parent, found, err := p.proj.Module(ctx, c.BroadcasterID, LoyaltyModuleName)
			if err != nil || !found || !parent.IsEnabled {
				continue
			}
		}
		reader.c = &c
		sources[m.Name] = reader
		delete(denied, m.Name)
	}
	if len(sources) == 0 && len(denied) == 0 {
		return base
	}
	return scope.Chain{scope.Namespaced{Base: base, Sources: sources, Denied: denied, OnError: func(err error) {
		p.log.Warn("command variables: read failed", module.BIDField(run.c.BroadcasterID), zap.Error(err))
	}}}
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
			if group.Read != nil {
				var err error
				values, err = group.Read(ctx, s.c)
				if err != nil {
					s.p.log.Warn("module variables: read failed", module.BIDField(s.c.BroadcasterID), zap.String("view", group.Name), zap.Error(err))
					values = nil
				}
			}
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
