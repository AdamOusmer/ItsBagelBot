// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package protobuf owns safe optimized protobuf serialization independently of
// any transport. Messages have one owner; decode copies wire-backed values.
package protobuf

import (
	"errors"
	"reflect"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/reflect/protoreflect"
)

const MaxMessageBytes = 4 << 20

var (
	ErrUnsupportedMessage = errors.New("protobuf: message must be a non-nil optimized protobuf pointer")
	ErrMessageTooLarge    = errors.New("protobuf: message exceeds size limit")
	ErrInvalidMessage     = errors.New("protobuf: invalid optimized message")
	ErrShortBuffer        = errors.New("protobuf: destination buffer is too short")
)

// Message is implemented by safe VT-generated code. UnmarshalVT must copy
// strings/bytes instead of aliasing its input. Receive destinations start empty
// or are VT-pool owned, including nested messages. Unsafe VT decoders do not
// satisfy this ownership contract.
type Message interface {
	SizeVT() int
	MarshalToSizedBufferVT([]byte) (int, error)
	UnmarshalVT([]byte) error
	ResetVT()
}

// HasUnknown reports retained fields outside a generated message's schema.
// It inspects only this message; bounded application validators own checks on
// their nested types. Custom optimized messages without metadata retain no
// inspectable unknown fields and return false. Validate non-nil values first.
func HasUnknown(value Message) bool {
	message, ok := value.(interface{ ProtoReflect() protoreflect.Message })
	return ok && len(message.ProtoReflect().GetUnknown()) != 0
}

func Check(value any) (Message, error) {
	message, ok := value.(Message)
	if !ok || message == nil {
		return nil, ErrUnsupportedMessage
	}
	pointer := reflect.ValueOf(value)
	if pointer.Kind() != reflect.Pointer || pointer.IsNil() {
		return nil, ErrUnsupportedMessage
	}
	return message, nil
}

func Size(value Message) (size int, err error) {
	defer func() {
		if recover() != nil {
			size = 0
			err = ErrInvalidMessage
		}
	}()
	message, err := Check(value)
	if err != nil {
		return 0, err
	}
	size = message.SizeVT()
	if size < 0 {
		return 0, ErrInvalidMessage
	}
	if size > MaxMessageBytes {
		return 0, ErrMessageTooLarge
	}
	return size, nil
}

// MarshalTo writes exactly the encoded message into dst[:n]. len(dst) must be
// sufficient; this function does not grow or retain dst. On failure n is zero,
// dst may contain partial output and must not be sent. No copying or allocation
// is required when callers reuse a sufficiently sized buffer.
func MarshalTo(dst []byte, value Message) (n int, err error) {
	defer func() {
		if recover() != nil {
			n = 0
			err = ErrInvalidMessage
		}
	}()
	size, err := Size(value)
	if err != nil {
		return 0, err
	}
	if size > len(dst) {
		return 0, ErrShortBuffer
	}
	n, err = value.MarshalToSizedBufferVT(dst[:size])
	if err != nil {
		return 0, err
	}
	if n != size {
		return 0, ErrInvalidMessage
	}
	return n, nil
}

// Reset clears a valid VT-owned message and safely returns nested objects to
// their pools. It is also available to transport error paths before decoding.
func Reset(value Message) (err error) {
	defer func() {
		if recover() != nil {
			err = ErrInvalidMessage
		}
	}()
	message, err := Check(value)
	if err != nil {
		return err
	}
	message.ResetVT()
	return nil
}

// Unmarshal replaces rather than merges the destination, clears partial data on
// failure and never retains src through the safe Message contract. String/bytes
// decoding may allocate; scalar-only decode can reuse storage without allocation.
func Unmarshal(src []byte, value Message) (err error) {
	message, err := Check(value)
	if err != nil {
		return err
	}
	defer func() {
		if recover() != nil {
			err = ErrInvalidMessage
		}
		if err != nil {
			_ = Reset(message)
		}
	}()
	if err = Reset(message); err != nil {
		return err
	}
	if len(src) > MaxMessageBytes {
		return ErrMessageTooLarge
	}
	return message.UnmarshalVT(src)
}

// Empty is wire compatible with google.protobuf.Empty and retains no input.
// Valid unknown fields are consumed; malformed wire data is rejected.
type Empty struct{}

func (*Empty) SizeVT() int                                { return 0 }
func (*Empty) MarshalToSizedBufferVT([]byte) (int, error) { return 0, nil }
func (*Empty) ResetVT()                                   {}
func (*Empty) UnmarshalVT(src []byte) error {
	for len(src) > 0 {
		_, _, n := protowire.ConsumeField(src)
		if n < 0 {
			return ErrInvalidMessage
		}
		src = src[n:]
	}
	return nil
}
