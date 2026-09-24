// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
)

func Translate(out *module.Output) {
	if out.Type != outgress.TypeChat {
		return
	}
	sc, ok := outgress.CutSlash(out.Text)
	if !ok {
		return
	}
	out.Type = sc.Type
	out.Color = sc.Color
	out.To = sc.To
	out.Text = sc.Text
}

func isEmptyAction(out *module.Output) bool {
	switch out.Type {
	case outgress.TypeAnnounce, outgress.TypePin, outgress.TypeChat:
		return out.Text == ""
	case outgress.TypeShoutout:
		return out.To == ""
	default:
		return false
	}
}
