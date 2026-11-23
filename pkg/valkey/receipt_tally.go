// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"strconv"
	"time"

	valkey_go "github.com/valkey-io/valkey-go"
)

var receiptTallyScript = NewScript(`
if not redis.call('SET', KEYS[1], '1', 'NX', 'EX', tonumber(ARGV[1])) then return 0 end
local hashes, ranked = tonumber(ARGV[2]), tonumber(ARGV[3])
local at, key = 4, 2
for _ = 1, hashes do
  local ttl, fields = tonumber(ARGV[at]), tonumber(ARGV[at + 1])
  at = at + 2
  for _ = 1, fields do
    redis.call('HINCRBY', KEYS[key], ARGV[at], ARGV[at + 1])
    at = at + 2
  end
  redis.call('EXPIRE', KEYS[key], ttl)
  key = key + 1
end
for _ = 1, ranked do
  redis.call('ZINCRBY', KEYS[key], ARGV[at + 1], ARGV[at])
  redis.call('EXPIRE', KEYS[key], tonumber(ARGV[at + 2]))
  at = at + 3
  key = key + 1
end
return 1`)

// FieldDelta is one HINCRBY step.
type FieldDelta struct {
	Field string
	Delta int64
}

// HashTally adds its deltas to one hash and renews the hash TTL.
type HashTally struct {
	Key    string
	TTL    time.Duration
	Fields []FieldDelta
}

// RankedDelta adds Delta to Member in one sorted set and renews its TTL.
type RankedDelta struct {
	Key    string
	TTL    time.Duration
	Member string
	Delta  int64
}

// ReceiptTally applies every hash and sorted-set delta at most once per receipt, atomically.
// Keys must share one hash slot (a common {tag}) on a cluster.
type ReceiptTally struct {
	Receipt Key
}

// Add reports whether the receipt was new and the deltas were applied.
func (t ReceiptTally) Add(ctx context.Context, client valkey_go.Client, hashes []HashTally, ranked []RankedDelta) (bool, error) {
	keys := []string{t.Receipt.Name}
	args := []string{
		strconv.FormatInt(int64(t.Receipt.TTL.Seconds()), 10),
		strconv.Itoa(len(hashes)),
		strconv.Itoa(len(ranked)),
	}
	for _, hash := range hashes {
		keys = append(keys, hash.Key)
		args = append(args, strconv.FormatInt(int64(hash.TTL.Seconds()), 10), strconv.Itoa(len(hash.Fields)))
		for _, field := range hash.Fields {
			args = append(args, field.Field, strconv.FormatInt(field.Delta, 10))
		}
	}
	for _, delta := range ranked {
		keys = append(keys, delta.Key)
		args = append(args, delta.Member, strconv.FormatInt(delta.Delta, 10), strconv.FormatInt(int64(delta.TTL.Seconds()), 10))
	}
	applied, err := receiptTallyScript.Exec(ctx, client, keys, args).ToInt64()
	return applied == 1, err
}
