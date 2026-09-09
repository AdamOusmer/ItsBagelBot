// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package module

import "ItsBagelBot/pkg/tmpl"

// Expand performs a single-pass {token} substitution over s, appending the
// result into dst and returning the grown slice. It allocates nothing of its
// own: the caller passes a pooled scratch buffer (see GetBuf) as dst.
//
// It is a thin delegation to pkg/tmpl, which owns the scanner and documents
// its brace, case and unknown-token rules. The name stays here because every
// module and the engine's own variable expansion speak in terms of it.
//
// repl is handed the lexed tmpl.Token, not a key string: a module that wants
// the whole "name:payload" lookup calls tok.Key(), and one that wants the
// payload alone reads tok.Payload instead of splitting the key back apart.
// That is the point of the type — no surface outside pkg/tmpl re-parses a
// span.
func Expand(dst []byte, s string, repl func(tok tmpl.Token) (val string, ok bool)) []byte {
	return tmpl.Append(dst, s, repl)
}

// ExpandString wraps Expand for callers who do not pool their own buffers,
// returning a newly allocated string.
func ExpandString(s string, repl func(tok tmpl.Token) (val string, ok bool)) string {
	return tmpl.Expand(s, repl)
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
// reply, anything else falls through to the generic dynamic spans ({random},
// {choice:…}) and is left literal when even those do not know it.
func (t TokenExpander[R]) Expand(text string, r *R) string {
	return ExpandString(text, func(tok tmpl.Token) (string, bool) {
		if field, ok := t[tok.Key()]; ok {
			return field(r), true
		}
		return tmpl.Dynamic(tok)
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
func (p StringPalette) Expand(text string) string {
	return ExpandString(text, func(tok tmpl.Token) (string, bool) {
		if val, ok := p[tok.Key()]; ok {
			return val, true
		}
		return tmpl.Dynamic(tok)
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
