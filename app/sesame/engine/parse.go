package engine

import "strings"

// parseCommand extracts the command name and argument string from chat text.
// "!so @bob hi" -> ("so", "@bob hi", true). Non-commands (no leading '!', or a
// bare "!") return ok=false. The name is lowercased; args are trimmed.
func parseCommand(text string) (name, args string, ok bool) {
	trimmed := strings.TrimLeft(text, " ")
	if !strings.HasPrefix(trimmed, "!") {
		return "", "", false
	}
	body := strings.TrimPrefix(trimmed, "!")
	name, args, _ = strings.Cut(body, " ")
	if name == "" {
		return "", "", false
	}
	return strings.ToLower(name), strings.TrimSpace(args), true
}


// splitTrailingDigits splits a run of trailing ASCII digits off the end of s.
// "clip30" -> ("clip", "30"); "clip" -> ("clip", ""). It is used by the command
// resolver to match a numeric-suffix trigger like !clip30.
func splitTrailingDigits(s string) (base, digits string) {
	i := len(s)
	for i > 0 && s[i-1] >= '0' && s[i-1] <= '9' {
		i--
	}
	return s[:i], s[i:]
}
