package engine

// expand performs a single-pass {key} substitution over tmpl, appending the
// result into dst and returning the grown slice. It allocates nothing of its
// own: the caller passes a pooled scratch buffer (see GetBuf) as dst.
//
// Literal runs are copied verbatim. On a "{key}" span, repl is asked for the
// key's value: if it returns ok, the value is appended; otherwise the literal
// "{key}" (braces included) is preserved, so an unknown token is left untouched
// rather than silently dropped. A '{' with no matching '}' is copied literally
// through to the end.
func expand(dst []byte, tmpl string, repl func(key string) (val string, ok bool)) []byte {
	for i := 0; i < len(tmpl); {
		if tmpl[i] != '{' {
			dst = append(dst, tmpl[i])
			i++
			continue
		}
		end := closeBrace(tmpl, i+1)
		if end < 0 {
			// No closing brace: copy the rest literally.
			return append(dst, tmpl[i:]...)
		}
		dst = appendToken(dst, tmpl, i, end, repl)
		i = end + 1
	}
	return dst
}

// closeBrace returns the index of the next '}' at or after from, or -1.
func closeBrace(s string, from int) int {
	for j := from; j < len(s); j++ {
		if s[j] == '}' {
			return j
		}
	}
	return -1
}

// appendToken resolves the "{key}" span tmpl[open:end+1] and appends either its
// value or, for an unknown key, the literal span (braces and all).
func appendToken(dst []byte, tmpl string, open, end int, repl func(key string) (val string, ok bool)) []byte {
	if val, ok := repl(tmpl[open+1 : end]); ok {
		return append(dst, val...)
	}
	return append(dst, tmpl[open:end+1]...)
}

// tokens are the substitution values a custom-command response can reference.
type tokens struct {
	user   string
	sender string
	args   string
	touser string
}

// expandCommand expands a custom-command response, supporting the {user},
// {sender}, {args} and {touser} tokens. It is expand specialized for the command
// path. {target} is the dashboard-facing name for {touser}; both are kept as
// aliases so existing commands continue to work. dst should be a pooled scratch
// buffer.
func expandCommand(dst []byte, tmpl string, t tokens) []byte {
	return expand(dst, tmpl, func(key string) (string, bool) {
		switch key {
		case "user":
			return t.user, true
		case "sender":
			return t.sender, true
		case "args":
			return t.args, true
		case "touser", "target":
			return t.touser, true
		default:
			return "", false
		}
	})
}
