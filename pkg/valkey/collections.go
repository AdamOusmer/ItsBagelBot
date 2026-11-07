// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"slices"
	"time"

	valkey_go "github.com/valkey-io/valkey-go"
)

// ListPush prepends one element to a list.
type ListPush struct {
	Key     string
	Element string
}

// ListCap keeps a list to its newest Keep elements for TTL after each write.
type ListCap struct {
	Keep int64
	TTL  time.Duration
}

func PushCapped(ctx context.Context, client valkey_go.Client, pushes []ListPush, limit ListCap) error {
	if len(pushes) == 0 {
		return nil
	}
	cmds := make(valkey_go.Commands, 0, len(pushes)+8)
	var keys []string
	for _, push := range pushes {
		cmds = append(cmds, client.B().Lpush().Key(push.Key).Element(push.Element).Build())
		if !slices.Contains(keys, push.Key) {
			keys = append(keys, push.Key)
		}
	}
	for _, key := range keys {
		cmds = append(cmds,
			client.B().Ltrim().Key(key).Start(0).Stop(limit.Keep-1).Build(),
			client.B().Expire().Key(key).Seconds(int64(limit.TTL.Seconds())).Build(),
		)
	}
	return firstError(client.DoMulti(ctx, cmds...))
}

func firstError(results []valkey_go.ValkeyResult) error {
	for _, result := range results {
		if err := result.Error(); err != nil {
			return err
		}
	}
	return nil
}

func ListAll(ctx context.Context, client valkey_go.Client, key string) ([]string, error) {
	return client.Do(ctx, client.B().Lrange().Key(key).Start(0).Stop(-1).Build()).AsStrSlice()
}

// HashRead is one hash from ReadHashes; a failed read leaves Fields nil and sets Err.
type HashRead struct {
	Fields map[string]string
	Err    error
}

func ReadHashes(ctx context.Context, client valkey_go.Client, keys []string) []HashRead {
	if len(keys) == 0 {
		return nil
	}
	cmds := make(valkey_go.Commands, len(keys))
	for i, key := range keys {
		cmds[i] = client.B().Hgetall().Key(key).Build()
	}
	replies := client.DoMulti(ctx, cmds...)
	out := make([]HashRead, len(replies))
	for i, reply := range replies {
		out[i].Fields, out[i].Err = reply.AsStrMap()
	}
	return out
}

func HashAll(ctx context.Context, client valkey_go.Client, key string) (map[string]string, error) {
	return client.Do(ctx, client.B().Hgetall().Key(key).Build()).AsStrMap()
}

// HashField names one field of a hash.
type HashField struct {
	Key   string
	Field string
}

func (f HashField) Exists(ctx context.Context, client valkey_go.Client) (bool, error) {
	return client.Do(ctx, client.B().Hexists().Key(f.Key).Field(f.Field).Build()).AsBool()
}

func (f HashField) Delete(ctx context.Context, client valkey_go.Client) error {
	return client.Do(ctx, client.B().Hdel().Key(f.Key).Field(f.Field).Build()).Error()
}

// HashWrite sets a field and renews the hash's TTL. Without Replace it only fills an empty field.
type HashWrite struct {
	Value   string
	Replace bool
	TTL     time.Duration
}

func (f HashField) Write(ctx context.Context, client valkey_go.Client, write HashWrite) error {
	cmd := client.B().Hsetnx().Key(f.Key).Field(f.Field).Value(write.Value).Build()
	if write.Replace {
		cmd = client.B().Hset().Key(f.Key).FieldValue().FieldValue(f.Field, write.Value).Build()
	}
	return firstError(client.DoMulti(ctx, cmd, client.B().Expire().Key(f.Key).Seconds(int64(write.TTL.Seconds())).Build()))
}

// StagedCommit names the staged key and how long the hash lives after taking it.
type StagedCommit struct {
	Staged  string
	HashTTL time.Duration
}

// CommitStaged moves a staged string into an empty hash field; nothing staged is not an error.
func (f HashField) CommitStaged(ctx context.Context, client valkey_go.Client, commit StagedCommit) error {
	body, err := client.Do(ctx, client.B().Get().Key(commit.Staged).Build()).ToString()
	if valkey_go.IsValkeyNil(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return firstError(client.DoMulti(ctx,
		client.B().Hsetnx().Key(f.Key).Field(f.Field).Value(body).Build(),
		client.B().Expire().Key(f.Key).Seconds(int64(commit.HashTTL.Seconds())).Build(),
		client.B().Del().Key(commit.Staged).Build(),
	))
}

// ExpiringSet renews its TTL on every add.
type ExpiringSet struct {
	Key string
	TTL time.Duration
}

// Add reports only the add: a failed renewal leaves the TTL from the last add in place.
func (s ExpiringSet) Add(ctx context.Context, client valkey_go.Client, members []string) error {
	results := client.DoMulti(ctx,
		client.B().Sadd().Key(s.Key).Member(members...).Build(),
		client.B().Pexpire().Key(s.Key).Milliseconds(s.TTL.Milliseconds()).Build(),
	)
	return results[0].Error()
}

func (s ExpiringSet) Remove(ctx context.Context, client valkey_go.Client, members []string) error {
	return client.Do(ctx, client.B().Srem().Key(s.Key).Member(members...).Build()).Error()
}

func (s ExpiringSet) Sample(ctx context.Context, client valkey_go.Client, count int) ([]string, error) {
	return client.Do(ctx, client.B().Srandmember().Key(s.Key).Count(int64(count)).Build()).AsStrSlice()
}

// GuardedInt is an integer read beside a guard key; Value only means something when HasValue.
type GuardedInt struct {
	Exists   bool
	Value    int64
	HasValue bool
}

// GuardedIntKeys pairs the guard key with the integer key.
type GuardedIntKeys struct {
	Guard string
	Value string
}

func (k GuardedIntKeys) Read(ctx context.Context, client valkey_go.Client) (GuardedInt, error) {
	results := client.DoMulti(ctx,
		client.B().Exists().Key(k.Guard).Build(),
		client.B().Get().Key(k.Value).Build(),
	)
	count, err := results[0].AsInt64()
	if err != nil || count == 0 {
		return GuardedInt{}, err
	}
	value, err := results[1].AsInt64()
	if valkey_go.IsValkeyNil(err) {
		return GuardedInt{Exists: true}, nil
	}
	if err != nil {
		return GuardedInt{}, err
	}
	return GuardedInt{Exists: true, Value: value, HasValue: true}, nil
}

// CountExisting reports how many of the keys exist.
func CountExisting(ctx context.Context, client valkey_go.Client, keys []string) (int64, error) {
	return client.Do(ctx, client.B().Exists().Key(keys...).Build()).AsInt64()
}

// TimeToLive is zero for a missing key or one with no expiry.
func TimeToLive(ctx context.Context, client valkey_go.Client, key string) (time.Duration, error) {
	ms, err := client.Do(ctx, client.B().Pttl().Key(key).Build()).AsInt64()
	if err != nil {
		return 0, err
	}
	return max(time.Duration(ms)*time.Millisecond, 0), nil
}
