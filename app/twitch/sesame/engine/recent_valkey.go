// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/moderation"
	"ItsBagelBot/pkg/cache"

	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

const (
	recentKeyPrefix      = "am:recent:"
	recentFlushInterval  = 50 * time.Millisecond
	recentFlushMaxBuffer = 512
	recentFetchLimit     = 256
)

type ValkeyRecent struct {
	client valkey.Client
	log    *zap.Logger

	mu       sync.Mutex
	pending  map[uint64][]recentEntry
	buffered int

	errPending     atomic.Int64
	lastWriteLogNs atomic.Int64
}

func NewValkeyRecent(client valkey.Client, log *zap.Logger) *ValkeyRecent {
	if log == nil {
		log = zap.NewNop()
	}
	return &ValkeyRecent{
		client:  client,
		log:     log,
		pending: make(map[uint64][]recentEntry),
	}
}

func recentChannelKey(chanID channelID) string {
	return cache.UserKey(recentKeyPrefix, uint64(chanID))
}

func (v *ValkeyRecent) Start(ctx context.Context) {
	ticker := time.NewTicker(recentFlushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			v.flush(context.WithoutCancel(ctx))
			return
		case <-ticker.C:
			v.flush(ctx)
		}
	}
}

func (v *ValkeyRecent) Record(chanID channelID, env *lane.Envelope, now time.Time) {
	if v.client == nil {
		return
	}
	entries := chatEntriesFromEnvelope(env, now)
	if entries == nil {
		return
	}
	var early bool
	v.mu.Lock()
	v.pending[uint64(chanID)] = append(v.pending[uint64(chanID)], entries...)
	v.buffered += len(entries)
	if v.buffered >= recentFlushMaxBuffer {
		early = true
	}
	v.mu.Unlock()
	if early {
		go v.flush(context.Background())
	}
}

func (v *ValkeyRecent) flush(ctx context.Context) {
	v.mu.Lock()
	if len(v.pending) == 0 {
		v.mu.Unlock()
		return
	}
	batch := v.pending
	v.pending = make(map[uint64][]recentEntry)
	v.buffered = 0
	v.mu.Unlock()

	plan := flushPlan{
		cutoff: strconv.FormatInt(int64((newestBufferedAt(batch)-stamp(recentTTL))/stamp(time.Millisecond)), 10),
		ttlSec: int64(recentTTL / time.Second),
	}

	for id, entries := range batch {
		v.flushChannel(ctx, channelID(id), entries, plan)
	}
}

type flushPlan struct {
	cutoff string
	ttlSec int64
}

func newestBufferedAt(batch map[uint64][]recentEntry) stamp {
	newest := stamp(0)
	for _, entries := range batch {
		for i := range entries {
			if entries[i].at > newest {
				newest = entries[i].at
			}
		}
	}
	return newest
}

func (v *ValkeyRecent) flushChannel(ctx context.Context, chanID channelID, entries []recentEntry, plan flushPlan) {
	key := recentChannelKey(chanID)
	zadd := v.client.B().Zadd().Key(key).ScoreMember()
	for i := range entries {
		zadd = zadd.ScoreMember(float64(entries[i].at/stamp(time.Millisecond)), encodeRecentMember(entries[i]))
	}
	resps := v.client.DoMulti(ctx,
		zadd.Build(),
		v.client.B().Zremrangebyscore().Key(key).Min("-inf").Max(plan.cutoff).Build(),
		v.client.B().Zremrangebyrank().Key(key).Start(0).Stop(-(recentRingCap + 1)).Build(),
		v.client.B().Expire().Key(key).Seconds(plan.ttlSec).Build(),
	)
	v.noteErrors(resps)
}

func (v *ValkeyRecent) Sweep(ctx context.Context, chanID channelID, phrase string, now time.Time) []RecentHit {
	q := moderation.Normalize(GetBuf(), phrase)
	defer PutBuf(q)
	if v.client == nil || utf8.RuneCount(q) == 0 {
		return nil
	}
	cutoff := strconv.FormatInt(now.Add(-recentTTL).UnixMilli(), 10)

	resp := v.client.Do(ctx, v.client.B().Zrangebyscore().
		Key(recentChannelKey(chanID)).
		Min(cutoff).Max("+inf").
		Limit(0, recentFetchLimit).
		Build())
	members, err := resp.AsStrSlice()
	if err != nil {
		v.noteReadError(err)
		return nil
	}

	hits := make([]RecentHit, 0, len(members))
	seen := newUIDSet(len(members))
	t := GetBuf()
	defer PutBuf(t)
	for _, m := range members {
		e, ok := parseRecentMember(m)
		if !ok {
			continue
		}
		t = moderation.Normalize(t, e.text)
		if !containsPhrase(t, q) || !seen.add(channelID(e.uid)) {
			continue
		}
		hits = append(hits, RecentHit{UserID: channelID(e.uid), Role: e.role})
	}
	return hits
}

func encodeRecentMember(e recentEntry) string {
	return strconv.FormatUint(e.uid, 10) + ":" + strconv.Itoa(int(e.role)) + ":" + e.text
}

func parseRecentMember(m string) (recentEntry, bool) {
	uidStr, rest, ok := strings.Cut(m, ":")
	if !ok {
		return recentEntry{}, false
	}
	roleStr, text, ok := strings.Cut(rest, ":")
	if !ok {
		return recentEntry{}, false
	}
	uid, err := strconv.ParseUint(uidStr, 10, 64)
	if err != nil || uid == 0 {
		return recentEntry{}, false
	}
	role, err := strconv.Atoi(roleStr)
	if err != nil || role < 0 {
		return recentEntry{}, false
	}
	return recentEntry{uid: uid, role: module.Role(role), text: text}, true
}

func (v *ValkeyRecent) noteErrors(resps []valkey.ValkeyResult) {
	var failed int64
	var first error
	for _, r := range resps {
		if err := r.Error(); err != nil {
			failed++
			if first == nil {
				first = err
			}
		}
	}
	v.noteFailure(failed, first)
}

func (v *ValkeyRecent) noteReadError(err error) { v.noteFailure(1, err) }

func (v *ValkeyRecent) noteFailure(failed int64, first error) {
	if failed == 0 {
		return
	}
	now := time.Now().UnixNano()
	last := v.lastWriteLogNs.Load()
	if last == 0 || now-last > int64(campaignErrLogInterval) {
		if v.lastWriteLogNs.CompareAndSwap(last, now) {
			v.log.Warn("nuke recent-chat store errors",
				zap.Int64("suppressed", v.errPending.Load()+failed),
				zap.Error(first))
			v.errPending.Store(0)
			return
		}
	}
	v.errPending.Add(failed)
}
