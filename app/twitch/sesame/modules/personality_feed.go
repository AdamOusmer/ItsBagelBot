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

// feedCommandCooldown is the shared window on both leaderboard commands. They
// are pure reads, so the window is about chat noise rather than load.
const feedCommandCooldown = 15 * time.Second

// feedBoardTop is how many channels !bagelboard names. Three fits a chat line
// next to the asking channel's own standing.
const feedBoardTop = 3

// feedStandingOnly is the limit !bagels asks for: a negative limit tells the
// modules service to skip the board query entirely and answer with the
// channel's own count and rank.
const feedStandingOnly = -1

// feedLookupCommand is the shape both leaderboard commands share: bail without
// a store, read the board at the limit this command needs, answer the shared
// failure line when the read fails, otherwise render. Only the limit, the log
// key and the renderer differ, so they live as arguments rather than as a
// second copy of the body.
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

// feedRankCommand answers !bagels: how often this channel has fed the bagel
// and where that places it. The fleet-wide tally stays on the "feed the bagel"
// reaction; this command is the per-channel half.
func feedRankCommand(d engine.Deps) module.RunFunc {
	return feedLookupCommand(d, feedStandingOnly, "feed rank", feedRankText)
}

// feedBoardCommand answers !bagelboard: the channels that fed the bagel most,
// then where the asking channel sits among them.
func feedBoardCommand(d engine.Deps) module.RunFunc {
	return feedLookupCommand(d, feedBoardTop, "feed board", feedBoardText)
}

// feedRankText renders !bagels: the standing, or the nudge when this channel
// has never fed the bagel.
func feedRankText(c *module.Context, board engine.FeedBoard) string {
	channel := c.Env.BroadcasterName()
	if board.Rank == 0 {
		return feedText(c, "feed.rank.none", "channel", channel)
	}
	return feedText(c, "feed.rank", append([]string{"channel", channel}, feedStandingArgs(board)...)...)
}

// feedBoardText renders !bagelboard: the podium plus this channel's standing.
// An empty board means nobody has ever fed the bagel, which gets its own line
// rather than a podium with no places on it.
func feedBoardText(c *module.Context, board engine.FeedBoard) string {
	if len(board.Entries) == 0 {
		return feedText(c, "feed.board.empty")
	}
	return feedText(c, "feed.board",
		"places", strings.Join(feedBoardPlaces(board.Entries), ", "),
		"standing", feedBoardStanding(c, board),
	)
}

// feedBoardStanding is the tail of the leaderboard line: the asking channel's
// own place, or a nudge when it has never fed the bagel.
func feedBoardStanding(c *module.Context, board engine.FeedBoard) string {
	if board.Rank == 0 || board.Channel == 0 {
		return feedText(c, "feed.board.none")
	}
	return feedText(c, "feed.board.standing", feedStandingArgs(board)...)
}

// feedStandingArgs is the count/rank/ranked triple both standing lines expand.
func feedStandingArgs(board engine.FeedBoard) []string {
	return []string{
		"count", strconv.FormatUint(board.Channel, 10),
		"rank", strconv.FormatUint(board.Rank, 10),
		"ranked", strconv.FormatUint(board.Ranked, 10),
	}
}

// feedBoardPlaces renders the podium, one "1. name (count)" per entry.
func feedBoardPlaces(entries []engine.FeedBoardEntry) []string {
	places := make([]string, 0, len(entries))
	for i, entry := range entries {
		places = append(places, strconv.Itoa(i+1)+". "+feedBoardName(entry)+
			" ("+strconv.FormatUint(entry.Count, 10)+")")
	}
	return places
}

// feedBoardName falls back to the id when a row carries no stored display name
// (a feeding whose event had none), so a nameless row still ranks.
func feedBoardName(entry engine.FeedBoardEntry) string {
	if entry.Name != "" {
		return entry.Name
	}
	return "channel " + strconv.FormatUint(entry.BroadcasterID, 10)
}

// feedText renders one localized line. kv are {token},value pairs; {user} (the
// invoking chatter) is always available, matching the quotes commands.
func feedText(c *module.Context, key string, kv ...string) string {
	return module.ExpandString(i18n.T(c.Locale, key), func(k string) (string, bool) {
		for i := 0; i+1 < len(kv); i += 2 {
			if kv[i] == k {
				return kv[i+1], true
			}
		}
		if k == "user" {
			return c.Env.ChatterUserLogin, true
		}
		return module.ParseDynamic(k)
	})
}

func feedEmit(c *module.Context, emit module.Emit, text string) {
	emit(&module.Output{Type: outgress.TypeChat, BroadcasterID: c.Env.BroadcasterUserID, Text: text})
}
