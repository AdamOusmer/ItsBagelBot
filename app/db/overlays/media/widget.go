// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package media

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"

	"ItsBagelBot/internal/domain/overlays"
)

type bundle struct {
	HTML   string          `json:"html"`
	CSS    string          `json:"css"`
	JS     string          `json:"js"`
	Fields json.RawMessage `json:"fields"`
}

func normalizeWidget(data []byte, _ overlays.AssetLimit) (parsed, error) {
	if !utf8.Valid(data) {
		return parsed{}, fail(CodeCorrupt)
	}
	dec := json.NewDecoder(bytes.NewReader(bytes.TrimPrefix(data, utf8BOM)))
	dec.DisallowUnknownFields()
	var b bundle
	if err := dec.Decode(&b); err != nil {
		return parsed{}, fail(CodeCorrupt)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return parsed{}, fail(CodeCorrupt)
	}
	out, err := json.Marshal(b)
	if err != nil {
		return parsed{}, fail(CodeCorrupt)
	}
	return parsed{data: out}, nil
}
