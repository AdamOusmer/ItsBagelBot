package module

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
		c := tmpl[i]
		if c != '{' {
			dst = append(dst, c)
			i++
			continue
		}
		// Find the closing brace.
		end := -1
		for j := i + 1; j < len(tmpl); j++ {
			if tmpl[j] == '}' {
				end = j
				break
			}
		}
		if end < 0 {
			// No closing brace: copy the rest literally.
			dst = append(dst, tmpl[i:]...)
			break
		}
		key := tmpl[i+1 : end]
		if val, ok := repl(key); ok {
			dst = append(dst, val...)
		} else {
			// Unknown token: keep it literal, braces and all.
			dst = append(dst, tmpl[i:end+1]...)
		}
		i = end + 1
	}
	return dst
}

// expandCommand expands a custom-command response, supporting the {user},
// {sender}, {args} and {touser} tokens. It is expand specialized for the
// command path so the hot path needs no closure capture beyond the four
// strings. {target} is the dashboard-facing name for {touser}; both are kept
// as aliases so existing commands continue to work. dst should be a pooled
// scratch buffer.
func expandCommand(dst []byte, tmpl, user, sender, args, touser string) []byte {
	return expand(dst, tmpl, func(key string) (string, bool) {
		switch key {
		case "user":
			return user, true
		case "sender":
			return sender, true
		case "args":
			return args, true
		case "touser", "target":
			return touser, true
		default:
			return "", false
		}
	})
}
