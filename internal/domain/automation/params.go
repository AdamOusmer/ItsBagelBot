// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automation

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

type defaulter interface{ setDefaults() }

type preparer interface{ prepare() }

func decodeStrict(raw []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("trailing data")
	}
	return nil
}

func isUnknownField(err error) bool {
	return err != nil && strings.HasPrefix(err.Error(), "json: unknown field")
}

func DecodeParams[T any](raw json.RawMessage) (T, error) {
	var v T
	if d, ok := any(&v).(defaulter); ok {
		d.setDefaults()
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return v, nil
	}
	err := decodeStrict(trimmed, &v)
	return v, err
}
