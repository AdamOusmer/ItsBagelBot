// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package codec

import (
	"errors"
	"fmt"

	"github.com/buger/jsonparser"
)

var ErrNotFound = errors.New("codec: key path not found")

type Path []string

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

func extractErr(err error, path Path) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, jsonparser.KeyPathNotFoundError) {
		return fmt.Errorf("%w: %v", ErrNotFound, path)
	}
	return fmt.Errorf("codec: extract %v: %w", path, err)
}

// Returned bytes alias data; copy anything that outlives it.
func ExtractValue(data []byte, path Path) ([]byte, Kind, error) {
	value, valueType, _, err := jsonparser.Get(data, path...)
	if err != nil {
		return nil, KindInvalid, extractErr(err, path)
	}
	return value, kindOf(valueType), nil
}

func ExtractString(data []byte, path Path) (string, error) {
	v, err := jsonparser.GetString(data, path...)
	return v, extractErr(err, path)
}

func ExtractInt(data []byte, path Path) (int64, error) {
	v, err := jsonparser.GetInt(data, path...)
	return v, extractErr(err, path)
}

func ExtractFloat(data []byte, path Path) (float64, error) {
	v, err := jsonparser.GetFloat(data, path...)
	return v, extractErr(err, path)
}

func ExtractBool(data []byte, path Path) (bool, error) {
	v, err := jsonparser.GetBoolean(data, path...)
	return v, extractErr(err, path)
}

func ExtractEach(data []byte, fn func(key, value []byte, kind Kind) error, path Path) error {
	err := jsonparser.ObjectEach(data, func(key, value []byte, valueType jsonparser.ValueType, _ int) error {
		return fn(key, value, kindOf(valueType))
	}, path...)
	if err == nil {
		return nil
	}
	if callbackErr := callerError(err); callbackErr != nil {
		return callbackErr
	}
	return extractErr(err, path)
}

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

func ParseInt(value []byte) (int64, error) {
	v, err := jsonparser.ParseInt(value)
	if err != nil {
		return 0, fmt.Errorf("codec: parse int: %w", err)
	}
	return v, nil
}

func ParseFloat(value []byte) (float64, error) {
	v, err := jsonparser.ParseFloat(value)
	if err != nil {
		return 0, fmt.Errorf("codec: parse float: %w", err)
	}
	return v, nil
}

func ParseBool(value []byte) (bool, error) {
	v, err := jsonparser.ParseBoolean(value)
	if err != nil {
		return false, fmt.Errorf("codec: parse bool: %w", err)
	}
	return v, nil
}

func ParseString(value []byte) (string, error) {
	v, err := jsonparser.ParseString(value)
	if err != nil {
		return "", fmt.Errorf("codec: parse string: %w", err)
	}
	return v, nil
}
