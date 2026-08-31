// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package activity

import (
	"context"
	"sort"
	"strconv"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"ItsBagelBot/pkg/codec"
	pkgvalkey "ItsBagelBot/pkg/valkey"

	"github.com/valkey-io/valkey-go"
)

// Store is the Valkey-backed Sink promised by activity.go's decision record.
// Three keys per channel, all stream-scoped (TTL refreshed on every write, so
// a dead channel's data ages out rather than accumulating forever):
//
//	activity:feed:<channelID>     LIST, LPUSHed newest-first, LTRIMed to feedCap
//	activity:latency:<channelID>  LIST, command DurationMS samples, LTRIMed to latencyCap
//	activity:dropped:<channelID>  STRING, this sink's own write-failure count
//
// Budget math for the feed key (activity.go promises ~6KB/channel): the wire
// row's fixed JSON overhead (short keys, RFC3339Nano timestamp) is 66 bytes;
// capping Text to maxTextBytes(40) and Meta to maxMetaBytes(14) puts a row at
// 120 bytes worst case, so feedCap(50) rows is ~6.0KB, not "however long the
// caller's strings happen to be."
//
// The dropped key is written as a side effect of every SUCCESSFUL Emit,
// piggy-backing the in-process counter's current value onto the same
// pipelined write (see Emit) rather than opening a second write path. During
// an outage that stops all writes it goes stale at the last known value,
// which is the same best-effort posture as the rest of this read tier.
type Store struct {
	client valkey.Client

	// dropped counts rows THIS SINK failed to persist: a JSON-encode failure
	// (should not happen; Row is plain data) or a Valkey write that errored
	// or blew writeTimeout. It does NOT cover rows shed by the pipeline's
	// observer mailbox under backpressure (app/sesame/engine/observe.go's
	// per-lane dropped counter) — that counter is unexported and not
	// reachable from this package, by design (see observe.go's package
	// doc). Emit is never told about those; this is the only drop count
	// this package can see or report.
	dropped atomic.Uint64
}

// NewStore builds a Store over an existing Valkey client. It does not own the
// client's lifecycle (matches every other Valkey-backed store in this repo:
// ValkeyLiveStore, ValkeyQueueStore, etc. all borrow rather than dial).
func NewStore(client valkey.Client) *Store {
	return &Store{client: client}
}

// Dropped returns this sink's write-failure count so far (see Store's doc for
// exactly what it covers). Exported for tests; production reads go through
// the persisted activity:dropped:<channelID> key instead (see Read).
func (s *Store) Dropped() uint64 {
	return s.dropped.Load()
}

const (
	feedKeyPrefix    = "activity:feed:"
	latencyKeyPrefix = "activity:latency:"
	droppedKeyPrefix = "activity:dropped:"

	// feedCap bounds the feed LIST; latencyCap bounds the latency reservoir
	// used for the median (see Read/median below).
	feedCap    = 50
	latencyCap = 32

	// feedTTL is refreshed on every write, so it only expires a channel that
	// has gone fully quiet. 12h matches SESAME_LIVE_TTL's default bound on a
	// single stream (app/sesame/internal/config/config.go).
	feedTTL = 12 * time.Hour

	maxTextBytes = 40
	maxMetaBytes = 14

	writeTimeout = 200 * time.Millisecond
	readTimeout  = 200 * time.Millisecond
)

func feedKey(channelID string) string    { return feedKeyPrefix + channelID }
func latencyKey(channelID string) string { return latencyKeyPrefix + channelID }
func droppedKey(channelID string) string { return droppedKeyPrefix + channelID }

// wireRow is the on-wire JSON shape written into the feed LIST. Field names
// are single letters deliberately: see the budget math in Store's doc.
type wireRow struct {
	Kind Kind   `json:"k"`
	Text string `json:"x"`
	Meta string `json:"m"`
	At   string `json:"a"`
	Dur  int    `json:"d"`
}

// truncateBytes cuts s to at most max bytes without splitting a multi-byte
// UTF-8 rune in half (a Twitch display name can carry non-ASCII).
func truncateBytes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	for max > 0 && !utf8.RuneStart(s[max]) {
		max--
	}
	return s[:max]
}

func encodeRow(row Row) ([]byte, error) {
	return codec.Marshal(wireRow{
		Kind: row.Kind,
		Text: truncateBytes(row.Text, maxTextBytes),
		Meta: truncateBytes(row.Meta, maxMetaBytes),
		At:   row.At.UTC().Format(time.RFC3339Nano),
		Dur:  row.DurationMS,
	})
}

func decodeRow(raw string) (Row, bool) {
	var w wireRow
	if err := codec.Unmarshal([]byte(raw), &w); err != nil {
		return Row{}, false
	}
	at, err := time.Parse(time.RFC3339Nano, w.At)
	if err != nil {
		return Row{}, false
	}
	return Row{Kind: w.Kind, Text: w.Text, Meta: w.Meta, At: at, DurationMS: w.Dur}, true
}

