// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package module

import (
	"math/rand/v2"
	"strconv"
	"strings"

	"ItsBagelBot/pkg/tmpl"
)

// Expand performs a single-pass {key} substitution over s, appending the
// result into dst and returning the grown slice. It allocates nothing of its
// own: the caller passes a pooled scratch buffer (see GetBuf) as dst.
//
// It is a thin delegation to pkg/tmpl, which owns the scanner and documents
// its brace, case and unknown-token rules. The name stays here because every
// module and the engine's own variable expansion speak in terms of it, and
// because a module's repl closure is what adds the ParseDynamic fallthrough
// the shared scanner deliberately knows nothing about.
func Expand(dst []byte, s string, repl func(key string) (val string, ok bool)) []byte {
	return tmpl.Append(dst, s, repl)
}

// ExpandString wraps Expand for callers who do not pool their own buffers,
// returning a newly allocated string.
func ExpandString(s string, repl func(key string) (val string, ok bool)) string {
	return tmpl.Expand(s, repl)
}

// ParseDynamic evaluates generic dynamic variables like {random}, {random:min-max},
// or {choice:a,b,c}. Callers can fall back to this in their repl callbacks.
func ParseDynamic(key string) (string, bool) {
	if key == "random" {
		return strconv.Itoa(rand.IntN(100) + 1), true
	}
	if strings.HasPrefix(key, "random:") {
		parts := strings.SplitN(strings.TrimPrefix(key, "random:"), "-", 2)
		if len(parts) == 2 {
			min, err1 := strconv.Atoi(parts[0])
			max, err2 := strconv.Atoi(parts[1])
			if err1 == nil && err2 == nil && max >= min {
				return strconv.Itoa(rand.IntN(max-min+1) + min), true
			}
		}
		return "", false
	}
	if strings.HasPrefix(key, "choice:") {
		choices := strings.Split(strings.TrimPrefix(key, "choice:"), ",")
		if len(choices) > 0 {
			return choices[rand.IntN(len(choices))], true
		}
	}
	return "", false
}

// TokenExpander maps a template token name to the accessor that renders it
// from one reply type. Every module that answers a chat command from an
// upstream reply had written the same closure — look the key up in my map,
// else fall through to the dynamic vars — so the palette now carries the
// expansion instead of each caller re-deriving it.
//
// It is keyed on *R rather than R because the callers decode into a local and
// pass its address; copying a reply struct per token lookup would be the only
// alternative.
type TokenExpander[R any] map[string]func(*R) string

// Expand renders tmpl over r: a {token} this palette knows resolves from the
// reply, anything else falls through to the generic dynamic vars ({random},
// {choice:…}) and is left literal when even those do not know it.
func (t TokenExpander[R]) Expand(tmpl string, r *R) string {
	return ExpandString(tmpl, func(key string) (string, bool) {
		if field, ok := t[key]; ok {
			return field(r), true
		}
		return ParseDynamic(key)
	})
}

// StringPalette is a token palette whose values are already rendered strings.
//
// It is the non-generic sibling of TokenExpander, for replies whose tokens
// cannot be read off the reply alone: mcsr renders {elo} as the word
// "unrated" and {result} as a translated win/loss/draw, so its palettes are
// built per call against the channel's locale and merged out of fragments two
// commands share. Pushing those through TokenExpander[R] would mean inventing
// a view type per command whose only job is to carry a locale next to the
// reply.
type StringPalette map[string]string

// Expand renders tmpl over the palette: a {token} it holds resolves to that
// value, anything else falls through to the generic dynamic vars ({random},
// {choice:…}) and is left literal when even those do not know it. This is the
// same resolution order TokenExpander uses, so a broadcaster's template
// behaves identically whichever palette answers it.
func (p StringPalette) Expand(tmpl string) string {
	return ExpandString(tmpl, func(key string) (string, bool) {
		if val, ok := p[key]; ok {
			return val, true
		}
		return ParseDynamic(key)
	})
}

// Merge folds fragment palettes into p, later entries winning, and returns the
// combined palette. p itself is not modified: a fragment shared by two
// commands is usually built by a helper both call, and merging in place would
// let one command's extra tokens leak into the other's.
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
