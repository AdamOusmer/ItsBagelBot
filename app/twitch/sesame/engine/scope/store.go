// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"context"
	"strings"

	"ItsBagelBot/pkg/tmpl"
)

const (
	counterName = "counter"
	countName   = "count"
)

const targetCounterPrefix = "target:"

func NormalizeName(name string) string {
	return tmpl.NormalizeName(name)
}

type Peeks interface {
	Peek(ctx context.Context, name string, addressed bool) (value string)
}

type Store struct {
	Peeks Peeks
}

func (s Store) Owns(v Var) bool {
	if s.Peeks == nil {
		return false
	}
	switch v.Name {
	case counterName:
		return true
	case countName:
		return v.HasPayload
	}
	return false
}

func (s Store) Plan(ctx context.Context, wants []Var) (Values, error) {
	out := &storeValues{peeked: make(map[string]string, len(wants))}
	for _, v := range wants {
		s.peekInto(ctx, out, v)
	}
	return out, nil
}

func (s Store) peekInto(ctx context.Context, out *storeValues, v Var) {
	ref, ok := counterRefOf(v)
	if !ok {
		return
	}
	if _, done := out.peeked[ref.key]; done {
		return
	}
	out.peeked[ref.key] = s.Peeks.Peek(ctx, ref.name, ref.addressed)
}

type counterRef struct {
	key       string
	name      string
	addressed bool
}

func counterRefOf(v Var) (counterRef, bool) {
	if !v.HasPayload {
		return counterRef{}, false
	}
	key := NormalizeName(v.Payload)
	base, addressed := strings.CutPrefix(key, targetCounterPrefix)
	if base == "" {
		return counterRef{}, false
	}
	return counterRef{key: key, name: base, addressed: addressed}, true
}

type storeValues struct {
	peeked map[string]string
}

func (m *storeValues) Get(v Var) (string, bool) {
	ref, ok := counterRefOf(v)
	if !ok {
		return "", false
	}
	value, ok := m.peeked[ref.key]
	return value, ok
}
