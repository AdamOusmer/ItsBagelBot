// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"strconv"
	"time"

	valkey_go "github.com/valkey-io/valkey-go"
)

var giftOpenScript = NewScript(`
local t = redis.call('TIME')
local now = t[1] * 1000 + math.floor(t[2] / 1000)
local id, total, window = ARGV[1], tonumber(ARGV[2]), tonumber(ARGV[5])
if redis.call('HSETNX', KEYS[1], 't:' .. id, ARGV[2]) == 0 then return 0 end
redis.call('HSET', KEYS[1], 'h:' .. id, ARGV[3], 's:' .. id, now)
local n = 0
local fields = redis.call('HGETALL', KEYS[1])
for i = 1, #fields, 2 do
  if string.sub(fields[i], 1, 2) == 'q:' then
    local value = fields[i + 1]
    local cut = string.find(value, '|', 1, true)
    if now - tonumber(string.sub(value, 1, cut - 1)) > window then
      redis.call('HDEL', KEYS[1], fields[i])
    elseif n < total then
      n = n + 1
      redis.call('HSET', KEYS[1], 'r:' .. id .. ':' .. n, string.sub(value, cut + 1))
      redis.call('HDEL', KEYS[1], fields[i])
    end
  end
end
redis.call('HSET', KEYS[1], 'n:' .. id, n)
redis.call('EXPIRE', KEYS[1], tonumber(ARGV[4]))
if n >= total then return 2 end
return 1`)

var giftAddScript = NewScript(`
local t = redis.call('TIME')
local now = t[1] * 1000 + math.floor(t[2] / 1000)
local best, bestAt
local fields = redis.call('HGETALL', KEYS[1])
for i = 1, #fields, 2 do
  if string.sub(fields[i], 1, 2) == 't:' then
    local id = string.sub(fields[i], 3)
    if tonumber(redis.call('HGET', KEYS[1], 'n:' .. id)) < tonumber(fields[i + 1]) then
      local at = tonumber(redis.call('HGET', KEYS[1], 's:' .. id))
      if not best or at < bestAt then best, bestAt = id, at end
    end
  end
end
redis.call('EXPIRE', KEYS[1], tonumber(ARGV[2]))
if not best then
  local seq = redis.call('HINCRBY', KEYS[1], 'qn', 1)
  redis.call('HSET', KEYS[1], 'q:' .. seq, now .. '|' .. ARGV[1])
  return {'', 0}
end
local n = tonumber(redis.call('HGET', KEYS[1], 'n:' .. best)) + 1
redis.call('HSET', KEYS[1], 'r:' .. best .. ':' .. n, ARGV[1], 'n:' .. best, n)
if n >= tonumber(redis.call('HGET', KEYS[1], 't:' .. best)) then return {best, 1} end
return {best, 0}`)

var giftTakeScript = NewScript(`
local header = redis.call('HGET', KEYS[1], 'h:' .. ARGV[1])
if not header then return false end
local n = tonumber(redis.call('HGET', KEYS[1], 'n:' .. ARGV[1]))
local out = {header}
local gone = {'t:' .. ARGV[1], 'h:' .. ARGV[1], 's:' .. ARGV[1], 'n:' .. ARGV[1]}
for k = 1, n do
  local field = 'r:' .. ARGV[1] .. ':' .. k
  out[#out + 1] = redis.call('HGET', KEYS[1], field)
  gone[#gone + 1] = field
end
redis.call('HDEL', KEYS[1], unpack(gone))
return out`)

// GiftBundle groups the recipients of one community gift under a single header.
// Recipients that arrive before their header are parked for Window and adopted when a header opens.
// All state of one key lives in one hash, so a key is one slot on a cluster.
type GiftBundle struct {
	TTL    time.Duration
	Window time.Duration
}

// GiftHeader is the gift announcement: Total recipients are expected.
type GiftHeader struct {
	ID      string
	Total   int
	Payload []byte
}

type GiftOpened int

const (
	GiftAlreadyOpen GiftOpened = iota
	GiftWaiting
	GiftComplete
)

// GiftAdded names the bundle that took the recipient, empty when it was parked.
type GiftAdded struct {
	BundleID string
	Complete bool
}

type GiftTaken struct {
	Header     []byte
	Recipients [][]byte
}

func (b GiftBundle) Open(ctx context.Context, client valkey_go.Client, key string, header GiftHeader) (GiftOpened, error) {
	args := []string{
		header.ID, strconv.Itoa(header.Total), string(header.Payload),
		strconv.FormatInt(int64(b.TTL.Seconds()), 10), strconv.FormatInt(b.Window.Milliseconds(), 10),
	}
	opened, err := giftOpenScript.Exec(ctx, client, []string{key}, args).ToInt64()
	return GiftOpened(opened), err
}

func (b GiftBundle) Add(ctx context.Context, client valkey_go.Client, key string, recipient []byte) (GiftAdded, error) {
	args := []string{string(recipient), strconv.FormatInt(int64(b.TTL.Seconds()), 10)}
	reply, err := giftAddScript.Exec(ctx, client, []string{key}, args).ToArray()
	if err != nil {
		return GiftAdded{}, err
	}
	id, err := reply[0].ToString()
	if err != nil {
		return GiftAdded{}, err
	}
	complete, err := reply[1].AsInt64()
	return GiftAdded{BundleID: id, Complete: complete == 1}, err
}

// Take removes the bundle and reports false when another caller already did.
func (b GiftBundle) Take(ctx context.Context, client valkey_go.Client, key, id string) (GiftTaken, bool, error) {
	reply, err := giftTakeScript.Exec(ctx, client, []string{key}, []string{id}).ToArray()
	if valkey_go.IsValkeyNil(err) {
		return GiftTaken{}, false, nil
	}
	if err != nil {
		return GiftTaken{}, false, err
	}
	taken := GiftTaken{}
	for i, item := range reply {
		text, err := item.ToString()
		if err != nil {
			return GiftTaken{}, false, err
		}
		if i == 0 {
			taken.Header = []byte(text)
			continue
		}
		taken.Recipients = append(taken.Recipients, []byte(text))
	}
	return taken, true, nil
}
