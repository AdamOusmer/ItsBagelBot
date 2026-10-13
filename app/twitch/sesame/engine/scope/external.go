// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"context"
	"slices"
)

const urlFetchName = "urlfetch"

type Fetcher interface {
	Fetch(ctx context.Context, names []string) map[string]string
}

type External struct {
	Fetcher Fetcher
	Max     int
}

func (External) Owns(v Var) bool { return v.Name == urlFetchName }

func (e External) Plan(ctx context.Context, wants []Var) (Values, error) {
	names := fetchNames(wants, e.Max)
	if len(names) == 0 {
		return urlValues{}, nil
	}
	return urlValues{values: e.Fetcher.Fetch(ctx, names), asked: names}, nil
}

func fetchNames(wants []Var, max int) []string {
	var names []string
	seen := make(map[string]struct{}, len(wants))
	for _, v := range wants {
		name := fetchName(v)
		if _, dup := seen[name]; name == "" || dup {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	if len(names) > max {
		return names[:max]
	}
	return names
}

func fetchName(v Var) string {
	if !v.HasPayload {
		return ""
	}
	return NormalizeName(v.Payload)
}

type urlValues struct {
	values map[string]string
	asked  []string
}

func (m urlValues) Get(v Var) (string, bool) {
	name := fetchName(v)
	if name == "" {
		return "", false
	}
	if value, ok := m.values[name]; ok {
		return value, true
	}
	return "", v.HasFallback && slices.Contains(m.asked, name)
}
