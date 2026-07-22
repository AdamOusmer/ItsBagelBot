package engine

import (
	"slices"
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
	// counters holds the pre-resolved {counter:<name>} values for this run,
	// keyed by normalized name. runCustom bumps each referenced counter once
	// (with ctx) before expansion, so the sync callback only looks values up.
	counters map[string]string
}

// counterTokenPrefix marks the counter substitution inside a response
// template: {counter:deaths} bumps the broadcaster's "deaths" counter by one
// and renders the new value.
const counterTokenPrefix = "counter:"

// botCounterTokenPrefix marks a bot-scope counter reference inside a counter
// token ({counter:bot:feeds}). Bot counters are admin-only: broadcaster
// commands never resolve or bump them, so the token is skipped and stays
// visible, exactly like any other unknown token. Only admin/system-authored
// content may resolve it.
const botCounterTokenPrefix = "bot:"

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
			if name, ok := strings.CutPrefix(key, counterTokenPrefix); ok {
				v, ok := t.counters[NormalizeCounterName(name)]
				return v, ok // unresolved (no loyalty store): leave the token visible
			}
			return module.ParseDynamic(key)
		}
	})
}

// counterTokenNames scans a response template for {counter:<name>} tokens and
// returns the distinct normalized names, in first-appearance order. nil when
// the template references none — the fast path for every ordinary command.
func counterTokenNames(tmpl string) []string {
	var names []string
	rest := tmpl
	for {
		i := strings.Index(rest, "{"+counterTokenPrefix)
		if i < 0 {
			return names
		}
		rest = rest[i+len(counterTokenPrefix)+1:]
		end := strings.IndexByte(rest, '}')
		if end < 0 {
			return names
		}
		name := NormalizeCounterName(rest[:end])
		rest = rest[end+1:]
		if name != "" && !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
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
