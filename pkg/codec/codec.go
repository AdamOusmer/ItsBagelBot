// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package codec

import (
	"encoding/json"
	"errors"
	"io"
	"reflect"

	"github.com/bytedance/sonic"
)

type (
	RawMessage  = json.RawMessage
	Number      = json.Number
	Marshaler   = json.Marshaler
	Unmarshaler = json.Unmarshaler
)

type (
	Encoder interface {
		Encode(v any) error
		SetEscapeHTML(on bool)
		SetIndent(prefix, indent string)
	}
	Decoder interface {
		Decode(v any) error
		Buffered() io.Reader
		DisallowUnknownFields()
		More() bool
		UseNumber()
	}
)

// Aliases the source buffer; convert to RawMessage before the value escapes.
type NoCopyRawMessage []byte

func (m NoCopyRawMessage) MarshalJSON() ([]byte, error) {
	if m == nil {
		return []byte("null"), nil
	}
	return m, nil
}

func (m *NoCopyRawMessage) UnmarshalJSON(data []byte) error {
	if m == nil {
		return errors.New("codec.NoCopyRawMessage: UnmarshalJSON on nil pointer")
	}
	*m = data
	return nil
}

var std = sonic.Config{
	CopyString:  true,
	EscapeHTML:  true,
	SortMapKeys: true,
}.Froze()

var fast = sonic.ConfigFastest

func Marshal(v any) ([]byte, error) { return std.Marshal(v) }

func MarshalIndent(v any, prefix, indent string) ([]byte, error) {
	return std.MarshalIndent(v, prefix, indent)
}

func MarshalToString(v any) (string, error) { return std.MarshalToString(v) }

func Unmarshal(data []byte, v any) error { return std.Unmarshal(data, v) }

func UnmarshalFromString(data string, v any) error { return std.UnmarshalFromString(data, v) }

func Valid(data []byte) bool { return std.Valid(data) }

// v aliases data; use only where data outlives every read of v.
func FastUnmarshal(data []byte, v any) error { return fast.Unmarshal(data, v) }

func FastMarshal(v any) ([]byte, error) { return fast.Marshal(v) }

func NewDecoder(r io.Reader) Decoder { return std.NewDecoder(r) }

func NewEncoder(w io.Writer) Encoder { return std.NewEncoder(w) }

func Pretouch(types ...reflect.Type) error { return sonic.PretouchMany(types) }
