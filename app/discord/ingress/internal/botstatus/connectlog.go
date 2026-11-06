// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package botstatus

import (
	"context"
	"strconv"
	"time"

	ddiscord "ItsBagelBot/internal/domain/discord"
	pkg_valkey "ItsBagelBot/pkg/valkey"

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
	scores, err := (pkg_valkey.TimeIndex{Key: ddiscord.BotConnectsKey}).Since(ctx, l.client, since)
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
	member := strconv.FormatInt(at.UnixMilli(), 10) + "-" + l.pod
	return (pkg_valkey.TimeIndex{Key: ddiscord.BotConnectsKey}).Add(ctx, l.client, at, member, ddiscord.BotConnectsTTL)
}
