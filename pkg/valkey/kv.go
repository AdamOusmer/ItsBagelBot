// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"time"

	"ItsBagelBot/pkg/codec"

	valkey_go "github.com/valkey-io/valkey-go"
)

// KV is the string-and-JSON half of Valkey that the fleet's small stores use:
// GET one key, SET it (usually with a TTL), DEL it. Every one of them had
// spelled out the same client.Do(ctx, client.B().Get().Key(k).Build()) chain
// and the same "err != nil || raw == """ miss rule by hand, and the two that
// stored JSON had drifted onto different codec entry points (see SetJSON).
//
// It is a value wrapping the client rather than a client of its own, so a
// caller keeps whatever routing view it already holds -- the node-local
// default, Primary, Throughput -- and KV only decides the command shape.
// Making it a client wrapper would have meant re-exposing Do/DoMulti/Receive
// and re-deciding routing here, where none of the callers want either.
type KV struct{ client valkey_go.Client }

// NewKV binds the helpers to client.
//
// A nil client is deliberately not handled: the stores that must survive an
// unreachable Valkey at boot already return a nil store from their own
// constructor and nil-check at the call site, so swallowing it here would
// turn their explicit "no Valkey" branch into a silently dead write.
func NewKV(client valkey_go.Client) KV { return KV{client: client} }

// Key names one key and how long it lives.
//
// The TTL travels with the name instead of being a fourth argument on Set
// because every write here is "this key, for this long"; a zero TTL then
// reads as the deliberate exception it is -- a flag that must outlive any
// process, such as the missing-permission marker learned from a 403 -- rather
// than an argument someone forgot.
type Key struct {
	Name string
	TTL  time.Duration
}

// GetString reads name. ok is false for BOTH a read error and a missing key:
// every caller collapses the two anyway (ask the source of truth again, or
// treat the cache as cold), so returning an error here would only add a
// branch each one immediately discards.
//
// A stored empty string also reads as a miss, because Valkey's GET cannot
// tell it from a missing key. A caller that must distinguish "confirmed
// nothing" from "unknown" stores a sentinel instead; see invitecache's
// noGuild.
func (kv KV) GetString(ctx context.Context, name string) (string, bool) {
	raw, err := kv.client.Do(ctx, kv.client.B().Get().Key(name).Build()).ToString()
	if err != nil || raw == "" {
		return "", false
	}
	return raw, true
}

// Set writes value at at.Name, expiring it after at.TTL when there is one.
func (kv KV) Set(ctx context.Context, at Key, value string) error {
	if at.TTL <= 0 {
		return kv.client.Do(ctx, kv.client.B().Set().Key(at.Name).Value(value).Build()).Error()
	}
	return kv.client.Do(ctx, kv.client.B().Set().Key(at.Name).Value(value).Ex(at.TTL).Build()).Error()
}

// Del removes name. A key that was not there is not an error, matching DEL.
func (kv KV) Del(ctx context.Context, name string) error {
	return kv.client.Do(ctx, kv.client.B().Del().Key(name).Build()).Error()
}

// GetJSON reads name and decodes it into T. ok is false for a miss, a read
// error, and a value that does not decode: the only writer of any of these
// keys is the fleet itself, so a blob that no longer parses came from a build
// that no longer exists, and reading it as "nothing cached" is what lets a
// rollout heal itself instead of failing every request behind it.
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

// SetJSON encodes value and writes it at at.
//
// These use codec.Marshal/Unmarshal, not the Fast pair, and that is the one
// codec decision for every Valkey JSON blob: sites had drifted apart, one
// writing FastMarshal and another Marshal for values of the same kind. The
// std pair is the correct half of that split here because the decoded value
// OUTLIVES the buffer it came from -- GetJSON returns it to a caller that
// caches it in its own struct -- which is exactly the case FastUnmarshal's
// doc excludes, and because the Valkey round trip dwarfs the copy either way.
func SetJSON[T any](ctx context.Context, kv KV, at Key, value T) error {
	raw, err := codec.Marshal(value)
	if err != nil {
		return err
	}
	return kv.Set(ctx, at, string(raw))
}