// Emit implements Sink. It is fire-and-forget from the caller's point of
// view (bounded by writeTimeout) and never blocks past that bound or panics
// on a Valkey outage; failures only increment dropped.
func (s *Store) Emit(ctx context.Context, channelID string, row Row) {
	data, err := encodeRow(row)
	if err != nil {
		s.dropped.Add(1)
		return
	}
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	c := pkgvalkey.Primary(s.client)
	cmds := feedWriteCmds(c, channelID, data, s.dropped.Load())
	if row.Kind == KindCommand && row.DurationMS > 0 {
		cmds = append(cmds, latencyWriteCmds(c, channelID, row.DurationMS)...)
	}
	if err := execWrites(ctx, c, cmds); err != nil {
		s.dropped.Add(1)
	}
}

// feedWriteCmds pushes one row, trims the list back to feedCap, refreshes its
// TTL, and piggy-backs the current dropped snapshot onto the same batch (see
// Store's doc for why that key rides along here instead of its own write).
func feedWriteCmds(c valkey.Client, channelID string, data []byte, dropped uint64) []valkey.Completed {
	key := feedKey(channelID)
	ttl := int64(feedTTL.Seconds())
	return []valkey.Completed{
		c.B().Lpush().Key(key).Element(string(data)).Build(),
		c.B().Ltrim().Key(key).Start(0).Stop(feedCap - 1).Build(),
		c.B().Expire().Key(key).Seconds(ttl).Build(),
		c.B().Set().Key(droppedKey(channelID)).Value(strconv.FormatUint(dropped, 10)).ExSeconds(ttl).Build(),
	}
}

func latencyWriteCmds(c valkey.Client, channelID string, ms int) []valkey.Completed {
	key := latencyKey(channelID)
	ttl := int64(feedTTL.Seconds())
	return []valkey.Completed{
		c.B().Lpush().Key(key).Element(strconv.Itoa(ms)).Build(),
		c.B().Ltrim().Key(key).Start(0).Stop(latencyCap - 1).Build(),
		c.B().Expire().Key(key).Seconds(ttl).Build(),
	}
}

func execWrites(ctx context.Context, c valkey.Client, cmds []valkey.Completed) error {
	for _, r := range c.DoMulti(ctx, cmds...) {
		if err := r.Error(); err != nil {
			return err
		}
	}
	return nil
}

// Feed is Read's result: the channel's rows newest-first, the derived
// latency median, and this sink's last-persisted drop count.
type Feed struct {
	Rows     []Row
	MedianMS *int
	Dropped  uint64
}

// Read serves the dashboard's activity panel (and, indirectly, documents the
// exact layout console/dashboard/src/lib/server/activity.ts reads directly
// from Valkey, since that TypeScript reader cannot import this Go package).
//
// Uses Primary, not the node-local replica valkey.Do would pick: the feed is
// written and read back constantly on the same hot panel, and a replica read
// here would routinely show a broadcaster's own bot's last action as missing
// for the length of the replication window. See pkg/valkey/routing.go's
// Primary doc.
func (s *Store) Read(ctx context.Context, channelID string) (Feed, error) {
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	c := pkgvalkey.Primary(s.client)
	resps := c.DoMulti(ctx,
		c.B().Lrange().Key(feedKey(channelID)).Start(0).Stop(feedCap-1).Build(),
		c.B().Lrange().Key(latencyKey(channelID)).Start(0).Stop(latencyCap-1).Build(),
		c.B().Get().Key(droppedKey(channelID)).Build(),
	)
	rows, err := readRows(resps[0])
	if err != nil {
		return Feed{}, err
	}
	return Feed{Rows: rows, MedianMS: readMedian(resps[1]), Dropped: readDropped(resps[2])}, nil
}

func readRows(resp valkey.ValkeyResult) ([]Row, error) {
	raw, err := resp.AsStrSlice()
	if err != nil {
		return nil, err
	}
	rows := make([]Row, 0, len(raw))
	for _, r := range raw {
		if row, ok := decodeRow(r); ok {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

// readMedian degrades to nil (no median yet, not an error) on a miss or a
// read failure: the feed itself is still valid without it.
func readMedian(resp valkey.ValkeyResult) *int {
	raw, err := resp.AsStrSlice()
	if err != nil {
		return nil
	}
	return median(raw)
}

// median is a windowed, approximate statistic: the median of at most the
// latencyCap(32) most recent command durations for this channel, not a true
// median over every command ever answered. That window is what keeps this
// cheap (a bounded LIST, no unbounded retention or reservoir sampling); a
// footer figure does not need statistical rigor, but it must not be
// presented as exact, which is why the doc comment on ActivityFeed.medianMs
// (console/dashboard/src/lib/overview-live.ts) spells this out too.
func median(raw []string) *int {
	vals := make([]int, 0, len(raw))
	for _, r := range raw {
		if n, err := strconv.Atoi(r); err == nil {
			vals = append(vals, n)
		}
	}
	if len(vals) == 0 {
		return nil
	}
	sort.Ints(vals)
	mid := vals[len(vals)/2]
	return &mid
}

// readDropped treats a miss (channel never dropped a row) or a read failure
// alike: 0. Both are "nothing known to report," not distinct states worth a
// second bool on Feed.
func readDropped(resp valkey.ValkeyResult) uint64 {
	raw, err := resp.ToString()
	if err != nil {
		return 0
	}
	n, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0
	}
	return n
}
