// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/pkg/cache"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/valkey-io/valkey-go"
)

const (
	pyramidWindow = 15 * time.Second
	streakWindow  = 10 * time.Second
)

var streakLadder = []int{5, 10, 25, 50, 100, 250, 500, 1000}

type EmotePlayUpdate struct {
	BroadcasterID uint64
	MsgID         string
	Emote         string
	Width         int
	Copies        int
}

type EmotePlayResult struct {
	PyramidDone     bool
	Apex            int
	StreakMilestone bool
	Streak          int
}

var streakLadderArg = joinLadder(streakLadder)

func joinLadder(ladder []int) string {
	parts := make([]string, len(ladder))
	for i, r := range ladder {
		parts[i] = strconv.Itoa(r)
	}
	return strings.Join(parts, ",")
}

type ValkeyEmotePlay struct {
	client valkey.Client
	pyrWin time.Duration
	stkWin time.Duration
	ttl    time.Duration
}

func NewValkeyEmotePlay(client valkey.Client) *ValkeyEmotePlay {
	return newValkeyEmotePlay(client, pyramidWindow, streakWindow)
}

func newValkeyEmotePlay(client valkey.Client, pyrWin, stkWin time.Duration) *ValkeyEmotePlay {
	return &ValkeyEmotePlay{
		client: client,
		pyrWin: pyrWin,
		stkWin: stkWin,
		ttl:    2 * pyrWin,
	}
}

func emoteplayKey(broadcasterID uint64) string {
	return cache.UserKey("emoteplay:v1:", broadcasterID)
}

func (s *ValkeyEmotePlay) Bump(ctx context.Context, u EmotePlayUpdate) (EmotePlayResult, error) {
	flags, milestone, apex, err := pkg_valkey.RepeatSequence{Key: emoteplayKey(u.BroadcasterID)}.Track(
		ctx, s.client, pkg_valkey.RepeatStep{
			Element: u.Emote, Width: u.Width, Copies: u.Copies, ReplayID: u.MsgID,
			PyramidWindow: s.pyrWin, StreakWindow: s.stkWin, TTL: s.ttl, Ladder: streakLadderArg,
		},
	)
	if err != nil {
		return EmotePlayResult{}, err
	}
	return EmotePlayResult{
		PyramidDone:     flags&1 != 0,
		Apex:            int(apex),
		StreakMilestone: flags&2 != 0,
		Streak:          int(milestone),
	}, nil
}
