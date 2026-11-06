// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"strconv"
	"time"

	valkey_go "github.com/valkey-io/valkey-go"
)

// SimpleQueue is a FIFO line kept as a sorted set scored by join time, alongside a companion key marking whether the line is open.
type SimpleQueue struct {
	Line string
	Open string
}

// SetOpen marks the line open for ttl and refreshes the line's own TTL alongside it, tolerating a missing line.
func (q SimpleQueue) SetOpen(ctx context.Context, client valkey_go.Client, ttl time.Duration) error {
	for _, r := range client.DoMulti(ctx,
		client.B().Set().Key(q.Open).Value("1").Ex(ttl).Build(),
		client.B().Expire().Key(q.Line).Seconds(int64(ttl.Seconds())).Build(),
	) {
		if err := r.Error(); err != nil && !valkey_go.IsValkeyNil(err) {
			return err
		}
	}
	return nil
}

// SetClosed deletes the open marker.
func (q SimpleQueue) SetClosed(ctx context.Context, client valkey_go.Client) error {
	return DelKeys(ctx, client, q.Open)
}

func (q SimpleQueue) IsOpen(ctx context.Context, client valkey_go.Client) (bool, error) {
	n, err := client.Do(ctx, client.B().Exists().Key(q.Open).Build()).AsInt64()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// Join adds member to the line if absent and refreshes both the line and the open marker's TTL; the open marker's refresh error is ignored, matching the line's own best-effort refresh.
func (q SimpleQueue) Join(ctx context.Context, client valkey_go.Client, member string, at time.Time, ttl time.Duration) (pos, size int64, joined bool, err error) {
	seconds := int64(ttl.Seconds())
	resps := client.DoMulti(ctx,
		client.B().Zadd().Key(q.Line).Nx().ScoreMember().ScoreMember(float64(at.UnixMilli()), member).Build(),
		client.B().Zrank().Key(q.Line).Member(member).Build(),
		client.B().Zcard().Key(q.Line).Build(),
		client.B().Expire().Key(q.Line).Seconds(seconds).Build(),
		client.B().Expire().Key(q.Open).Seconds(seconds).Build(),
	)
	added, err := resps[0].AsInt64()
	if err != nil {
		return 0, 0, false, err
	}
	rank, err := resps[1].AsInt64()
	if err != nil {
		return 0, 0, false, err
	}
	size, err = resps[2].AsInt64()
	if err != nil {
		return 0, 0, false, err
	}
	return rank + 1, size, added > 0, nil
}

func (q SimpleQueue) Remove(ctx context.Context, client valkey_go.Client, member string) (bool, error) {
	n, err := client.Do(ctx, client.B().Zrem().Key(q.Line).Member(member).Build()).AsInt64()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// Pop removes and returns the earliest member, and the line's remaining size.
func (q SimpleQueue) Pop(ctx context.Context, client valkey_go.Client) (member string, remaining int64, err error) {
	resps := client.DoMulti(ctx,
		client.B().Zpopmin().Key(q.Line).Count(1).Build(),
		client.B().Zcard().Key(q.Line).Build(),
	)
	popped, err := resps[0].AsZScores()
	if err != nil {
		return "", 0, err
	}
	remaining, err = resps[1].AsInt64()
	if err != nil {
		return "", 0, err
	}
	if len(popped) == 0 {
		return "", remaining, nil
	}
	return popped[0].Member, remaining, nil
}

// List returns up to n earliest members and the line's total size.
func (q SimpleQueue) List(ctx context.Context, client valkey_go.Client, n int64) (entries []string, total int64, err error) {
	resps := client.DoMulti(ctx,
		client.B().Zrange().Key(q.Line).Min("0").Max(strconv.FormatInt(n-1, 10)).Build(),
		client.B().Zcard().Key(q.Line).Build(),
	)
	entries, err = resps[0].AsStrSlice()
	if err != nil {
		return nil, 0, err
	}
	total, err = resps[1].AsInt64()
	if err != nil {
		return nil, 0, err
	}
	return entries, total, nil
}

func (q SimpleQueue) Clear(ctx context.Context, client valkey_go.Client) error {
	return DelKeys(ctx, client, q.Line)
}
