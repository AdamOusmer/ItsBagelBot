// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"

	"ItsBagelBot/app/twitch/sesame/engine/scope"
	"ItsBagelBot/pkg/tmpl"
)

type chainFor func(toks []tmpl.Token) scope.Chain

func unnestText(ctx context.Context, text string, chainOf chainFor, onErr func(error)) string {
	return tmpl.Unnest(text, func(inner []tmpl.Token) []string {
		chain := chainOf(tmpl.WithCondRefs(inner))
		values := chain.Plan(ctx, inner, onErr)
		out := make([]string, len(inner))
		for i := range inner {
			out[i] = string(chain.Render(nil, inner[i:i+1], values))
		}
		return out
	})
}
