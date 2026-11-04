// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automation

import (
	"bytes"

	"ItsBagelBot/pkg/codec"
)

type defaulter interface{ setDefaults() }

type preparer interface{ prepare() }

func DecodeParams[T any](raw codec.RawMessage) (T, error) {
	var v T
	if d, ok := any(&v).(defaulter); ok {
		d.setDefaults()
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return v, nil
	}
	err := codec.UnmarshalStrict(trimmed, &v)
	return v, err
}
