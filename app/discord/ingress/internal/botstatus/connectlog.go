// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package botstatus

import (
	"context"
	"strconv"
	"time"

	ddiscord "ItsBagelBot/internal/domain/discord"

	"github.com/valkey-io/valkey-go"
)

type ConnectLog struct {
	client valkey.Client
	pod    string
}

func NewConnectLog(client valkey.Client, pod string) *ConnectLog {
	return &ConnectLog{client: client, pod: pod}
}

func (l *ConnectLog) Load(ctx context.Context, since time.Time) ([]time.Time, error) {
	floor := strconv.FormatInt(since.UnixMilli(), 10)
	resps := l.client.DoMulti(ctx,
		l.client.B().Zremrangebyscore().Key(ddiscord.BotConnectsKey).Min("-inf").Max("("+floor).Build(),
		l.client.B().Zrangebyscore().Key(ddiscord.BotConnectsKey).Min(floor).Max("+inf").Withscores().Build(),
	)
	if err := resps[0].Error(); err != nil {
		return nil, err
	}
	scores, err := resps[1].AsZScores()
	if err != nil {
		return nil, err
	}
	return stamps(scores), nil
}

func stamps(scores []valkey.ZScore) []time.Time {
	out := make([]time.Time, 0, len(scores))
	for _, s := range scores {
		out = append(out, time.UnixMilli(int64(s.Score)))
	}
	return out
}

func (l *ConnectLog) Add(ctx context.Context, at time.Time) error {
	ms := at.UnixMilli()
	member := strconv.FormatInt(ms, 10) + "-" + l.pod
	resps := l.client.DoMulti(ctx,
		l.client.B().Zadd().Key(ddiscord.BotConnectsKey).ScoreMember().ScoreMember(float64(ms), member).Build(),
		l.client.B().Expire().Key(ddiscord.BotConnectsKey).Seconds(int64(ddiscord.BotConnectsTTL.Seconds())).Build(),
	)
	return resps[0].Error()
}
