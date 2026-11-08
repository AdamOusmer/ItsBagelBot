// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package grpc

import (
	"ItsBagelBot/pkg/protobuf"

	"google.golang.org/grpc/mem"
)

const maxCodecBytes = protobuf.MaxMessageBytes

// grpc/mem.NewBuffer does not return backing arrays at or below its small-buffer
// threshold to their pool. Use the default pool's 4 KiB bucket even for small
// messages so Free can return both the bytes and reference-counted buffer.
const minPooledBufferBytes = 4 << 10

var (
	ErrUnsupportedMessage = protobuf.ErrUnsupportedMessage
	ErrMessageTooLarge    = protobuf.ErrMessageTooLarge
	ErrInvalidMessage     = protobuf.ErrInvalidMessage
)

// Message and Empty retain source compatibility while serialization ownership
// lives in the transport-independent shared protobuf package.
type Message = protobuf.Message
type Empty = protobuf.Empty

type vtCodec struct{}

func (vtCodec) Name() string { return "proto" }

func messageOf(value any) (Message, error) { return protobuf.Check(value) }

func (vtCodec) Marshal(value any) (out mem.BufferSlice, err error) {
	defer func() {
		if recover() != nil {
			out = nil
			err = ErrInvalidMessage
		}
	}()
	message, err := messageOf(value)
	if err != nil {
		return nil, err
	}
	size, err := messageSize(value)
	if err != nil {
		return nil, err
	}
	if size == 0 {
		return nil, nil
	}
	return marshalBuffer(message, size)
}

// messageSize is also used by client request preflight; malformed optimized
// implementations must not panic before the codec gets a chance to validate.
func messageSize(value any) (int, error) {
	message, err := protobuf.Check(value)
	if err != nil {
		return 0, err
	}
	return protobuf.Size(message)
}

func marshalBuffer(message Message, size int) (out mem.BufferSlice, err error) {
	return marshalWithPool(message, size, mem.DefaultBufferPool())
}

func marshalWithPool(message Message, size int, pool mem.BufferPool) (out mem.BufferSlice, err error) {
	buffer := pooledBytes(pool, size)
	owned := true
	defer func() {
		if owned {
			pool.Put(buffer)
		}
	}()
	*buffer = (*buffer)[:size]
	n, err := protobuf.MarshalTo(*buffer, message)
	if err != nil {
		return nil, err
	}
	if n != size {
		return nil, ErrInvalidMessage
	}
	out = mem.BufferSlice{mem.NewBuffer(buffer, pool)}
	owned = false
	return out, nil
}

func pooledBytes(pool mem.BufferPool, size int) *[]byte {
	buffer := pool.Get(max(size, minPooledBufferBytes))
	*buffer = (*buffer)[:size]
	return buffer
}

func materializeWithPool(data mem.BufferSlice, pool mem.BufferPool) mem.Buffer {
	if len(data) == 1 {
		data[0].Ref()
		return data[0]
	}
	size := data.Len()
	if size == 0 {
		return mem.SliceBuffer(nil)
	}
	buffer := pooledBytes(pool, size)
	data.CopyTo(*buffer)
	return mem.NewBuffer(buffer, pool)
}

func (vtCodec) Unmarshal(data mem.BufferSlice, value any) (err error) {
	message, err := messageOf(value)
	if err != nil {
		return err
	}
	defer func() {
		if recover() != nil {
			err = ErrInvalidMessage
		}
		if err != nil {
			_ = resetMessage(message)
		}
	}()
	if err = checkBuffers(data); err != nil {
		return err
	}
	buffer := materializeWithPool(data, mem.DefaultBufferPool())
	defer buffer.Free()
	return protobuf.Unmarshal(buffer.ReadOnlyData(), message)
}

func resetMessage(message Message) error { return protobuf.Reset(message) }

func checkBuffers(data mem.BufferSlice) error {
	size := 0
	for _, buffer := range data {
		if buffer == nil {
			return ErrInvalidMessage
		}
		n := buffer.Len()
		if n < 0 || n > maxCodecBytes-size {
			return ErrMessageTooLarge
		}
		size += n
	}
	return nil
}
