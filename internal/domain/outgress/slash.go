package outgress

import "strings"

// SlashCommand is the routed form of a leading chat slash-verb in authored
// response text: the outgress action it becomes plus the fields that action
// carries. It is produced by CutSlash, the single owner of the verb grammar —
// sesame's engine translates module outputs through it and outgress routes
// its own synthetic sends (the clip reply) through it, so every path
// recognizes exactly the same verbs.
type SlashCommand struct {
	// Type is the outgress action: TypeAnnounce, TypeShoutout or TypePin.
	Type string
	// Color is the announce color (primary, blue, green, orange, purple);
	// empty for non-announce.
	Color string
	// To is the shoutout target with the leading '@' removed; empty otherwise.
	To string
	// Text is the message with the verb (and, for shoutout, the target)
	// consumed.
	Text string
}

// CutSlash inspects text for a leading slash-verb and returns its routed
// form. ok=false means the text is plain chat: no verb, an unknown verb, or
// /me — which Twitch chat renders itself, so the verb must stay in the text.
//
// Recognized verbs:
//
//   - /announce[blue|green|orange|purple] <message> -> TypeAnnounce + Color
//     (plain /announce is "primary"); the verb is stripped from Text.
//   - /shoutout <target> [message] -> TypeShoutout; the first token (leading
//     '@' stripped) becomes To and is removed from Text.
//   - /pin <message> -> TypePin; the verb is stripped from Text.
func CutSlash(text string) (SlashCommand, bool) {
	if color, rest, ok := cutAnnounce(text); ok {
		return SlashCommand{Type: TypeAnnounce, Color: color, Text: rest}, true
	}
	if rest, ok := cutVerb(text, "/shoutout"); ok {
		rest = strings.TrimLeft(rest, " ")
		target, remainder, _ := strings.Cut(rest, " ")
		return SlashCommand{
			Type: TypeShoutout,
			To:   strings.TrimPrefix(target, "@"),
			Text: strings.TrimLeft(remainder, " "),
		}, true
	}
	if rest, ok := cutVerb(text, "/pin"); ok {
		return SlashCommand{Type: TypePin, Text: rest}, true
	}
	return SlashCommand{}, false
}

// cutAnnounce reports whether text leads with an /announce verb. It returns
// the announce color and the text with the verb (and one following space)
// stripped.
func cutAnnounce(text string) (color, rest string, ok bool) {
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
