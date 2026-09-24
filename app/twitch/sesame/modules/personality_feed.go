// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/internal/domain/outgress"

	"go.uber.org/zap"
)

const feedCommandCooldown = 15 * time.Second

const feedBoardTop = 3

const feedStandingOnly = -1

func feedLookupCommand(d engine.Deps, limit int, logKey string, render func(*module.Context, engine.FeedBoard) string) module.RunFunc {
	return func(ctx context.Context, c *module.Context, _ string, emit module.Emit) error {
		if d.Personality == nil {
			return nil
		}
		board, err := d.Personality.FeedBoard(ctx, c.BroadcasterID, limit)
		if err != nil {
			c.Log.Warn("personality: "+logKey+" lookup failed", zap.Error(err))
			feedEmit(c, emit, i18n.T(c.Locale, "feed.unavailable"))
			return nil
		}
		feedEmit(c, emit, render(c, board))
		return nil
	}
}

func feedRankCommand(d engine.Deps) module.RunFunc {
	return feedLookupCommand(d, feedStandingOnly, "feed rank", feedRankText)
}

func feedBoardCommand(d engine.Deps) module.RunFunc {
	return feedLookupCommand(d, feedBoardTop, "feed board", feedBoardText)
}

func feedRankText(c *module.Context, board engine.FeedBoard) string {
	channel := c.Env.BroadcasterName()
	if board.Rank == 0 {
		return feedText(c, "feed.rank.none", "channel", channel)
	}
	return feedText(c, "feed.rank", append([]string{"channel", channel}, feedStandingArgs(board)...)...)
}

func feedBoardText(c *module.Context, board engine.FeedBoard) string {
	if len(board.Entries) == 0 {
		return feedText(c, "feed.board.empty")
	}
	return feedText(c, "feed.board",
		"places", strings.Join(feedBoardPlaces(board.Entries), ", "),
		"standing", feedBoardStanding(c, board),
	)
}

func feedBoardStanding(c *module.Context, board engine.FeedBoard) string {
	if board.Rank == 0 || board.Channel == 0 {
		return feedText(c, "feed.board.none")
	}
	return feedText(c, "feed.board.standing", feedStandingArgs(board)...)
}

func feedStandingArgs(board engine.FeedBoard) []string {
	return []string{
		"count", strconv.FormatUint(board.Channel, 10),
		"rank", strconv.FormatUint(board.Rank, 10),
		"ranked", strconv.FormatUint(board.Ranked, 10),
	}
}

func feedBoardPlaces(entries []engine.FeedBoardEntry) []string {
	places := make([]string, 0, len(entries))
	for i, entry := range entries {
		places = append(places, strconv.Itoa(i+1)+". "+feedBoardName(entry)+
			" ("+strconv.FormatUint(entry.Count, 10)+")")
	}
	return places
}

func feedBoardName(entry engine.FeedBoardEntry) string {
	if entry.Name != "" {
		return entry.Name
	}
	return "channel " + strconv.FormatUint(entry.BroadcasterID, 10)
}

func feedText(c *module.Context, key string, kv ...string) string {
	p := module.Common(c).Merge(module.KV(kv...))
	return p.ExpandString(i18n.T(c.Locale, key))
}

func feedEmit(c *module.Context, emit module.Emit, text string) {
	emit(&module.Output{Type: outgress.TypeChat, BroadcasterID: c.Env.BroadcasterUserID, Text: text})
}
