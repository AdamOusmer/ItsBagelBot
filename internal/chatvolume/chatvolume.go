// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package chatvolume is the Valkey-backed store behind the Overview
// dashboard's chat-volume chart: per-channel, per-minute chat message
// counts, plus which minutes had a command answered (the chart's tan
// ticks), for the trailing ringWidth minutes of the current stream.
//
// Decision record: this reintroduces the per-channel ring buffer
// automod.Baseline explicitly rejected (app/sesame/automod/baseline.go:100-104,
// "3 metrics x unbounded retention... two floats beat both" in favor of an
// EWMA). That rejection does not carry over here, for the same reason
// internal/activity's Sink didn't inherit it either: an EWMA compresses a
// metric to two floats because the caller only ever needs a mean and a
// spread back out. It cannot answer "what were the last 60 discrete
// per-minute counts" — there is no decompression step that recovers 60
// separate numbers from two running moments. The chart needs exactly those
// discrete points, so an average is not a substitute; what bounds this store
// instead is a hard cap (a fixed 60-slot ring, never more fields) plus a TTL
// backstop, not compression.
//
// Unlike internal/activity's feed/latency LISTs (which append one
// independent row per event, so a blind LPUSH+LTRIM is correct regardless of
// write order or which replica wrote it), a chat-volume bucket AGGREGATES:
// each message increments the current minute's count, and that count must
// reset — not add on top of — whatever the slot held one lap (60 minutes)
// ago, or whatever a sibling sesame replica last wrote there (chat for one
// broadcaster is not guaranteed sticky to a single replica). Deciding
// reset-vs-increment needs to see the slot's current state before writing
// it, and two replicas racing that decision independently would corrupt the
// count. A single atomic Lua script (see bumpScript) makes the
// read-decide-write one server-side operation, and therefore one round trip
// from the caller — the "no read-modify-write on the hot path" rule is kept
// through atomicity here rather than through a blind append.
//
// Storage layout: one HASH per channel, "chatvol:<broadcasterID>".
//
//	a          anchor epoch (unix-minutes) of this ring's first write.
//	"0".."59"  ring slots, keyed by epoch%ringWidth: "<delta>:<count>:<handled>",
//	           where delta=epoch-anchor disambiguates a slot's current
//	           occupant from whatever a previous lap around the ring (or a
//	           previous stream that has not yet TTL'd out) left sitting
//	           there. See readSlot/bumpScript.
//
// Budget: MEMORY USAGE on a fully-populated ring (all 60 slots plus anchor,
// two-digit counts) measured 816 bytes against a real Valkey — call it
// roughly 1KB/channel. Same order of magnitude as the ~0.5KB
// internal/activity's Emit doc budgets for its own per-channel structure,
// and nowhere near "unbounded": the slot count is fixed at ringWidth
// forever and never grows with stream length or traffic (a busier channel
// makes the per-slot counts a digit or two longer, not the field count).
// The TTL (chatVolTTL) is the backstop for a channel that goes fully idle; a
// stream.online event also clears the ring outright so the chart starts at
// zero for a new stream instead of trailing the previous one.
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

