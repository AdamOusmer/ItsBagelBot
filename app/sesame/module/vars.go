package module

import (
	"math/rand/v2"
	"strconv"
	"strings"
)


// Expand performs a single-pass {key} substitution over tmpl, appending the
// result into dst and returning the grown slice. It allocates nothing of its
// own: the caller passes a pooled scratch buffer (see GetBuf) as dst.
//
// Literal runs are copied verbatim. On a "{key}" span, repl is asked for the
// key's value: if it returns ok, the value is appended; otherwise the literal
// "{key}" (braces included) is preserved, so an unknown token is left untouched
// rather than silently dropped. A '{' with no matching '}' is copied literally
// through to the end.
//
// Token names are case-insensitive: the key's name — everything before the
// first ':' — is lowercased before repl sees it, so {User} and {USER} resolve
// like {user}. A payload after the ':' keeps its case ({choice:Hi,Yo} offers
// "Hi"), so every repl matches against lowercase names only.
func Expand(dst []byte, tmpl string, repl func(key string) (val string, ok bool)) []byte {
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
	if val, ok := repl(normalizeKey(tmpl[open+1 : end])); ok {
		return append(dst, val...)
	}
	return append(dst, tmpl[open:end+1]...)
}

// normalizeKey lowercases a token's name — the part before the first ':' —
// leaving any payload untouched, so {Random:1-6} normalizes to random:1-6 but
// {choice:Hi,Yo} keeps its option casing.
func normalizeKey(key string) string {
	if name, payload, found := strings.Cut(key, ":"); found {
		return strings.ToLower(name) + ":" + payload
	}
	return strings.ToLower(key)
}


// ExpandString wraps Expand for callers who do not pool their own buffers,
// returning a newly allocated string.
func ExpandString(tmpl string, repl func(key string) (val string, ok bool)) string {
	if tmpl == "" {
		return ""
	}
	// Pre-allocate a reasonable guess to avoid growth. Most chat messages are small.
	dst := make([]byte, 0, len(tmpl)+32)
	return string(Expand(dst, tmpl, repl))
}

// ParseDynamic evaluates generic dynamic variables like {random}, {random:min-max},
// or {choice:a,b,c}. Callers can fall back to this in their repl callbacks.
func ParseDynamic(key string) (string, bool) {
	if key == "random" {
		return strconv.Itoa(rand.IntN(100) + 1), true
	}
	if strings.HasPrefix(key, "random:") {
		parts := strings.SplitN(strings.TrimPrefix(key, "random:"), "-", 2)
		if len(parts) == 2 {
			min, err1 := strconv.Atoi(parts[0])
			max, err2 := strconv.Atoi(parts[1])
			if err1 == nil && err2 == nil && max >= min {
				return strconv.Itoa(rand.IntN(max-min+1) + min), true
			}
		}
		return "", false
	}
	if strings.HasPrefix(key, "choice:") {
		choices := strings.Split(strings.TrimPrefix(key, "choice:"), ",")
		if len(choices) > 0 {
			return choices[rand.IntN(len(choices))], true
		}
	}
	return "", false
}
