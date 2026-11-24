// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"time"

	valkey_go "github.com/valkey-io/valkey-go"
)

// ScoredLog is a sorted log with no size bound whose members expire together; every Add renews the TTL.
type ScoredLog struct {
	Key string
	TTL time.Duration
}

// Add records member at score; adding the same member again only moves its score.
func (l ScoredLog) Add(ctx context.Context, client valkey_go.Client, score float64, member string) error {
	for _, result := range client.DoMulti(ctx,
		client.B().Zadd().Key(l.Key).ScoreMember().ScoreMember(score, member).Build(),
		client.B().Expire().Key(l.Key).Seconds(int64(l.TTL.Seconds())).Build(),
	) {
		if err := result.Error(); err != nil {
			return err
		}
	}
	return nil
}

// Latest returns the n highest-scored members, highest first.
func (l ScoredLog) Latest(ctx context.Context, client valkey_go.Client, n int64) ([]string, error) {
	return client.Do(ctx, client.B().Zrevrange().Key(l.Key).Start(0).Stop(n-1).Build()).AsStrSlice()
}

// All returns every member, highest score first.
func (l ScoredLog) All(ctx context.Context, client valkey_go.Client) ([]string, error) {
	return client.Do(ctx, client.B().Zrevrange().Key(l.Key).Start(0).Stop(-1).Build()).AsStrSlice()
}
