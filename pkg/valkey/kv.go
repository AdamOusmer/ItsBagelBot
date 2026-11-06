// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"time"

	"ItsBagelBot/pkg/codec"

	valkey_go "github.com/valkey-io/valkey-go"
)

type KV struct{ client valkey_go.Client }

func NewKV(client valkey_go.Client) KV { return KV{client: client} }

type Key struct {
	Name string
	TTL  time.Duration
}

func (kv KV) GetString(ctx context.Context, name string) (string, bool) {
	raw, err := kv.client.Do(ctx, kv.client.B().Get().Key(name).Build()).ToString()
	if err != nil || raw == "" {
		return "", false
	}
	return raw, true
}

func (kv KV) Set(ctx context.Context, at Key, value string) error {
	if at.TTL <= 0 {
		return kv.client.Do(ctx, kv.client.B().Set().Key(at.Name).Value(value).Build()).Error()
	}
	return kv.client.Do(ctx, kv.client.B().Set().Key(at.Name).Value(value).Ex(at.TTL).Build()).Error()
}

func (kv KV) Del(ctx context.Context, name string) error {
	return kv.client.Do(ctx, kv.client.B().Del().Key(name).Build()).Error()
}

// SetIfAbsent keeps an existing value; losing to one is not an error.
func (kv KV) SetIfAbsent(ctx context.Context, at Key, value string) error {
	err := kv.client.Do(ctx, kv.client.B().Set().Key(at.Name).Value(value).Nx().Px(at.TTL).Build()).Error()
	if valkey_go.IsValkeyNil(err) {
		return nil
	}
	return err
}

// SetBytes stores value without copying it first; TTL must be positive.
func (kv KV) SetBytes(ctx context.Context, at Key, value []byte) error {
	return kv.client.Do(ctx, kv.client.B().Set().Key(at.Name).Value(valkey_go.BinaryString(value)).Ex(at.TTL).Build()).Error()
}

// ReadBytes tells a missing key from a failed read.
func (kv KV) ReadBytes(ctx context.Context, name string) ([]byte, bool, error) {
	return readOptional(ctx, kv, name, valkey_go.ValkeyResult.AsBytes)
}

// ReadInt tells a missing key from a failed read or a non-integer value.
func (kv KV) ReadInt(ctx context.Context, name string) (int64, bool, error) {
	return readOptional(ctx, kv, name, valkey_go.ValkeyResult.AsInt64)
}

func readOptional[T any](ctx context.Context, kv KV, name string, decode func(valkey_go.ValkeyResult) (T, error)) (T, bool, error) {
	var zero T
	value, err := decode(kv.client.Do(ctx, kv.client.B().Get().Key(name).Build()))
	if valkey_go.IsValkeyNil(err) {
		return zero, false, nil
	}
	if err != nil {
		return zero, false, err
	}
	return value, true, nil
}

func GetJSON[T any](ctx context.Context, kv KV, name string) (T, bool) {
	var out T
	raw, ok := kv.GetString(ctx, name)
	if !ok {
		return out, false
	}
	if err := codec.Unmarshal([]byte(raw), &out); err != nil {
		var zero T
		return zero, false
	}
	return out, true
}

// ReadJSON is GetJSON for callers that must tell a missing key from a read or decode failure.
func ReadJSON[T any](ctx context.Context, client valkey_go.Client, name string) (T, bool, error) {
	var zero T
	raw, err := client.Do(ctx, client.B().Get().Key(name).Build()).AsBytes()
	if valkey_go.IsValkeyNil(err) {
		return zero, false, nil
	}
	if err != nil {
		return zero, false, err
	}
	var out T
	if err := codec.Unmarshal(raw, &out); err != nil {
		return zero, false, err
	}
	return out, true, nil
}

// Not the Fast codec pair: GetJSON's result outlives its buffer.
func SetJSON[T any](ctx context.Context, kv KV, at Key, value T) error {
	raw, err := codec.Marshal(value)
	if err != nil {
		return err
	}
	return kv.Set(ctx, at, string(raw))
}
