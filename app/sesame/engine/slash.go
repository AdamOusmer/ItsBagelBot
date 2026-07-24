package engine

import (
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
)

// Translate inspects out.Text for a leading slash-verb and, when it finds a
// known one, rewrites the Output in place: it sets the outgress Type (and Color
// or To where the verb carries one) and strips the verb prefix from Text. An
// author writes the verb the same way they would in chat (e.g.
// "/announce hello"), and the engine turns it into the right outgress action.
//
// The verb grammar lives in outgress.CutSlash — the single owner shared with
// outgress's own synthetic sends — see its doc for the recognized verbs. /me
// is a plain passthrough: Twitch chat interprets the leading "/me" itself, so
// the verb is NOT stripped and the output stays chat.
//
// Only a chat output is translated. That makes the call idempotent — an
// already-routed action (announce/shoutout/pin) is never re-parsed — so it is
// safe both in the custom-command path (which translates per line before
// batching) and centrally in the pipeline's emit, where EVERY module output
// passes through it.
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

// isEmptyAction reports whether a translated output carries no usable payload, so
// dispatch can skip emitting a call Twitch would reject: an /announce with no
// message, a /shoutout with no target, or an empty chat line.
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