const (
	keyPrefix = "chatvol:"

	// ringWidth is the fixed slot count — the hard cap referenced above. It
	// never grows regardless of stream length or traffic.
	ringWidth = 60

	// chatVolTTL is refreshed on every write; it only expires a channel that
	// has gone fully quiet, not one mid-stream. stream.online is the primary,
	// immediate reset (see Observe); this is the backstop for a channel that
	// never gets one (a missed observer delivery, a restart).
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

// bumpScript increments the current minute's bucket, resetting it instead of
// adding on top of a stale occupant (see the package doc's decision record).
//
//	KEYS[1] = the channel's hash key
//	ARGV[1] = epoch (unix-minutes)
//	ARGV[2] = "1" when this message was a handled command, else "0"
//	ARGV[3] = TTL seconds to (re)apply
//	ARGV[4] = ring width
//
// Returns the bucket's resulting count.
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

// Store is the Observer + read side of the chat-volume ring. Construct once
// per process and register it as a pipeline observer (see
// app/sesame/main.go and chatvolume_observer.go); it borrows client rather
// than owning its lifecycle, matching every other Valkey-backed store in
// this repo.
type Store struct {
	client valkey.Client
	log    *zap.Logger
}

// New builds a Store over an existing Valkey client. log may be nil (write
// failures are then silently dropped, same fail-open posture as the rest of
// this store: chat-volume is a dashboard nicety, never worth nacking chat
// over).
func New(client valkey.Client, log *zap.Logger) *Store {
	return &Store{client: client, log: log}
}

// Event is the minimal slice of app/sesame/engine.ObservedEvent this package
// needs. It exists so chatvolume — an internal/* package — never imports
// app/sesame/engine: internal/* stays below app/*, the same layering
// internal/activity keeps by defining its own Row type instead of importing
// engine's ObservedEvent directly. app/sesame/chatvolume_observer.go adapts
// one into the other at the single registration call site (main.go).
type Event struct {
	BroadcasterID uint64
	Type          string
	At            time.Time
	Handled       bool
}

// Observe implements this package's narrow observer shape. A chat message
// bumps the current minute's bucket (and its handled tick); a stream going
// live clears the ring so the chart starts at zero for the new stream. Every
// other event type is a no-op — this store only cares about the two.
func (s *Store) Observe(ev Event) {
	switch ev.Type {
	case typeStreamOnline:
		s.reset(ev.BroadcasterID)
	case typeChatMessage:
		s.bump(ev.BroadcasterID, ev.At, ev.Handled)
	}
}

// reset clears one channel's ring. Fire-and-forget from the caller's
// perspective, bounded by writeTimeout; a failure is logged, not surfaced —
// worst case the chart briefly trails the previous stream until the next
// message ages those slots out on its own (see readSlot's delta check) or
// the TTL backstop reclaims the key.
func (s *Store) reset(broadcasterID uint64) {
	ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()
	c := pkgvalkey.Primary(s.client)
	if err := c.Do(ctx, c.B().Del().Key(chatVolKey(broadcasterID)).Build()).Error(); err != nil && s.log != nil {
		s.log.Warn("chatvolume: reset failed", zap.Uint64("broadcaster_id", broadcasterID), zap.Error(err))
	}
}

// bump runs bumpScript for one message. Fire-and-forget and bounded like
// reset; the one EVALSHA/EVAL call is the store's whole write-path cost per
// chat message.
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

// ChatVolume is one channel's ring, reconstructed oldest-first: Buckets[0] is
// ringWidth-1 minutes ago, Buckets[len-1] is the current (possibly
// in-progress) minute.
type ChatVolume struct {
	Buckets      []int
	CommandTicks []int // indices into Buckets where a command answered
	Now          int   // Buckets[len(Buckets)-1], broken out for convenience
	Peak         int
}

// Read serves the dashboard's chat-volume panel. console/dashboard's
// chat-volume.ts reads the same Valkey layout directly (that TypeScript
// reader cannot import this Go package), so a change to bumpScript's wire
// format must be mirrored there.
//
// Uses Primary, not the node-local replica plain Do would pick: the current
// minute's bucket is written on every chat message and read back on every
// dashboard load, so a replica read would routinely show the just-elapsed
// minute as short or zero for the length of the replication window. See
// pkg/valkey/routing.go's Primary doc.
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
	// A missing/malformed anchor parses to 0; every slot's delta check below
	// then legitimately misses too (an empty ring has no slot fields to
	// begin with), so this needs no separate empty-ring branch.
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

// readSlot returns target minute's count/handled tick, or (0, false) when
// the ring's slot for that minute either was never written or belongs to a
// different lap around the ring (its stored delta does not match target's).
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
