// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package watchtime owns primary-consistent admission and the replayable watch
// award outbox. Accepted work must survive downstream SQL failures. Deployment
// persistence and replication determine the outbox's infrastructure durability.
package watchtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/codec"
	pkgvalkey "ItsBagelBot/pkg/valkey"
	"github.com/valkey-io/valkey-go"
)

const Stream = "watchtime:outbox"

func AdmissionKey(id uint64) string { return "watchtime:admission:" + strconv.FormatUint(id, 10) }

type Snapshot struct {
	BroadcasterID    uint64
	Generation       string
	LiveSession      string
	AccountCreatedAt int64
	Config           codec.RawMessage
}
type Store struct{ client valkey.Client }

func NewStore(client valkey.Client) *Store { return &Store{client: pkgvalkey.Primary(client)} }

const admission = `
if redis.call('HGET',KEYS[2],'deleted') == '1' then return false end
if redis.call('HGET',KEYS[1],'active') ~= '1' or redis.call('HGET',KEYS[1],'banned') == '1' or redis.call('HGET',KEYS[1],'module:loyalty:enabled') ~= '1' then return false end
if redis.call('SISMEMBER',KEYS[4],ARGV[1]) == 1 then return false end
local session=redis.call('GET',KEYS[3]); if not session then return false end
local generation=redis.call('HGET',KEYS[2],'epoch') or '0'
local instance=redis.call('HGET',KEYS[2],'instance'); if not instance then return false end
`
const capture = admission + `if redis.call('HGET',KEYS[5],'active') == '1' then session=redis.call('HGET',KEYS[5],'session') or session end
return {generation,session,instance,redis.call('HGET',KEYS[1],'module:loyalty:config') or ''}`

func (s *Store) Capture(ctx context.Context, id uint64) (Snapshot, bool, error) {
	if id == 0 {
		return Snapshot{}, false, nil
	}
	sid := strconv.FormatUint(id, 10)
	vals, err := s.client.Do(ctx, s.client.B().Eval().Script(capture).Numkeys(5).Key("settings:"+sid, AdmissionKey(id), "live:"+sid, "trial:desired", "loyaltick:state:"+sid).Arg(sid).Build()).AsStrSlice()
	if valkey.IsValkeyNil(err) {
		return Snapshot{}, false, nil
	}
	if err != nil {
		return Snapshot{}, false, err
	}
	if len(vals) != 4 {
		return Snapshot{}, false, errors.New("invalid watch admission reply")
	}
	instance, err := strconv.ParseInt(vals[2], 10, 64)
	if err != nil {
		return Snapshot{}, false, err
	}
	return Snapshot{id, vals[0], vals[1], instance, codec.RawMessage(vals[3])}, true, nil
}
func (s *Store) Validate(ctx context.Context, snapshot Snapshot) (bool, error) {
	current, ok, err := s.Capture(ctx, snapshot.BroadcasterID)
	return ok && current.Generation == snapshot.Generation && current.LiveSession == snapshot.LiveSession && current.AccountCreatedAt == snapshot.AccountCreatedAt, err
}
func OperationID(a data.WatchAwardDTO) string {
	sum := sha256.Sum256([]byte(strconv.FormatUint(a.UserID, 10) + ":" + strconv.FormatInt(a.AccountCreatedAt, 10) + ":" + a.Generation + ":" + a.LiveSession + ":" + a.WindowID + ":" + strconv.FormatUint(uint64(a.Chunk), 10)))
	return hex.EncodeToString(sum[:])
}

const enqueue = admission + `
if generation ~= ARGV[2] or instance ~= ARGV[9] then return false end
local floor=tonumber(redis.call('GET',KEYS[9]) or '0')
if floor>0 and tonumber(ARGV[10])<=floor then return false end
if ARGV[7] == '' and session ~= ARGV[3] then return false end
if ARGV[7] ~= '' then
 if redis.call('GET',KEYS[7]) ~= ARGV[7] or redis.call('HGET',KEYS[8],'active') ~= '1' or redis.call('HGET',KEYS[8],'window') ~= ARGV[8] or redis.call('HGET',KEYS[8],'session') ~= ARGV[3] then return false end
end
local previous=redis.call('HGET',KEYS[6],ARGV[4]); if previous then
 if previous ~= ARGV[6] then return redis.error_reply('watch operation payload mismatch') end
 return 1
end
local delivery=redis.call('XADD',KEYS[5],'*','operation_id',ARGV[4],'payload',ARGV[5])
redis.call('ZADD',KEYS[10],ARGV[10],delivery)
redis.call('HSET',KEYS[6],ARGV[4],ARGV[6])
return 1`

