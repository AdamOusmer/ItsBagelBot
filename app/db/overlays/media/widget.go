// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package media

import (
	"bytes"
	"unicode/utf8"

	"ItsBagelBot/internal/domain/overlays"
	"ItsBagelBot/pkg/codec"
)

type bundle struct {
	HTML   string           `json:"html"`
	CSS    string           `json:"css"`
	JS     string           `json:"js"`
	Fields codec.RawMessage `json:"fields"`
}

func normalizeWidget(data blob, _ overlays.AssetLimit) (parsed, error) {
	if !utf8.Valid(data) {
		return parsed{}, fail(CodeCorrupt)
	}
	var b bundle
	if codec.UnmarshalStrict(bytes.TrimPrefix(data, utf8BOM), &b) != nil {
		return parsed{}, fail(CodeCorrupt)
	}
	if len(b.Fields) > 0 {
		var fields bytes.Buffer
		if codec.Compact(&fields, b.Fields) != nil {
			return parsed{}, fail(CodeCorrupt)
		}
		b.Fields = fields.Bytes()
	}
	out, err := codec.Marshal(b)
	if err != nil {
		return parsed{}, fail(CodeCorrupt)
	}
	return parsed{data: out}, nil
}
