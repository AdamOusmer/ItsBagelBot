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

type Store struct {
	client valkey.Client

	dropped atomic.Uint64
}

func NewStore(client valkey.Client) *Store {
	return &Store{client: client}
}

func (s *Store) Dropped() uint64 {
	return s.dropped.Load()
}

// web/dashboard/src/lib/server/activity.ts parses these keys and rows; change both together.
const (
	feedKeyPrefix    = "activity:feed:"
	latencyKeyPrefix = "activity:latency:"
	droppedKeyPrefix = "activity:dropped:"

	feedCap    = 50
	latencyCap = 32

	feedTTL = 12 * time.Hour

	maxTextBytes = 40
	maxMetaBytes = 14

	writeTimeout = 200 * time.Millisecond
	readTimeout  = 200 * time.Millisecond
)

func feedKey(channelID string) string    { return feedKeyPrefix + channelID }
func latencyKey(channelID string) string { return latencyKeyPrefix + channelID }
func droppedKey(channelID string) string { return droppedKeyPrefix + channelID }

type wireRow struct {
	Kind Kind   `json:"k"`
	Text string `json:"x"`
	Meta string `json:"m"`
	At   string `json:"a"`
	Dur  int    `json:"d"`
}

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

type Feed struct {
	Rows     []Row
	MedianMS *int
	Dropped  uint64
}

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

func readMedian(resp valkey.ValkeyResult) *int {
	raw, err := resp.AsStrSlice()
	if err != nil {
		return nil
	}
	return median(raw)
}

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
