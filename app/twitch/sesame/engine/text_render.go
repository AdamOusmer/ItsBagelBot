// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"

	"ItsBagelBot/app/twitch/sesame/engine/scope"
	"ItsBagelBot/pkg/tmpl"
)

type textRender struct {
	lex     func(text string) []tmpl.Token
	chainOf func(toks []tmpl.Token) scope.Chain
	onErr   func(error)
}

func (r textRender) expand(ctx context.Context, text string) string {
	flat, toks := r.tokens(ctx, text)
	if !hasVarToken(toks) {
		return flat
	}
	return r.render(ctx, toks)
}

func (r textRender) tokens(ctx context.Context, text string) (string, []tmpl.Token) {
	flat := tmpl.Unnest(text, func(inner []tmpl.Token) []string {
		chain := r.chainOf(tmpl.WithCondRefs(inner))
		values := chain.Plan(ctx, inner, r.onErr)
		out := make([]string, len(inner))
		for i := range inner {
			out[i] = string(chain.Render(nil, inner[i:i+1], values))
		}
		return out
	})
	return flat, r.lex(flat)
}

func (r textRender) render(ctx context.Context, toks []tmpl.Token) string {
	chain := r.chainOf(tmpl.WithCondRefs(toks))
	values := chain.Plan(ctx, toks, r.onErr)
	buf := chain.Render(GetBuf(), toks, values)
	out := string(buf)
	PutBuf(buf)
	return out
}

func hasVarToken(toks []tmpl.Token) bool {
	for _, tok := range toks {
		if tok.Kind == tmpl.KindVar {
			return true
		}
	}
	return false
}
