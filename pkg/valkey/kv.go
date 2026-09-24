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

// Not the Fast codec pair: GetJSON's result outlives its buffer.
func SetJSON[T any](ctx context.Context, kv KV, at Key, value T) error {
	raw, err := codec.Marshal(value)
	if err != nil {
		return err
	}
	return kv.Set(ctx, at, string(raw))
}
