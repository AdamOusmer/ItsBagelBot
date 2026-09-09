// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import "context"

// urlFetchName is the token this scope answers: {urlfetch:name} renders the
// value gossip's custom.fetch endpoint extracted for the broadcaster-authored
// definition "name" ({urlfetch:name.a.b} selects a dotted path into the
// fetched document). Nothing is mutated — the token is a pure read.
const urlFetchName = "urlfetch"

// Fetcher resolves a batch of definition names in one fan-out. The engine
// implements it over gossip, and owns everything that is not grammar: the
// redelivery claim, the per-name concurrency, the failure-text table and the
// ExternalVar cap on whatever a third party returned.
//
// A name absent from the returned map stays literal, which is how a missing or
// inactive definition reads as the authoring mistake it is.
type Fetcher interface {
	Fetch(ctx context.Context, names []string) map[string]string
}

// External answers {urlfetch:...}. It is last in the chain: it is the only
// scope whose Plan leaves the process, so nothing it does can delay a token
// another scope could have answered.
//
// It is mounted only when a fetch caller is wired.
type External struct {
	Fetcher Fetcher
	// Max caps how many distinct payloads one response may fan out; it is
	// also the concurrency bound the fetcher works under. Payloads past the
	// cap stay verbatim, exactly like an unknown token.
	Max int
}

// Owns claims the urlfetch token family.
func (External) Owns(name string) bool { return name == urlFetchName }

// Plan fans every distinct payload out once, with ctx, before any rendering
// happens — the whole reason this package has a Plan phase. A repl callback is
// synchronous and ctx-free, so a network call inside one is impossible to
// write here rather than merely discouraged.
func (e External) Plan(ctx context.Context, wants []Var) (Values, error) {
	names := fetchNames(wants, e.Max)
	if len(names) == 0 {
		return urlValues(nil), nil
	}
	return urlValues(e.Fetcher.Fetch(ctx, names)), nil
}

// fetchNames folds each payload and returns the distinct ones in
// first-appearance order, capped at max.
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

// fetchName folds one span's payload into the definition name; "" for a span
// that names no definition ({urlfetch} or {urlfetch:}), which stays literal.
func fetchName(v Var) string {
	if !v.HasPayload {
		return ""
	}
	return NormalizeName(v.Payload)
}

// urlValues is one run's fetched text, keyed by the folded payload so
// "{URLFETCH:Temp}" reads the value "{urlfetch:temp}" fetched.
type urlValues map[string]string

func (m urlValues) Get(v Var) (string, bool) {
	name := fetchName(v)
	if name == "" {
		return "", false
	}
	value, ok := m[name]
	return value, ok
}
