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

// ConnectLog is the Valkey half of the gateway's connect budget: the sorted
// set at discord.BotConnectsKey, holding one member per gateway connect
// attempt inside the rolling window.
//
// It lives here rather than in internal/gateway for the same reason Reporter
// does -- the gateway package knows about WebSockets and nothing else, and a
// Valkey client in it would make every gateway test need one. gateway
// declares the two-method ConnectLog interface; this is its only
// implementation.
type ConnectLog struct {
	client valkey.Client
	pod    string
}

// NewConnectLog builds the log. pod disambiguates two attempts that land on
// the same millisecond from different pods, which a sorted set would
// otherwise collapse into one member.
func NewConnectLog(client valkey.Client, pod string) *ConnectLog {
	return &ConnectLog{client: client, pod: pod}
}

// Load prunes everything before since and returns what is left, oldest
// first. Pruning on read is what keeps the set from growing without bound
// on a pod that never restarts: the TTL below is a sweep for an abandoned
// key, not the window.
func (l *ConnectLog) Load(ctx context.Context, since time.Time) ([]time.Time, error) {
	floor := strconv.FormatInt(since.UnixMilli(), 10)
	resps := l.client.DoMulti(ctx,
		l.client.B().Zremrangebyscore().Key(ddiscord.BotConnectsKey).Min("-inf").Max("("+floor).Build(),
		l.client.B().Zrangebyscore().Key(ddiscord.BotConnectsKey).Min(floor).Max("+inf").Build(),
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

// stamps reads the attempt times off the scores rather than off the member
// strings: the score is the field Valkey orders and ranges by, so it is the
// one that has to be authoritative if the two ever disagree.
func stamps(scores []valkey.ZScore) []time.Time {
	out := make([]time.Time, 0, len(scores))
	for _, s := range scores {
		out = append(out, time.UnixMilli(int64(s.Score)))
	}
	return out
}

// Add appends one attempt and refreshes the key's sweep TTL.
func (l *ConnectLog) Add(ctx context.Context, at time.Time) error {
	ms := at.UnixMilli()
	member := strconv.FormatInt(ms, 10) + "-" + l.pod
	resps := l.client.DoMulti(ctx,
		l.client.B().Zadd().Key(ddiscord.BotConnectsKey).ScoreMember().ScoreMember(float64(ms), member).Build(),
		l.client.B().Expire().Key(ddiscord.BotConnectsKey).Seconds(int64(ddiscord.BotConnectsTTL.Seconds())).Build(),
	)
	// The EXPIRE is bookkeeping; only a failed ZADD means the attempt went
	// unrecorded, which is the thing the budget has to hear about.
	return resps[0].Error()
}
