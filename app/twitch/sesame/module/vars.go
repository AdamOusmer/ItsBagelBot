// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package module

import (
	"ItsBagelBot/app/twitch/sesame/engine/scope"
	"ItsBagelBot/pkg/tmpl"
)

func Expand(dst []byte, s string, repl func(tok tmpl.Token) (val string, ok bool)) []byte {
	return tmpl.Append(dst, s, repl)
}

func ExpandString(s string, repl func(tok tmpl.Token) (val string, ok bool)) string {
	return tmpl.Expand(s, repl)
}

func pureFallback(tok tmpl.Token) (string, bool) {
	values, _ := scope.Pure{}.Plan(nil, nil)
	return values.Get(tok)
}

type TokenExpander[R any] map[string]func(*R) string

func (t TokenExpander[R]) Expand(text string, r *R) string {
	return ExpandString(text, func(tok tmpl.Token) (string, bool) {
		if field, ok := t[tok.Key()]; ok {
			return field(r), true
		}
		return pureFallback(tok)
	})
}

func (t TokenExpander[R]) Names() []string {
	out := make([]string, 0, len(t))
	for name := range t {
		out = append(out, name)
	}
	return out
}

type StringPalette map[string]string

func (p StringPalette) Expand(text string) string {
	return ExpandString(text, func(tok tmpl.Token) (string, bool) {
		if val, ok := p[tok.Key()]; ok {
			return val, true
		}
		return pureFallback(tok)
	})
}

func (p StringPalette) Merge(parts ...StringPalette) StringPalette {
	out := make(StringPalette, len(p)+len(parts)*4)
	for key, val := range p {
		out[key] = val
	}
	for _, part := range parts {
		for key, val := range part {
			out[key] = val
		}
	}
	return out
}

type SpecEntry struct {
	Name string
	Doc  string
}

type Spec struct {
	Entries []SpecEntry
}

func (s Spec) Names() []string {
	out := make([]string, len(s.Entries))
	for i, e := range s.Entries {
		out[i] = e.Name
	}
	return out
}

func (s Spec) Bind(value func(name string) func() string) Palette {
	entries := make([]Entry, len(s.Entries))
	for i, e := range s.Entries {
		entries[i] = Entry{Name: e.Name, Doc: e.Doc, Value: value(e.Name)}
	}
	return Palette{entries: entries}
}

type Entry struct {
	Name  string
	Doc   string
	Value func() string
}

type Palette struct {
	entries []Entry
	locale  string
}

func (p Palette) Names() []string {
	out := make([]string, len(p.entries))
	for i, e := range p.entries {
		out[i] = e.Name
	}
	return out
}

func (p Palette) Merge(others ...Palette) Palette {
	order := make([]string, 0, len(p.entries))
	byName := make(map[string]Entry, len(p.entries))
	add := func(e Entry) {
		if _, ok := byName[e.Name]; !ok {
			order = append(order, e.Name)
		}
		byName[e.Name] = e
	}
	for _, e := range p.entries {
		add(e)
	}
	locale := p.locale
	for _, other := range others {
		for _, e := range other.entries {
			add(e)
		}
		if other.locale != "" {
			locale = other.locale
		}
	}
	out := make([]Entry, len(order))
	for i, name := range order {
		out[i] = byName[name]
	}
	return Palette{entries: out, locale: locale}
}

func (p Palette) Resolve(tok tmpl.Token) (string, bool) {
	for _, e := range p.entries {
		if e.Name == tok.Name {
			if tok.HasPayload {
				return "", false
			}
			return e.Value(), true
		}
	}
	values, _ := scope.Pure{Locale: p.locale}.Plan(nil, nil)
	return values.Get(tok)
}

func (p Palette) Expand(dst []byte, s string) []byte {
	return tmpl.Append(dst, s, p.Resolve)
}

func (p Palette) ExpandString(s string) string {
	return tmpl.Expand(s, p.Resolve)
}

type Locale string

func (p Palette) WithLocale(locale Locale) Palette {
	p.locale = string(locale)
	return p
}

func KV(kv ...string) Palette {
	order := make([]string, 0, len(kv)/2)
	byName := make(map[string]string, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		name, val := kv[i], kv[i+1]
		if name == "" {
			continue
		}
		if _, seen := byName[name]; !seen {
			order = append(order, name)
		}
		byName[name] = val
	}
	entries := make([]Entry, len(order))
	for i, name := range order {
		val := byName[name]
		entries[i] = Entry{Name: name, Value: func() string { return val }}
	}
	return Palette{entries: entries}
}

func Common(c *Context) Palette {
	return Palette{
		entries: []Entry{
			{Name: "user", Doc: "the invoking chatter's login", Value: func() string { return c.Env.ChatterUserLogin }},
			{Name: "channel", Doc: "the broadcaster's login", Value: func() string { return c.Env.BroadcasterUserLogin }},
		},
		locale: c.Locale,
	}
}
