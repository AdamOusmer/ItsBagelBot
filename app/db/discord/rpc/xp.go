// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"

	"ItsBagelBot/internal/domain/rpc/discorddata"
)

type xpRPC struct{ repo XPStore }

// subscribeXP registers the four member-XP verbs.
func subscribeXP(w Wiring) error {
	h := xpRPC{repo: w.Repo}
	return errors.Join(
		serve(w, discorddata.VerbXPGet, h.get),
		serve(w, discorddata.VerbXPAdd, h.add),
		serve(w, discorddata.VerbXPDaily, h.daily),
		serve(w, discorddata.VerbXPTop, h.top),
	)
}

func (h xpRPC) get(ctx context.Context, req discorddata.XPGetRequest) discorddata.XPGetReply {
	row, found, err := h.repo.XPGet(ctx, req.GuildID, req.UserID)
	if err != nil {
		message, code := failure(err)
		return discorddata.XPGetReply{Error: message, Code: code}
	}
	if !found {
		return discorddata.XPGetReply{}
	}
	return discorddata.XPGetReply{
		XPValue:         row.Xp,
		Level:           row.Level,
		LastDailyUnixMs: unixMsPtr(row.LastDaily),
		Found:           true,
	}
}

func (h xpRPC) add(ctx context.Context, req discorddata.XPAddRequest) discorddata.XPAddReply {
	result, err := h.repo.XPAdd(ctx, req.GuildID, req.UserID, req.Delta)
	if err != nil {
		message, code := failure(err)
		return discorddata.XPAddReply{Error: message, Code: code}
	}
	return discorddata.XPAddReply{XPValue: result.XP, Level: result.Level, LeveledUp: result.LeveledUp}
}

func (h xpRPC) daily(ctx context.Context, req discorddata.XPDailyRequest) discorddata.XPDailyReply {
	result, err := h.repo.XPDaily(ctx, req.GuildID, req.UserID, req.Amount)
	if err != nil {
		message, code := failure(err)
		return discorddata.XPDailyReply{Error: message, Code: code}
	}
	return discorddata.XPDailyReply{
		Granted:    result.Granted,
		XPValue:    result.XP,
		Level:      result.Level,
		NextUnixMs: unixMs(result.NextDaily),
	}
}

func (h xpRPC) top(ctx context.Context, req discorddata.XPTopRequest) discorddata.XPTopReply {
	rows, err := h.repo.XPTop(ctx, req.GuildID, req.Limit)
	if err != nil {
		message, code := failure(err)
		return discorddata.XPTopReply{Error: message, Code: code}
	}
	out := make([]discorddata.XPRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, discorddata.XPRow{UserID: row.UserID, XPValue: row.Xp, Level: row.Level})
	}
	return discorddata.XPTopReply{Rows: out}
}
