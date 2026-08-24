// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package custom

import (
	"errors"
	"fmt"
	"strings"

	"ItsBagelBot/pkg/codec"
)

// Dot-path extraction for user-defined fetches: indexing and string coercion
// only, deliberately. A JSONPath dependency would pull a query language —
// filters, wildcards, scripts, recursion — into the one place broadcasters
// control the input; the console picker authors dotted paths with bare-digit
// array indices, and that grammar alone is what runs here.

// maxPathDepth caps def path + token tail combined.
//
// Eight is twice the deepest structure a sane JSON API answer needs to reach
// its scalar leaf (weather.current.temperature / data.items.0.name are depth
// 2-4); every level beyond that is either an API we do not want to couple to
// or an attempt to make us walk something enormous. It matches the shared
// validation table (console, commands service, gossip) so no author sees a
// save-time rule differ from a fetch-time rule.
const maxPathDepth = 8

// errPathInvalid marks malformed authoring: bad segment characters or too
// deep. Stable, so callers may negative-cache it.
var errPathInvalid = errors.New("invalid json path")

// buildPath validates the effective segments — the token path when the token
// carried one, otherwise the definition's stored path — converting bare-digit
// array indices into jsonparser's "[N]" form. Depth is capped at 8.
func buildPath(segments []string) (codec.Path, error) {
	full := segments
	if len(full) > maxPathDepth {
		return nil, fmt.Errorf("%w: deeper than %d", errPathInvalid, maxPathDepth)
	}
	out := make(codec.Path, 0, len(full))
	for _, seg := range full {
		if !validSegment(seg) {
			return nil, fmt.Errorf("%w: %q", errPathInvalid, seg)
		}
		if isDigits(seg) {
			// jsonparser addresses arrays by bracketed index.
			out = append(out, "["+seg+"]")
			continue
		}
		out = append(out, seg)
	}
	return out, nil
}

// validSegment enforces [A-Za-z0-9_-]+ — the same grammar the console's
// picker and the commands-service validation share, so nothing reaches here
// that could not have been authored in the editor.
func validSegment(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_', c == '-':
		default:
			return false
		}
	}
	return true
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// splitTokenPath splits "{urlfetch:name.path...}"'s DefID at its first dot:
// names carry no dots ([a-z0-9_]{1,32}), so the first dot always begins the
// per-token path. Empty input yields ("", nil).
func splitTokenPath(defID string) (name string, tail []string) {
	defID = strings.TrimSpace(defID)
	name, tailStr, _ := strings.Cut(defID, ".")
	name = strings.ToLower(name)
	if tailStr == "" {
		return name, nil
	}
	for _, seg := range strings.Split(tailStr, ".") {
		if seg != "" { // tolerate doubled dots from hand-typed tokens
			tail = append(tail, seg)
		}
	}
	return name, tail
}

// extractValues pulls the reply values out of an upstream body: the whole
// body as text for plain-kind defs (no path), otherwise the single scalar at
// the dot-path, coerced to string and rune-capped by the caller. An absent
// key, a null, or an object/array leaf is an error — broken authoring, not
// infrastructure.
func extractValues(body []byte, path codec.Path) ([]string, error) {
	if len(path) == 0 {
		// Plain kind: the response text itself is the value.
		return []string{strings.TrimSpace(string(body))}, nil
	}
	v, kind, err := codec.ExtractValue(body, path)
	if err != nil {
		return nil, err
	}
	switch kind {
	case codec.KindString:
		s, perr := codec.ParseString(v)
		if perr != nil {
			return nil, perr
		}
		return []string{s}, nil
	case codec.KindNumber, codec.KindBool:
		// Raw bytes ARE the canonical text form of these — no float
		// re-formatting drift between upstream and chat.
		return []string{string(v)}, nil
	default:
		return nil, fmt.Errorf("value at path is %s, not a scalar", kind)
	}
}
