// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"strconv"

	valkey_go "github.com/valkey-io/valkey-go"
)

// FieldValue is one hash field and the value to write it.
type FieldValue struct {
	Field string
	Value string
}

// Skips every effect once the guarded field already holds one of the protected tail values, so a stale caller can never override a fresher one.
var guardedTransitionScript = NewScript(`
local current=redis.call('HGET',KEYS[1],ARGV[1])
local n=tonumber(ARGV[2])
local tailStart=5+n*2
for i=tailStart,#ARGV do if current==ARGV[i] then return 0 end end
local hset={KEYS[1]}
for i=0,n-1 do hset[#hset+1]=ARGV[3+i*2]; hset[#hset+1]=ARGV[4+i*2] end
redis.call('HSET', unpack(hset))
redis.call('SADD',KEYS[2],ARGV[3+n*2])
if ARGV[4+n*2]=='1' then redis.call('SADD',KEYS[3],ARGV[3+n*2]) else redis.call('SREM',KEYS[3],ARGV[3+n*2]) end
return 1`)

// GuardedTransition writes Fields to Key unless GuardField already holds a protected value, lists Key's member in Index, and toggles the member's membership in Flagged.
type GuardedTransition struct {
	Key     string
	Index   string
	Flagged string
}

// Apply runs the guarded write as one script call.
func (t GuardedTransition) Apply(ctx context.Context, client valkey_go.Client, guardField string, fields []FieldValue, member string, flagged bool, protected ...string) (bool, error) {
	args := make([]string, 0, 2+len(fields)*2+2+len(protected))
	args = append(args, guardField, strconv.Itoa(len(fields)))
	for _, fv := range fields {
		args = append(args, fv.Field, fv.Value)
	}
	flag := ""
	if flagged {
		flag = "1"
	}
	args = append(args, member, flag)
	args = append(args, protected...)
	n, err := RunScripts(ctx, client, []ScriptCall{{
		Script: guardedTransitionScript,
		Key:    t.Key,
		Also:   []string{t.Index, t.Flagged},
		Args:   args,
	}})[0].ToInt64()
	return n == 1, err
}
