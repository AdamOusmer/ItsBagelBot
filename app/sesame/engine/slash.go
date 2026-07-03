package engine

import (
	"strings"

	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
)

// Translate inspects out.Text for a leading slash-verb and, when it finds a
// known one, rewrites the Output in place: it sets the outgress Type (and Color
// or To where the verb carries one) and strips the verb prefix from Text. A
// command author writes the verb the same way they would in chat (e.g.
// "/announce hello"), and the engine turns it into the right outgress action.
//
// Recognized verbs:
//
//   - /announce[blue|green|orange|purple] -> TypeAnnounce, that color (plain
//     /announce is "primary"); the verb is stripped from Text.
//   - /shoutout <target> -> TypeShoutout; the first token (leading '@' stripped)
//     becomes To and is removed from Text.
//   - /me -> left as a plain chat line; the verb is NOT stripped (Twitch chat
//     interprets the leading "/me" itself).
//
// Anything else is left unchanged.
func Translate(out *module.Output) {
	text := out.Text

	// /announce family. Order matters: the colored variants are prefixes of the
	// bare verb's namespace, so check the longer forms first.
	if color, rest, ok := matchAnnounce(text); ok {
		out.Type = outgress.TypeAnnounce
		out.Color = color
		out.Text = rest
		return
	}

	// /shoutout <target>
	if rest, ok := cutVerb(text, "/shoutout"); ok {
		rest = strings.TrimLeft(rest, " ")
		target, remainder, _ := strings.Cut(rest, " ")
		target = strings.TrimPrefix(target, "@")
		out.Type = outgress.TypeShoutout
		out.To = target
		out.Text = strings.TrimLeft(remainder, " ")
		return
	}

	// /me is a plain passthrough: leave Type=chat and keep the verb in Text.
}

// isEmptyAction reports whether a translated output carries no usable payload, so
// dispatch can skip emitting a call Twitch would reject: an /announce with no
// message, a /shoutout with no target, or an empty chat line.
func isEmptyAction(out *module.Output) bool {
	switch out.Type {
	case outgress.TypeAnnounce, outgress.TypeChat:
		return out.Text == ""
	case outgress.TypeShoutout:
		return out.To == ""
	default:
		return false
	}
}

// matchAnnounce reports whether text leads with an /announce verb. It returns
// the announce color and the text with the verb (and one following space)
// stripped.
func matchAnnounce(text string) (color, rest string, ok bool) {
	type variant struct {
		verb  string
		color string
	}
	// Longest verbs first so "/announceblue" is not mistaken for "/announce".
	for _, v := range []variant{
		{"/announceblue", "blue"},
		{"/announcegreen", "green"},
		{"/announceorange", "orange"},
		{"/announcepurple", "purple"},
		{"/announce", "primary"},
	} {
		if r, matched := cutVerb(text, v.verb); matched {
			return v.color, r, true
		}
	}
	return "", "", false
}

// cutVerb matches verb either as the whole string or as a "verb " prefix. On a
// match it returns the remainder with the single separating space removed (empty
// when the text was exactly the verb). A "verb" followed by a non-space (e.g.
// "/announceblue" when matching "/announce") is not a match.
func cutVerb(text, verb string) (rest string, ok bool) {
	if text == verb {
		return "", true
	}
	if strings.HasPrefix(text, verb+" ") {
		return text[len(verb)+1:], true
	}
	return "", false
}
