package channels

import (
	"context"
	"github.com/valkey-io/valkey-go"
	"strconv"
	"time"
)

const hotIndexKey = "outgress:channels:hot"
const hotRetention = 7 * 24 * time.Hour
const hotBatch = 500
const hotRenewWindow = 24 * time.Hour

var retainHot = valkey.NewLuaScript(`
if redis.call('EXISTS',KEYS[1])==1 then
 redis.call('EXPIRE',KEYS[1],ARGV[2])
 redis.call('ZADD',KEYS[2],ARGV[3],ARGV[1])
end
return 1`)
var pruneHot = valkey.NewLuaScript(`
local ids=redis.call('ZRANGEBYSCORE',KEYS[1],'-inf',ARGV[1],'LIMIT',0,ARGV[2])
for _,id in ipairs(ids) do
 local key=ARGV[3]..id
 if redis.call('EXISTS',key)==0 then
  redis.call('ZREM',KEYS[1],id)
  redis.call('SREM',KEYS[2],id)
 else
  local ttl=redis.call('TTL',key)
  if ttl<0 then redis.call('EXPIRE',key,ARGV[4]);ttl=tonumber(ARGV[4]) end
  redis.call('ZADD',KEYS[1],tonumber(ARGV[1])+ttl,id)
 end
end
return #ids`)

func (r *Registry) retainHot(ctx context.Context, id string) error {
	return retainHot.Exec(ctx, r.client, []string{keyPrefix + id, hotIndexKey}, []string{id, strconv.FormatInt(int64(hotRetention/time.Second), 10), strconv.FormatInt(time.Now().Add(hotRetention).Unix(), 10)}).Error()
}
func (r *Registry) RenewHot(ctx context.Context, keep func(context.Context, string) bool) error {
	due, err := r.renewalCandidates(ctx)
	if err != nil {
		return err
	}
	for _, id := range due {
		if err := r.renewIfKept(ctx, id, keep); err != nil {
			return err
		}
	}
	return nil
}

func (r *Registry) renewalCandidates(ctx context.Context) ([]string, error) {
	before := strconv.FormatInt(time.Now().Add(hotRenewWindow).Unix(), 10)
	var due []string
	for offset := int64(0); ; offset += hotBatch {
		ids, err := r.client.Do(ctx, r.client.B().Zrangebyscore().Key(hotIndexKey).Min("-inf").Max(before).Limit(offset, hotBatch).Build()).AsStrSlice()
		if err != nil {
			return nil, err
		}
		due = append(due, ids...)
		if len(ids) < hotBatch {
			return due, nil
		}
	}
}

func (r *Registry) renewIfKept(ctx context.Context, id string, keep func(context.Context, string) bool) error {
	if !keep(ctx, id) {
		return nil
	}
	return r.retainHot(ctx, id)
}

func (r *Registry) PruneHot(ctx context.Context) error {
	if err := migrateHot.Exec(ctx, r.client, []string{indexKey, hotIndexKey, "outgress:channels:migration-cursor"}, []string{strconv.Itoa(hotBatch), keyPrefix, strconv.FormatInt(int64(hotRetention/time.Second), 10), strconv.FormatInt(time.Now().Unix(), 10)}).Error(); err != nil {
		return err
	}
	return pruneHot.Exec(ctx, r.client, []string{hotIndexKey, indexKey}, []string{strconv.FormatInt(time.Now().Unix(), 10), strconv.Itoa(hotBatch), keyPrefix, strconv.FormatInt(int64(hotRetention/time.Second), 10)}).Error()
}

var eraseIncarnation = valkey.NewLuaScript(`
if redis.call('HGET',KEYS[1],'account_created_at') ~= ARGV[2] then return 0 end
redis.call('DEL',KEYS[1]);redis.call('ZREM',KEYS[2],ARGV[1]);redis.call('SREM',KEYS[3],ARGV[1]);return 1`)

func (r *Registry) EraseIncarnation(ctx context.Context, id string, incarnation int64) error {
	if err := eraseIncarnation.Exec(ctx, r.client, []string{keyPrefix + id, hotIndexKey, indexKey}, []string{id, strconv.FormatInt(incarnation, 10)}).Error(); err != nil {
		return err
	}
	r.cache.Invalidate(id)
	return nil
}
func (r *Registry) SetIncarnation(ctx context.Context, id string, incarnation int64) error {
	return r.client.Do(ctx, r.client.B().Hset().Key(keyPrefix+id).FieldValue().FieldValue("account_created_at", strconv.FormatInt(incarnation, 10)).Build()).Error()
}

// Migrate the historical set in bounded, resumable batches. Migrated membership
// is removed, so an idle historical population is drained instead of rescanned.
var migrateHot = valkey.NewLuaScript(`
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
