// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"strconv"
	"time"

	valkey_go "github.com/valkey-io/valkey-go"
)

var retainIfPresentScript = NewScript(`
if redis.call('EXISTS',KEYS[1])==1 then
 redis.call('EXPIRE',KEYS[1],ARGV[2])
 redis.call('ZADD',KEYS[2],ARGV[3],ARGV[1])
end
return 1`)

// RetainIfPresent refreshes Key's TTL and rescores Member in Index, only while Key still exists.
type RetainIfPresent struct {
	Key   string
	Index string
}

func (r RetainIfPresent) Run(ctx context.Context, client valkey_go.Client, member string, ttl time.Duration, score int64) error {
	return RunScripts(ctx, client, []ScriptCall{{
		Script: retainIfPresentScript,
		Key:    r.Key,
		Also:   []string{r.Index},
		Args:   []string{member, strconv.FormatInt(int64(ttl/time.Second), 10), strconv.FormatInt(score, 10)},
	}})[0].Error()
}

var retentionSweepScript = NewScript(`
local ids=redis.call('ZRANGEBYSCORE',KEYS[1],'-inf',ARGV[1],'LIMIT',0,ARGV[2])
for _,id in ipairs(ids) do
 local key=ARGV[3]..id
 if redis.call('EXISTS',key)==0 then
  redis.call('ZREM',KEYS[1],id)
  for i=2,#KEYS do redis.call('SREM',KEYS[i],id) end
 else
  local ttl=redis.call('TTL',key)
  if ttl<0 then redis.call('EXPIRE',key,ARGV[4]);ttl=tonumber(ARGV[4]) end
  redis.call('ZADD',KEYS[1],tonumber(ARGV[1])+ttl,id)
 end
end
return #ids`)

// RetentionSweep drops members from Index whose KeyPrefix+member key is gone, removing them from SetIdx too, and otherwise floors the key's TTL and rescores it.
type RetentionSweep struct {
	Index     string
	SetIdx    []string
	KeyPrefix string
}

func (s RetentionSweep) Run(ctx context.Context, client valkey_go.Client, now time.Time, batch int, floorTTL time.Duration) (int64, error) {
	keys := append([]string{s.Index}, s.SetIdx...)
	n, err := RunScripts(ctx, client, []ScriptCall{{
		Script: retentionSweepScript,
		Key:    keys[0],
		Also:   keys[1:],
		Args:   []string{strconv.FormatInt(now.Unix(), 10), strconv.Itoa(batch), s.KeyPrefix, strconv.FormatInt(int64(floorTTL/time.Second), 10)},
	}})[0].ToInt64()
	return n, err
}

var retentionMigrationScript = NewScript(`
local cursor=redis.call('GET',KEYS[3]) or '0'
local page=redis.call('SSCAN',KEYS[1],cursor,'COUNT',ARGV[1])
redis.call('SET',KEYS[3],page[1])
for i,id in ipairs(page[2]) do
 if i>tonumber(ARGV[1]) then break end
 local key=ARGV[2]..id
 if redis.call('EXISTS',key)==1 then
  local ttl=redis.call('TTL',key)
  if ttl<0 then redis.call('EXPIRE',key,ARGV[3]);ttl=tonumber(ARGV[3]) end
  redis.call('ZADD',KEYS[2],tonumber(ARGV[4])+ttl,id)
 end
 redis.call('SREM',KEYS[1],id)
end
return #page[2]`)

// RetentionMigration drains Source in bounded, resumable batches tracked by Cursor, carrying still-live members into Index.
type RetentionMigration struct {
	Source string
	Index  string
	Cursor string
}

func (m RetentionMigration) Run(ctx context.Context, client valkey_go.Client, batch int, keyPrefix string, floorTTL time.Duration, now time.Time) (int64, error) {
	n, err := RunScripts(ctx, client, []ScriptCall{{
		Script: retentionMigrationScript,
		Key:    m.Source,
		Also:   []string{m.Index, m.Cursor},
		Args:   []string{strconv.Itoa(batch), keyPrefix, strconv.FormatInt(int64(floorTTL/time.Second), 10), strconv.FormatInt(now.Unix(), 10)},
	}})[0].ToInt64()
	return n, err
}

var guardedEraseScript = NewScript(`
if redis.call('HGET',KEYS[1],ARGV[3]) ~= ARGV[2] then return 0 end
redis.call('DEL',KEYS[1])
redis.call('ZREM',KEYS[2],ARGV[1])
for i=3,#KEYS do redis.call('SREM',KEYS[i],ARGV[1]) end
return 1`)

// GuardedErase deletes Key and removes Member from SortedIdx and every SetIdx, but only while GuardField on Key still equals Expect.
type GuardedErase struct {
	Key       string
	SortedIdx string
	SetIdx    []string
}

func (g GuardedErase) Run(ctx context.Context, client valkey_go.Client, guardField, expect, member string) (bool, error) {
	keys := append([]string{g.Key, g.SortedIdx}, g.SetIdx...)
	n, err := RunScripts(ctx, client, []ScriptCall{{
		Script: guardedEraseScript,
		Key:    keys[0],
		Also:   keys[1:],
		Args:   []string{member, expect, guardField},
	}})[0].ToInt64()
	return n == 1, err
}
