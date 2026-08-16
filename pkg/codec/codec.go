// Package codec is the fleet's single JSON implementation. Every service
// encodes and decodes through here instead of importing encoding/json or sonic
// directly, so the whole fleet shares one set of wire semantics rather than
// drifting between three encoders with three different escaping and key-order
// rules.
//
// The default functions are backed by Sonic, configured to reproduce
// encoding/json's observable behavior exactly, so migrating a call site is a
// rename and nothing more:
//
//   - CopyString: decoded strings own their memory instead of aliasing the
//     source buffer, so a decoded value may outlive the buffer it came from.
//   - EscapeHTML: <, > and & stay escaped, keeping payloads that carry chat
//     text inert wherever a consumer embeds them.
//   - SortMapKeys: map-typed values encode byte-identically across processes.
//
// FastMarshal and FastUnmarshal drop those guarantees for the per-message hot
// paths. Use them only where the caller provably owns the source buffer; see
// their docs.
//
// Nothing outside this file names the backend: the exported surface is
// functions, locally declared interfaces, and aliases of encoding/json's own
// wire types. Replacing Sonic means editing this file and nothing else.
//
// # Go version ceiling
//
// Sonic only compiles its fast path on a Go release it has validated, because
// that path is generated assembly reading runtime internals that move between
// releases. In v1.15.2 the ceiling is Go 1.27: sonic.go carries a !go1.27
// constraint, and compat.go, a thin wrapper over encoding/json, takes over
// from 1.27 onward.
//
// That swap is silent. Same API, same semantics, no build error and no test
// failure, just the standard library encoder back again and the entire reason
// this package exists gone with it. The fleet therefore stays on Go 1.26.5:
// pinned by the go directive, by image digest in every service Containerfile,
// and in CI. Raising it is a deliberate step gated on a Sonic release that
// lifts the ceiling, and TestSonicFastPathIsLive fails if the fallback ever
// engages.
package codec

import (
	"encoding/json"
	"errors"
	"io"
	"reflect"

	"github.com/bytedance/sonic"
)

// Wire types are aliases of the encoding/json originals rather than new types.
// Sonic honors the stdlib Marshaler/Unmarshaler interfaces, and an alias keeps
// these assignment-compatible with any code the fleet does not own, such as
// ent-generated models, so no conversion is needed at the boundary.
type (
	// RawMessage defers decoding of a nested object, exactly as
	// json.RawMessage does.
	RawMessage = json.RawMessage
	// Number holds a JSON number as its literal text.
	Number = json.Number
	// Marshaler is implemented by types with a custom encoding.
	Marshaler = json.Marshaler
	// Unmarshaler is implemented by types with a custom decoding.
	Unmarshaler = json.Unmarshaler
)

// Encoder and Decoder are declared here rather than aliased from the backend so
// the package's exported surface names no implementation type. Both the Sonic
// and the encoding/json stream types satisfy these method sets as written.
type (
	// Encoder writes JSON values to a stream.
	Encoder interface {
		Encode(v any) error
		SetEscapeHTML(on bool)
		SetIndent(prefix, indent string)
	}
	// Decoder reads JSON values from a stream.
	Decoder interface {
		Decode(v any) error
		Buffered() io.Reader
		DisallowUnknownFields()
		More() bool
		UseNumber()
	}
)

// NoCopyRawMessage is a RawMessage that keeps a view into the source buffer
// instead of copying it, for nested payloads a hot path passes through without
// inspecting. It is only sound while that buffer is alive and unmodified: pair
// it with FastUnmarshal on a path that owns its buffer, and convert to
// RawMessage before the value escapes that ownership.
//
// A backend that hands UnmarshalJSON a copy rather than a view degrades this to
// an ordinary RawMessage instead of breaking, which is why the type is declared
// here rather than aliased.
type NoCopyRawMessage []byte

// MarshalJSON returns m unchanged, since m already holds encoded JSON.
func (m NoCopyRawMessage) MarshalJSON() ([]byte, error) {
	if m == nil {
		return []byte("null"), nil
	}
	return m, nil
}

// UnmarshalJSON points m at data instead of copying it.
func (m *NoCopyRawMessage) UnmarshalJSON(data []byte) error {
	if m == nil {
		return errors.New("codec.NoCopyRawMessage: UnmarshalJSON on nil pointer")
	}
	*m = data
	return nil
}

// std is the fleet default: Sonic tuned to encoding/json's semantics.
var std = sonic.Config{
	CopyString:  true,
	EscapeHTML:  true,
	SortMapKeys: true,
}.Froze()

// fast trades the std guarantees for speed on the per-message hot paths. It is
// unexported so no call site holds a backend value; reach it through
// FastMarshal and FastUnmarshal.
var fast = sonic.ConfigFastest

// Marshal returns the JSON encoding of v.
func Marshal(v any) ([]byte, error) { return std.Marshal(v) }

// MarshalIndent returns the JSON encoding of v with the given prefix and indent.
func MarshalIndent(v any, prefix, indent string) ([]byte, error) {
	return std.MarshalIndent(v, prefix, indent)
}

// MarshalToString returns the JSON encoding of v as a string, skipping the
// []byte-to-string copy the caller would otherwise pay.
func MarshalToString(v any) (string, error) { return std.MarshalToString(v) }

// Unmarshal parses JSON data and stores the result in the value pointed to by v.
func Unmarshal(data []byte, v any) error { return std.Unmarshal(data, v) }

// UnmarshalFromString parses a JSON string and stores the result in the value
// pointed to by v.
func UnmarshalFromString(data string, v any) error { return std.UnmarshalFromString(data, v) }

// Valid reports whether data is well-formed JSON.
func Valid(data []byte) bool { return std.Valid(data) }

// FastUnmarshal decodes data into v without copying strings out of it: the
// decoded value holds views into data. Use it only on a path that owns data for
// at least as long as v is read, such as a subscription callback decoding the
// message buffer it was handed. Everywhere else use Unmarshal, which copies.
func FastUnmarshal(data []byte, v any) error { return fast.Unmarshal(data, v) }

// FastMarshal encodes v with the escaping and key-ordering guarantees of
// Marshal dropped. Use it for payloads the fleet produces and consumes itself,
// where no consumer depends on byte-for-byte output.
func FastMarshal(v any) ([]byte, error) { return fast.Marshal(v) }

// NewDecoder returns a streaming decoder reading from r.
func NewDecoder(r io.Reader) Decoder { return std.NewDecoder(r) }

// NewEncoder returns a streaming encoder writing to w.
func NewEncoder(w io.Writer) Encoder { return std.NewEncoder(w) }

// Pretouch compiles Sonic's codecs for the given types up front. Sonic builds a
// codec on a type's first use, so without this the first message of each shape
// pays the compile cost on a latency-sensitive path. Services call it at boot
// with their hot wire types.
func Pretouch(types ...reflect.Type) error { return sonic.PretouchMany(types) }
