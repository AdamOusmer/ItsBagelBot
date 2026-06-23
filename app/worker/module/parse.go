package module

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
