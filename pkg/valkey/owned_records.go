// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"strconv"
	"time"

	valkey_go "github.com/valkey-io/valkey-go"
)

const ownedRecordState = `
local function state(id, owner)
  local current = redis.call('HGET', KEYS[2], id)
  if current == owner then
    return 1
  end
  if current or redis.call('EXISTS', KEYS[4]) == 1 then
    return -1
  end
  return 0
end
`

var saveOwnedRecordScript = valkey_go.NewLuaScript(ownedRecordState + `
local standing = state(ARGV[1], ARGV[2])
if standing == -1 or (standing == 1 and ARGV[4] == '1') then
  return -1
end
if standing == 0 and ARGV[5] ~= '' then
  redis.call('HINCRBY', KEYS[3], ARGV[5], 1)
end
redis.call('HSET', KEYS[1], ARGV[1], ARGV[3])
redis.call('HSET', KEYS[2], ARGV[1], ARGV[2])
for i = 1, 3 do
  redis.call('PEXPIRE', KEYS[i], ARGV[6])
end
if ARGV[5] ~= '' then
  redis.call('PEXPIRE', KEYS[5], ARGV[6])
  redis.call('PEXPIRE', KEYS[6], ARGV[6])
end
return 1
`)

var standingOwnedRecordScript = valkey_go.NewLuaScript(ownedRecordState + `
return state(ARGV[1], ARGV[2])
`)

var finishOwnedRecordScript = valkey_go.NewLuaScript(ownedRecordState + `
local standing = state(ARGV[1], ARGV[2])
if standing ~= 1 then
  return standing
end
redis.call('HDEL', KEYS[1], ARGV[1])
redis.call('HDEL', KEYS[2], ARGV[1])
if ARGV[3] ~= '' and redis.call('HINCRBY', KEYS[3], ARGV[3], -1) <= 0 then
  redis.call('HDEL', KEYS[3], ARGV[3])
end
return 1
`)

var claimOwnedRecordScript = valkey_go.NewLuaScript(`
local current = redis.call('HGET', KEYS[2], ARGV[1])
if current == ARGV[3] then
  return {2, ''}
end
if current ~= ARGV[2] then
  return {0, ''}
end
redis.call('HSET', KEYS[2], ARGV[1], ARGV[3])
redis.call('SET', KEYS[4], '1', 'PX', ARGV[4])
return {1, redis.call('HGET', KEYS[1], ARGV[1])}
`)

// OwnedRecord is one field of a records hash with an owner beside it. PinCounts tallies how many records hold each pin; Taken marks a record just handed to a new owner.
type OwnedRecord struct {
	Records   string
	Owners    string
	PinCounts string
	Taken     string
	ID        string
}

func (r OwnedRecord) keys() []string { return []string{r.Records, r.Owners, r.PinCounts, r.Taken} }

// OwnedSave writes a record. Pin, when set, counts the record against that pin on first write and keeps PinKeys alive with it.
type OwnedSave struct {
	Owner     string
	Body      string
	MustBeNew bool
	Pin       string
	PinKeys   []string
	TTL       time.Duration
}

// Save returns 1 when written and -1 when another owner holds the record, or the record exists and MustBeNew is set.
func (r OwnedRecord) Save(ctx context.Context, client valkey_go.Client, save OwnedSave) (int64, error) {
	fresh := ""
	if save.MustBeNew {
		fresh = "1"
	}
	args := []string{r.ID, save.Owner, save.Body, fresh, save.Pin, strconv.FormatInt(save.TTL.Milliseconds(), 10)}
	return saveOwnedRecordScript.Exec(ctx, client, append(r.keys(), save.PinKeys...), args).AsInt64()
}

// Standing returns 1 for the owner, -1 for another owner or a handover in progress, and 0 for a missing record.
func (r OwnedRecord) Standing(ctx context.Context, client valkey_go.Client, owner string) (int64, error) {
	return standingOwnedRecordScript.Exec(ctx, client, r.keys(), []string{r.ID, owner}).AsInt64()
}

// OwnedFinish removes a record its owner is done with and takes it off Pin.
type OwnedFinish struct {
	Owner string
	Pin   string
}

// Finish removes the record for its owner and otherwise returns the standing it found.
func (r OwnedRecord) Finish(ctx context.Context, client valkey_go.Client, finish OwnedFinish) (int64, error) {
	return finishOwnedRecordScript.Exec(ctx, client, r.keys(), []string{r.ID, finish.Owner, finish.Pin}).AsInt64()
}

// OwnedHandover moves a record from one owner to another; TakenTTL is how long the new owner's claim stays marked.
type OwnedHandover struct {
	From     string
	To       string
	TakenTTL time.Duration
}

// Claim outcomes of OwnedRecord.Claim.
const (
	OwnedClaimGone    int64 = 0
	OwnedClaimWon     int64 = 1
	OwnedClaimAlready int64 = 2
)

// OwnedClaimResult carries the record body apart from its read error, so a caller can settle ownership before judging the body.
type OwnedClaimResult struct {
	Status  int64
	Body    string
	BodyErr error
}

// Claim hands the record over when From still owns it.
func (r OwnedRecord) Claim(ctx context.Context, client valkey_go.Client, handover OwnedHandover) (OwnedClaimResult, error) {
	args := []string{r.ID, handover.From, handover.To, strconv.FormatInt(handover.TakenTTL.Milliseconds(), 10)}
	reply, err := claimOwnedRecordScript.Exec(ctx, client, r.keys(), args).ToArray()
	if err != nil {
		return OwnedClaimResult{}, err
	}
	status, err := reply[0].AsInt64()
	if err != nil {
		return OwnedClaimResult{}, err
	}
	body, bodyErr := reply[1].ToString()
	return OwnedClaimResult{Status: status, Body: body, BodyErr: bodyErr}, nil
}
