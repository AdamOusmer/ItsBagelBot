package engine

import (
	"strings"

	"ItsBagelBot/app/sesame/module"
)

// tokens are the substitution values a custom-command response can reference.
type tokens struct {
	user    string
	sender  string
	args    string
	touser  string
	channel string
}

// expandCommand expands a custom-command response, supporting the {user},
// {sender}, {args} and {touser} tokens. It is expand specialized for the command
// path. {target} is the dashboard-facing name for {touser}; both are kept as
// aliases so existing commands continue to work. dst should be a pooled scratch
// buffer.
func expandCommand(dst []byte, tmpl string, t tokens) []byte {
	return module.Expand(dst, tmpl, func(key string) (string, bool) {
		switch key {
		case "user":
			return strings.TrimPrefix(t.user, "@"), true
		case "sender":
			return strings.TrimPrefix(t.sender, "@"), true
		case "args":
			return t.args, true
		case "touser", "target":
			return strings.TrimPrefix(t.touser, "@"), true
		case "channel":
			return t.channel, true
		default:
			return module.ParseDynamic(key)
		}
	})
}

// sanitizeVar neutralizes a user-supplied command variable so it cannot inject a
// leading slash-verb into the expanded response. Leading spaces and slashes are
// trimmed; the rest is untouched (a URL's "http://" keeps its slashes because
// they are not leading).
func sanitizeVar(s string) string {
	return trimLeftSlashSpace(s)
}

func trimLeftSlashSpace(s string) string {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '/') {
		i++
	}
	return s[i:]
}
