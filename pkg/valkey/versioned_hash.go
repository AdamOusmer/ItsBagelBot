// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	valkey_go "github.com/valkey-io/valkey-go"
)

const versionedHashSuffix = "#v"

// A field only moves to a strictly newer version.
var versionedHashSetScript = NewScript(`
local cur = tonumber(redis.call('HGET', KEYS[1], ARGV[1] .. '#v'))
if cur and cur >= tonumber(ARGV[3]) then return 0 end
redis.call('HSET', KEYS[1], ARGV[1], ARGV[2], ARGV[1] .. '#v', ARGV[3])
redis.call('EXPIRE', KEYS[1], tonumber(ARGV[4]))
return 1`)

// VersionedHash holds the latest value per field, each guarded by its own version. Field names must not contain '#'.
type VersionedHash struct {
	Key string
	TTL time.Duration
}

// VersionedValue is a field value and the version that wrote it.
type VersionedValue struct {
	Value   string
	Version int64
}

// Set writes value when its version is newer than the field's, and reports whether it did.
func (h VersionedHash) Set(ctx context.Context, client valkey_go.Client, field string, value VersionedValue) (bool, error) {
	applied, err := versionedHashSetScript.Exec(ctx, client, []string{h.Key}, []string{
		field, value.Value,
		strconv.FormatInt(value.Version, 10),
		strconv.FormatInt(int64(h.TTL.Seconds()), 10),
	}).ToInt64()
	return applied == 1, err
}

// ReadFields returns the named fields that exist, each with its version.
func (h VersionedHash) ReadFields(ctx context.Context, client valkey_go.Client, fields ...string) (map[string]VersionedValue, error) {
	if len(fields) == 0 {
		return map[string]VersionedValue{}, nil
	}
	names := make([]string, 0, 2*len(fields))
	for _, field := range fields {
		names = append(names, field, field+versionedHashSuffix)
	}
	cells, err := client.Do(ctx, client.B().Hmget().Key(h.Key).Field(names...).Build()).ToArray()
	if err != nil {
		return nil, err
	}
	out := make(map[string]VersionedValue, len(fields))
	for i, field := range fields {
		value, valueErr := cells[2*i].ToString()
		version, versionErr := cells[2*i+1].AsInt64()
		if valkey_go.IsValkeyNil(valueErr) || valkey_go.IsValkeyNil(versionErr) {
			continue
		}
		if err := errors.Join(valueErr, versionErr); err != nil {
			return nil, err
		}
		out[field] = VersionedValue{Value: value, Version: version}
	}
	return out, nil
}

// Read returns every field with its version.
func (h VersionedHash) Read(ctx context.Context, client valkey_go.Client) (map[string]VersionedValue, error) {
	raw, err := client.Do(ctx, client.B().Hgetall().Key(h.Key).Build()).AsStrMap()
	if err != nil {
		return nil, err
	}
	out := make(map[string]VersionedValue, len(raw)/2)
	for field, value := range raw {
		if strings.HasSuffix(field, versionedHashSuffix) {
			continue
		}
		version, err := strconv.ParseInt(raw[field+versionedHashSuffix], 10, 64)
		if err != nil {
			return nil, err
		}
		out[field] = VersionedValue{Value: value, Version: version}
	}
	return out, nil
}
