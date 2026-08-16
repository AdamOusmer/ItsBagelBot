package codec

import (
	"errors"
	"fmt"

	"github.com/buger/jsonparser"
)

// The extract family reads fields out of a JSON document without decoding it
// into a Go value. It exists for the shape the struct decoder handles badly: a
// large or wide payload where only a few fields, or only the keys matching a
// pattern, are actually wanted. Walking the bytes once and materializing
// nothing else keeps the cost flat in the size of the document, where a full
// decode allocates for every field it builds and then throws most of them away.
//
// Use it when the document is big relative to what you need from it, or when
// the interesting keys are not known at compile time so no struct could name
// them. When the whole payload maps onto a struct, Unmarshal is both faster and
// clearer; extraction is not a general replacement for decoding.
//
// The bytes handed to a callback, and those returned by ExtractValue, are views
// into data. They stay valid only while data does, so copy anything that has to
// outlive the call.

// ErrNotFound reports that a path named no field in the document. It is
// distinct from a field that is present and null, which yields KindNull.
var ErrNotFound = errors.New("codec: key path not found")

// Path names a field by the object keys leading to it, outermost first: the
// "wins" inside "stats" is Path{"stats", "wins"}. An empty Path means the
// document's own top level.
//
// It is a declared type rather than a variadic string list so a caller can
// build it once and keep it. On a hot path that matters: a variadic argument is
// a fresh slice on every call, and for an extraction that otherwise allocates
// nothing it would be the only allocation left. Hoist it instead:
//
//	var statsPath = codec.Path{"stats"}
//
// The extract functions only read the Path they are given, so one value is safe
// to share across calls and goroutines.
type Path []string

// Kind is the JSON type of an extracted value.
type Kind uint8

const (
	KindInvalid Kind = iota
	KindString
	KindNumber
	KindObject
	KindArray
	KindBool
	KindNull
)

func (k Kind) String() string {
	switch k {
	case KindString:
		return "string"
	case KindNumber:
		return "number"
	case KindObject:
		return "object"
	case KindArray:
		return "array"
	case KindBool:
		return "bool"
	case KindNull:
		return "null"
	default:
		return "invalid"
	}
}

func kindOf(t jsonparser.ValueType) Kind {
	switch t {
	case jsonparser.String:
		return KindString
	case jsonparser.Number:
		return KindNumber
	case jsonparser.Object:
		return KindObject
	case jsonparser.Array:
		return KindArray
	case jsonparser.Boolean:
		return KindBool
	case jsonparser.Null:
		return KindNull
	default:
		return KindInvalid
	}
}

// extractErr normalizes a backend error, mapping a missing path onto ErrNotFound
// so callers can branch on it with errors.Is and never name the backend.
func extractErr(err error, path Path) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, jsonparser.KeyPathNotFoundError) {
		return fmt.Errorf("%w: %v", ErrNotFound, path)
	}
	return fmt.Errorf("codec: extract %v: %w", path, err)
}

// ExtractValue returns the raw bytes of the value at path, and its kind.
//
// The bytes are the value as it appears in data, with one exception worth
// knowing: a string is returned unquoted and still escaped, so it is not by
// itself valid JSON. Pass it to ParseString to get the decoded text. Object and
// array values are returned whole and are valid JSON.
func ExtractValue(data []byte, path Path) ([]byte, Kind, error) {
	value, valueType, _, err := jsonparser.Get(data, path...)
	if err != nil {
		return nil, KindInvalid, extractErr(err, path)
	}
	return value, kindOf(valueType), nil
}

// ExtractString returns the decoded string at path. It allocates only when the
// value contains escape sequences.
func ExtractString(data []byte, path Path) (string, error) {
	v, err := jsonparser.GetString(data, path...)
	return v, extractErr(err, path)
}

// ExtractInt returns the integer at path.
func ExtractInt(data []byte, path Path) (int64, error) {
	v, err := jsonparser.GetInt(data, path...)
	return v, extractErr(err, path)
}

// ExtractFloat returns the number at path as a float64.
func ExtractFloat(data []byte, path Path) (float64, error) {
	v, err := jsonparser.GetFloat(data, path...)
	return v, extractErr(err, path)
}

