// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"strconv"
	"time"

	valkey_go "github.com/valkey-io/valkey-go"
)

var renewDueScript = valkey_go.NewLuaScript(`
local t = redis.call('TIME')
return redis.call('ZADD', KEYS[1], t[1] * 1000 + math.floor(t[2] / 1000) + tonumber(ARGV[2]), ARGV[1])
`)

var lapsedDueScript = valkey_go.NewLuaScript(`
local t = redis.call('TIME')
local now = t[1] * 1000 + math.floor(t[2] / 1000)
local lapsed = redis.call('ZRANGEBYSCORE', KEYS[1], '-inf', now, 'LIMIT', 0, ARGV[1])
local upcoming = redis.call('ZRANGEBYSCORE', KEYS[1], '(' .. now, '+inf', 'WITHSCORES', 'LIMIT', 0, 1)
local due = -1
if #upcoming > 0 then
  due = tonumber(upcoming[2]) - now
end
return {lapsed, due}
`)

// DueIndex orders members by the server-clock moment each comes due, so replicas with skewed clocks agree.
type DueIndex struct{ Key string }

// DueEntry is one member due after a delay from now.
type DueEntry struct {
	Member string
	After  time.Duration
}

func (i DueIndex) Renew(ctx context.Context, client valkey_go.Client, entry DueEntry) error {
	args := []string{entry.Member, strconv.FormatInt(entry.After.Milliseconds(), 10)}
	return renewDueScript.Exec(ctx, client, []string{i.Key}, args).Error()
}

// DueNotice names a member to make due at once and the channel that hears about it.
type DueNotice struct {
	Member  string
	Channel string
}

// LapseNow makes the member due immediately and publishes it. A failed publish is not reported; the next sweep finds the member anyway.
func (i DueIndex) LapseNow(ctx context.Context, client valkey_go.Client, notice DueNotice) error {
	results := client.DoMulti(ctx,
		client.B().Zadd().Key(i.Key).ScoreMember().ScoreMember(0, notice.Member).Build(),
		client.B().Publish().Channel(notice.Channel).Message(notice.Member).Build(),
	)
	return results[0].Error()
}

func (i DueIndex) Remove(ctx context.Context, client valkey_go.Client, member string) error {
	return client.Do(ctx, client.B().Zrem().Key(i.Key).Member(member).Build()).Error()
}

// Lapsed returns up to limit lapsed members and how long until the next one, negative when none is upcoming.
func (i DueIndex) Lapsed(ctx context.Context, client valkey_go.Client, limit int) ([]string, time.Duration, error) {
	reply, err := lapsedDueScript.Exec(ctx, client, []string{i.Key}, []string{strconv.Itoa(limit)}).ToArray()
	if err != nil {
		return nil, 0, err
	}
	lapsed, err := reply[0].AsStrSlice()
	if err != nil {
		return nil, 0, err
	}
	due, err := reply[1].AsInt64()
	return lapsed, time.Duration(due) * time.Millisecond, err
}

// OnPublish blocks until ctx ends, calling wake for every message on the channel.
func OnPublish(ctx context.Context, client valkey_go.Client, channel string, wake func()) error {
	return client.Receive(ctx, client.B().Subscribe().Channel(channel).Build(), func(valkey_go.PubSubMessage) { wake() })
}
