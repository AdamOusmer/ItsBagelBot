// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package custom

import (
	"errors"
	"fmt"
	"strings"

	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/codec"
)

// Dot-path indexing only: no JSONPath query language on broadcaster-controlled input.

var errPathInvalid = errors.New("invalid json path")

func buildPath(segments []string) (codec.Path, error) {
	if err := validate.FetchPath(segments); err != nil {
		return nil, fmt.Errorf("%w: %w", errPathInvalid, err)
	}
	out := make(codec.Path, 0, len(segments))
	for _, seg := range segments {
		if isDigits(seg) {
			out = append(out, "["+seg+"]")
			continue
		}
		out = append(out, seg)
	}
	return out, nil
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

func splitTokenPath(defID string) (name string, tail []string) {
	defID = strings.TrimSpace(defID)
	name, tailStr, _ := strings.Cut(defID, ".")
	name = strings.ToLower(name)
	if tailStr == "" {
		return name, nil
	}
	for _, seg := range strings.Split(tailStr, ".") {
		if seg != "" {
			tail = append(tail, seg)
		}
	}
	return name, tail
}

func extractValues(body []byte, path codec.Path) ([]string, error) {
	if len(path) == 0 {
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
		return []string{string(v)}, nil
	default:
		return nil, fmt.Errorf("value at path is %s, not a scalar", kind)
	}
}