// ExtractBool returns the boolean at path.
func ExtractBool(data []byte, path Path) (bool, error) {
	v, err := jsonparser.GetBoolean(data, path...)
	return v, extractErr(err, path)
}

// ExtractEach calls fn once for every member of the object at path, in document
// order, passing the member's key and raw value. With no path it walks the
// top-level object.
//
// This is the wide-object case: aggregating counters whose names follow a
// pattern, or reading a map whose keys no struct can enumerate. Neither the key
// nor the value is copied, and nothing is materialized for the members fn
// ignores. The walk costs one allocation for the adapter callback and nothing
// per member, so scanning a thousand-key object costs the same as scanning a
// five-key one.
//
// Returning an error from fn stops the walk and returns that error unchanged,
// so a sentinel can be used to stop early.
func ExtractEach(data []byte, fn func(key, value []byte, kind Kind) error, path Path) error {
	err := jsonparser.ObjectEach(data, func(key, value []byte, valueType jsonparser.ValueType, _ int) error {
		return fn(key, value, kindOf(valueType))
	}, path...)
	if err == nil {
		return nil
	}
	// An error the callback raised is the caller's own; pass it through rather
	// than wrapping it as a parse failure.
	if callbackErr := callerError(err); callbackErr != nil {
		return callbackErr
	}
	return extractErr(err, path)
}

// ExtractArray calls fn once for every element of the array at path, in order,
// passing the element's raw value. Elements are views into data and are not
// copied.
func ExtractArray(data []byte, fn func(value []byte, kind Kind) error, path Path) error {
	_, err := jsonparser.ArrayEachErr(data, func(value []byte, valueType jsonparser.ValueType, _ int, elemErr error) error {
		if elemErr != nil {
			return elemErr
		}
		return fn(value, kindOf(valueType))
	}, path...)
	if err == nil {
		return nil
	}
	if callbackErr := callerError(err); callbackErr != nil {
		return callbackErr
	}
	return extractErr(err, path)
}

// callerError reports whether err came from a caller's callback rather than
// from the parser, so ExtractEach and ExtractArray can return it untouched and
// keep errors.Is working against the caller's own sentinels.
func callerError(err error) error {
	switch {
	case errors.Is(err, jsonparser.KeyPathNotFoundError),
		errors.Is(err, jsonparser.MalformedJsonError),
		errors.Is(err, jsonparser.MalformedObjectError),
		errors.Is(err, jsonparser.MalformedArrayError),
		errors.Is(err, jsonparser.MalformedStringError),
		errors.Is(err, jsonparser.MalformedValueError),
		errors.Is(err, jsonparser.MalformedStringEscapeError),
		errors.Is(err, jsonparser.UnknownValueTypeError),
		errors.Is(err, jsonparser.OverflowIntegerError):
		return nil
	default:
		return err
	}
}

// ParseInt decodes a raw number value handed to an extract callback.
func ParseInt(value []byte) (int64, error) {
	v, err := jsonparser.ParseInt(value)
	if err != nil {
		return 0, fmt.Errorf("codec: parse int: %w", err)
	}
	return v, nil
}

// ParseFloat decodes a raw number value handed to an extract callback.
func ParseFloat(value []byte) (float64, error) {
	v, err := jsonparser.ParseFloat(value)
	if err != nil {
		return 0, fmt.Errorf("codec: parse float: %w", err)
	}
	return v, nil
}

// ParseBool decodes a raw boolean value handed to an extract callback.
func ParseBool(value []byte) (bool, error) {
	v, err := jsonparser.ParseBoolean(value)
	if err != nil {
		return false, fmt.Errorf("codec: parse bool: %w", err)
	}
	return v, nil
}

// ParseString decodes a raw string value handed to an extract callback,
// resolving any escape sequences. It allocates only when escapes are present.
func ParseString(value []byte) (string, error) {
	v, err := jsonparser.ParseString(value)
	if err != nil {
		return "", fmt.Errorf("codec: parse string: %w", err)
	}
	return v, nil
}
