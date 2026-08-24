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

	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/moderation"

	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

// ValkeyRecent is the centralized sweep memory behind !nuke. Sesame's replica
// pool shares one durable JetStream consumer (pkg/bus), so no pod sees more
// than an arbitrary fraction of a channel's chat — an in-process window would
// make every sweep silently incomplete, exactly during the raids it exists
// for. The ZSET at am:recent:<chan> gives every pod the same complete view;
// it joins the automod state family (am:*) whose members are all TTL-bound,
// tenant-scoped and never leave the cache tier.
//
// The hot path pays no I/O: Record appends into a per-process buffer that a
// ticker flushes as one pipelined DoMulti per touched channel (ZADD the new
// members, ZREMRANGEBYSCORE the expired ones, ZREMRANGEBYRANK enforce the
// cardinality cap, EXPIRE slide the window). Batching keeps the firehose at
// one round trip per ~50ms instead of per line — the campaign juror's six
// commands per line is documented in docs/automod as a cost to dig out of,
// not one to add back. A flush loses at most one interval of lines on a hard
// crash; for raid-cleanup memory that trade is free.
//
// Scores are unix MILLIS: exact in float64 (nanos are not), so expiry cutoffs
// never jitter. Members encode "uid:role:text" — both prefixes are numeric,
// so parsing cuts twice and the text may contain anything Twitch delivers.
const (
	recentKeyPrefix      = "am:recent:"
	recentFlushInterval  = 50 * time.Millisecond
	recentFlushMaxBuffer = 512 // pending entries before an early flush fires
	recentFetchLimit     = 256 // sweep read cap; server-side cardinality ≤ recentRingCap
)

// ValkeyRecent implements recentStore over valkey.
type ValkeyRecent struct {
	client valkey.Client
	log    *zap.Logger

	mu       sync.Mutex
	pending  map[uint64][]recentEntry
	buffered int

	// Write/read-error visibility, the campaign juror's lesson: a dead backend
	// must show up once per interval carrying how many operations were
	// swallowed, not vanish into per-line debug spam or silence.
	errPending     atomic.Int64
	lastWriteLogNs atomic.Int64
}

// NewValkeyRecent builds the store over the shared client. A nil client is
// the kill switch: records are dropped and sweeps come back empty, matching
// every other store's nil-degrades behavior.
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
	return recentKeyPrefix + strconv.FormatUint(uint64(chanID), 10)
}

// Start runs the flush loop until ctx is canceled, then best-effort flushes
// what remains. Wiring starts it on the process lifecycle context.
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

// Record buffers one chat envelope for the next flush. It parses and bounds
// here (the envelope is pooled by the caller) but copies nothing: retained
// strings alias the payload buffer, which the transport never reuses.
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

// flush drains the pending buffer as one pipelined DoMulti per touched
// channel. The eviction cutoff derives from the batch's newest entry rather
// than wall-clock time, so tests stay deterministic and a quiet channel's
// stale members still die on its next activity (the sliding EXPIRE covers
// the idle case).
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

// flushPlan carries the batch-wide eviction bounds every per-channel pipeline
// shares: the TTL cutoff in millis (derived from the freshest buffered line,
// keeping tests deterministic) and the sliding key expiry.
type flushPlan struct {
	cutoff string
	ttlSec int64
}

// newestBufferedAt finds the freshest entry in the batch; the eviction cutoff
// derives from it rather than wall-clock time, so tests stay deterministic and
// a quiet channel's stale members still die on its next activity (the sliding
// EXPIRE covers the idle case).
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

// flushChannel writes one channel's buffered lines as a single pipelined
// DoMulti: add the new members, evict expired ones, enforce the cardinality
// cap, and slide the key's TTL.
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

// Sweep reads the channel's fresh members in one round trip and matches them
// in-process with the same normalization and word-boundary rules as the
// in-memory store. Fail-open: any read error yields no hits, never a block.
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
	t := GetBuf()
	defer PutBuf(t)
	for _, m := range members {
		e, ok := parseRecentMember(m)
		if !ok {
			continue // the score carried the age; the member carries only identity
		}
		t = moderation.Normalize(t, e.text)
		if !containsPhrase(t, q) || seenUID(hits, channelID(e.uid)) {
			continue
		}
		hits = append(hits, RecentHit{UserID: channelID(e.uid), Role: e.role})
	}
	return hits
}

func encodeRecentMember(e recentEntry) string {
	return strconv.FormatUint(e.uid, 10) + ":" + strconv.Itoa(int(e.role)) + ":" + e.text
}

// parseRecentMember decodes "uid:role:text". Both prefixes are numeric, so
// the two cuts are unambiguous no matter what the text carries.
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

// noteErrors sweeps write replies so a failing backend surfaces once per
// interval with the swallowed count (see ValkeyCampaign for the origin).
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