func (s *Store) Enqueue(ctx context.Context, a data.WatchAwardDTO) (bool, error) {
	return s.EnqueueOwned(ctx, a, "")
}
func (s *Store) EnqueueOwned(ctx context.Context, a data.WatchAwardDTO, owner string) (bool, error) {
	if err := ValidateAward(a); err != nil {
		return false, err
	}
	body, err := codec.Marshal(a)
	if err != nil {
		return false, err
	}
	digest := sha256.Sum256(body)
	sid := strconv.FormatUint(a.UserID, 10)
	n, err := s.client.Do(ctx, s.client.B().Eval().Script(enqueue).Numkeys(10).Key("settings:"+sid, AdmissionKey(a.UserID), "live:"+sid, "trial:desired", Stream, "watchtime:operations:"+sid, "loyaltick:claim:"+sid, "loyaltick:state:"+sid, RetentionFloor, OutboxWindowIndex).Arg(sid, a.Generation, a.LiveSession, OperationID(a), string(body), hex.EncodeToString(digest[:]), owner, a.WindowID, strconv.FormatInt(a.AccountCreatedAt, 10), strconv.FormatInt(WindowStartUnixMilli(a), 10)).Build()).AsInt64()
	if valkey.IsValkeyNil(err) {
		return false, nil
	}
	return n == 1, err
}

// RestoreAccount accepts only the same still-active instance or a strictly
// newer creation timestamp. Delayed UserChanged cannot revive a deleted instance.
func (s *Store) RestoreAccount(ctx context.Context, id uint64, createdAt int64) (bool, error) {
	if id == 0 || createdAt <= 0 {
		return false, errors.New("invalid account instance")
	}
	n, err := s.client.Do(ctx, s.client.B().Eval().Script(`
 local prior=tonumber(redis.call('HGET',KEYS[1],'instance') or '0')
 local incoming=tonumber(ARGV[1])
 if incoming < prior then return 0 end
 if incoming == prior then
  if redis.call('HGET',KEYS[1],'deleted') == '1' then return 0 end
  return 1
 end
 redis.call('HINCRBY',KEYS[1],'epoch',1)
 redis.call('HSET',KEYS[1],'instance',ARGV[1]); redis.call('HDEL',KEYS[1],'deleted','state_revision','loyalty_revision')
 redis.call('DEL',KEYS[2],KEYS[3],KEYS[4])
 return 1`).Numkeys(4).Key(AdmissionKey(id), "settings:"+strconv.FormatUint(id, 10), "loyaltick:state:"+strconv.FormatUint(id, 10), "loyaltick:claim:"+strconv.FormatUint(id, 10)).Arg(strconv.FormatInt(createdAt, 10)).Build()).AsInt64()
	return n == 1, err
}

// DeleteAccount retires only its instance, atomically revoking outstanding
// work and removing settings/scheduler state. A delayed old deletion is ignored.
func (s *Store) DeleteAccount(ctx context.Context, id uint64, createdAt int64) (bool, error) {
	if id == 0 || createdAt <= 0 {
		return false, errors.New("invalid account instance")
	}
	sid := strconv.FormatUint(id, 10)
	n, err := s.client.Do(ctx, s.client.B().Eval().Script(`
 local prior=tonumber(redis.call('HGET',KEYS[1],'instance') or '0')
 if tonumber(ARGV[1]) < prior then return 0 end
 redis.call('HINCRBY',KEYS[1],'epoch',1)
 redis.call('HSET',KEYS[1],'instance',ARGV[1],'deleted','1')
 redis.call('DEL',KEYS[2],KEYS[3],KEYS[4],KEYS[5],KEYS[7]); redis.call('ZREM',KEYS[6],ARGV[2])
 return 1`).Numkeys(7).Key(AdmissionKey(id), "settings:"+sid, "loyaltick:state:"+sid, "loyaltick:claim:"+sid, "loyaltick:"+sid, "loyaltick:due", "live:"+sid).Arg(strconv.FormatInt(createdAt, 10), sid).Build()).AsInt64()
	return n == 1, err
}
