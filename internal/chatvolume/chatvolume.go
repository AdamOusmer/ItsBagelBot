// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package chatvolume

import (
	"context"
	"strconv"
	"strings"
	"time"

	pkgvalkey "ItsBagelBot/pkg/valkey"

	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

// web/dashboard/src/lib/server/chat-volume.ts parses this hash layout; change both together.
const (
	keyPrefix = "chatvol:"

	ringWidth = 60

	chatVolTTL = 90 * time.Minute

	typeChatMessage  = "channel.chat.message"
	typeStreamOnline = "stream.online"

	writeTimeout = 200 * time.Millisecond
	readTimeout  = 200 * time.Millisecond
)

var (
	ttlArg   = strconv.Itoa(int(chatVolTTL.Seconds()))
	widthArg = strconv.Itoa(ringWidth)
)

var bumpScript = valkey.NewLuaScript(`
local key = KEYS[1]
local epoch = tonumber(ARGV[1])
local handled = ARGV[2]
local ttl = ARGV[3]
local width = tonumber(ARGV[4])

local anchor = tonumber(redis.call('HGET', key, 'a'))
if not anchor then
    anchor = epoch
    redis.call('HSET', key, 'a', anchor)
end

local delta = epoch - anchor
local slot = tostring(epoch % width)
local count = 1
local existing = redis.call('HGET', key, slot)
if existing then
    local sep1 = string.find(existing, ':')
    if sep1 and tonumber(string.sub(existing, 1, sep1 - 1)) == delta then
        local sep2 = string.find(existing, ':', sep1 + 1)
        count = tonumber(string.sub(existing, sep1 + 1, sep2 - 1)) + 1
        if handled ~= '1' then
            handled = string.sub(existing, sep2 + 1)
        end
    end
end

redis.call('HSET', key, slot, delta .. ':' .. count .. ':' .. handled)
redis.call('EXPIRE', key, ttl)
return count
`)

type Store struct {
	client valkey.Client
	log    *zap.Logger
}

func New(client valkey.Client, log *zap.Logger) *Store {
	return &Store{client: client, log: log}
}

type Event struct {
	BroadcasterID uint64
	Type          string
	At            time.Time
	Handled       bool
}

func (s *Store) Observe(ev Event) {
	switch ev.Type {
	case typeStreamOnline:
		s.reset(ev.BroadcasterID)
	case typeChatMessage:
		s.bump(ev.BroadcasterID, ev.At, ev.Handled)
	}
}

func (s *Store) reset(broadcasterID uint64) {
	ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()
	c := pkgvalkey.Primary(s.client)
	if err := c.Do(ctx, c.B().Del().Key(chatVolKey(broadcasterID)).Build()).Error(); err != nil && s.log != nil {
		s.log.Warn("chatvolume: reset failed", zap.Uint64("broadcaster_id", broadcasterID), zap.Error(err))
	}
}

func (s *Store) bump(broadcasterID uint64, at time.Time, handled bool) {
	ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()
	c := pkgvalkey.Primary(s.client)
	h := "0"
	if handled {
		h = "1"
	}
	epoch := strconv.FormatInt(epochMinute(at), 10)
	_, err := bumpScript.Exec(ctx, c, []string{chatVolKey(broadcasterID)}, []string{epoch, h, ttlArg, widthArg}).AsInt64()
	if err != nil && s.log != nil {
		s.log.Warn("chatvolume: bump failed", zap.Uint64("broadcaster_id", broadcasterID), zap.Error(err))
	}
}

type ChatVolume struct {
	Buckets      []int
	CommandTicks []int
	Now          int
	Peak         int
}

func (s *Store) Read(ctx context.Context, broadcasterID uint64, now time.Time) (ChatVolume, error) {
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	c := pkgvalkey.Primary(s.client)
	fields, err := c.Do(ctx, c.B().Hgetall().Key(chatVolKey(broadcasterID)).Build()).AsStrMap()
	if err != nil {
		return ChatVolume{}, err
	}
	return buildChatVolume(fields, epochMinute(now)), nil
}

func buildChatVolume(fields map[string]string, nowEpoch int64) ChatVolume {
	anchor, _ := strconv.ParseInt(fields["a"], 10, 64)

	buckets := make([]int, ringWidth)
	var ticks []int
	for i := 0; i < ringWidth; i++ {
		target := nowEpoch - int64(ringWidth-1) + int64(i)
		count, handled := readSlot(fields, target, anchor)
		buckets[i] = count
		if handled {
			ticks = append(ticks, i)
		}
	}
	return ChatVolume{Buckets: buckets, CommandTicks: ticks, Now: buckets[ringWidth-1], Peak: peakOf(buckets)}
}

func readSlot(fields map[string]string, target, anchor int64) (count int, handled bool) {
	raw, ok := fields[slotName(target)]
	if !ok {
		return 0, false
	}
	delta, c, h, ok := parseSlotValue(raw)
	if !ok || delta != target-anchor {
		return 0, false
	}
	return c, h
}

func parseSlotValue(raw string) (delta int64, count int, handled bool, ok bool) {
	parts := strings.SplitN(raw, ":", 3)
	if len(parts) != 3 {
		return 0, 0, false, false
	}
	d, err1 := strconv.ParseInt(parts[0], 10, 64)
	c, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return 0, 0, false, false
	}
	return d, c, parts[2] == "1", true
}

func peakOf(buckets []int) int {
	peak := 0
	for _, v := range buckets {
		if v > peak {
			peak = v
		}
	}
	return peak
}

func chatVolKey(broadcasterID uint64) string {
	return keyPrefix + strconv.FormatUint(broadcasterID, 10)
}

func slotName(epoch int64) string {
	return strconv.FormatInt(((epoch%ringWidth)+ringWidth)%ringWidth, 10)
}

func epochMinute(t time.Time) int64 {
	return t.Unix() / 60
}
