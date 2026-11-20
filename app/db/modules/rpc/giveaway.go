// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"strconv"
	"time"

	"ItsBagelBot/app/db/modules/repository"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"
	"ItsBagelBot/pkg/bus"
)

type giveawayRPC struct {
	repo *repository.GiveawayWinners
}

func SubscribeGiveaways(w bus.RPCWiring, repo *repository.GiveawayWinners, prefix string) error {
	g := giveawayRPC{repo: repo}
	return bus.ServeVerbs(w, prefix,
		giveawayVerb(modulesrpc.VerbGiveawayRecord, g.record),
		giveawayVerb(modulesrpc.VerbGiveawayDeliver, g.deliver),
		giveawayVerb(modulesrpc.VerbGiveawayRecent, g.recent),
		giveawayVerb(modulesrpc.VerbGiveawayWonWithin, g.wonWithin),
	)
}

func giveawayVerb(name string, handle func(context.Context, modulesrpc.GiveawayRequest, uint64) (modulesrpc.GiveawayReply, error)) bus.Verb[modulesrpc.GiveawayRequest, modulesrpc.GiveawayReply] {
	return bus.VerbForUser(name, func(ctx context.Context, req modulesrpc.GiveawayRequest, userID uint64) (modulesrpc.GiveawayReply, error) {
		if userID == 0 {
			return modulesrpc.GiveawayReply{}, bus.ErrInvalidUserID
		}
		return handle(ctx, req, userID)
	})
}

func invalidGiveaway(message string) (modulesrpc.GiveawayReply, error) {
	return modulesrpc.GiveawayReply{Refusal: domainrpc.Refused(domainrpc.CodeInvalid, message)}, nil
}

func validWinnerRef(roundID string, slot int) bool {
	return validWinnerRoundID(roundID) && validWinnerSlot(slot)
}

func validWinnerRoundID(roundID string) bool {
	return roundID != "" && len(roundID) <= modulesrpc.GiveawayRoundIDMaxLen
}

func validWinnerSlot(slot int) bool {
	return slot > 0 && slot <= modulesrpc.GiveawayMaxWinners
}

func validWinnerDetails(w modulesrpc.GiveawayWinner) bool {
	return validWinnerPrize(w) && validWinnerMetadata(w)
}

func validWinnerPrize(w modulesrpc.GiveawayWinner) bool {
	return w.Prize != "" && len(w.Prize) <= modulesrpc.GiveawayPrizeMaxLen
}

func validWinnerMetadata(w modulesrpc.GiveawayWinner) bool {
	return len(w.Login) <= modulesrpc.GiveawayLoginMaxLen && validWinnerTimes(w)
}

func validWinnerTimes(w modulesrpc.GiveawayWinner) bool {
	return w.WonAt > 0 && w.ClaimedAt >= 0
}

func parseWinner(w modulesrpc.GiveawayWinner) (repository.WinnerRow, bool) {
	if !validWinnerRef(w.RoundID, w.Slot) || !validWinnerDetails(w) {
		return repository.WinnerRow{}, false
	}
	viewerID, err := bus.NonZeroUserID(w.ViewerID)
	if err != nil {
		return repository.WinnerRow{}, false
	}
	row := repository.WinnerRow{
		WinnerRef: repository.WinnerRef{RoundID: w.RoundID, Slot: uint8(w.Slot)},
		ViewerID:  viewerID, Login: w.Login, Prize: w.Prize, WonAt: time.UnixMilli(w.WonAt).UTC(), Claimed: w.Claimed,
	}
	if w.ClaimedAt > 0 {
		claimedAt := time.UnixMilli(w.ClaimedAt).UTC()
		row.ClaimedAt = &claimedAt
	}
	return row, true
}

func (g giveawayRPC) record(ctx context.Context, req modulesrpc.GiveawayRequest, userID uint64) (modulesrpc.GiveawayReply, error) {
	if len(req.Winners) > modulesrpc.GiveawayMaxWinners {
		return invalidGiveaway("too many giveaway winners")
	}
	rows := make([]repository.WinnerRow, len(req.Winners))
	slots := make(map[uint8]struct{}, len(req.Winners))
	for i, winner := range req.Winners {
		row, ok := parseWinner(winner)
		if !ok || winner.RoundID != req.Winners[0].RoundID {
			return invalidGiveaway("invalid giveaway winner")
		}
		if _, duplicate := slots[row.Slot]; duplicate {
			return invalidGiveaway("duplicate giveaway winner slot")
		}
		slots[row.Slot] = struct{}{}
		rows[i] = row
	}
	return modulesrpc.GiveawayReply{}, g.repo.Record(ctx, userID, rows)
}

func (g giveawayRPC) deliver(ctx context.Context, req modulesrpc.GiveawayRequest, userID uint64) (modulesrpc.GiveawayReply, error) {
	if !validWinnerRef(req.RoundID, req.Slot) {
		return invalidGiveaway("invalid giveaway winner slot")
	}
	delivery := repository.WinnerDelivery{
		WinnerRef: repository.WinnerRef{RoundID: req.RoundID, Slot: uint8(req.Slot)},
		Delivered: req.Delivered,
	}
	found, err := g.repo.MarkDelivered(ctx, userID, delivery)
	return modulesrpc.GiveawayReply{Found: found}, err
}

func (g giveawayRPC) recent(ctx context.Context, req modulesrpc.GiveawayRequest, userID uint64) (modulesrpc.GiveawayReply, error) {
	if !validRecentPage(req) || len(req.RoundID) > modulesrpc.GiveawayRoundIDMaxLen {
		return invalidGiveaway("invalid giveaway winners page")
	}
	q := modulesrpc.GiveawayRecentQuery{RoundID: req.RoundID, Limit: req.Limit, Offset: req.Offset}
	rows, err := g.repo.Recent(ctx, userID, q)
	return modulesrpc.GiveawayReply{Winners: rows}, err
}

func validRecentPage(req modulesrpc.GiveawayRequest) bool {
	return req.Offset >= 0 && validRecentLimit(req.Limit)
}

func validRecentLimit(limit int) bool {
	return limit >= 0 && limit <= modulesrpc.GiveawayMaxRecentPage
}

func (g giveawayRPC) wonWithin(ctx context.Context, req modulesrpc.GiveawayRequest, userID uint64) (modulesrpc.GiveawayReply, error) {
	if len(req.ViewerIDs) > modulesrpc.GiveawayMaxViewerIDs || req.SinceMs <= 0 {
		return invalidGiveaway("invalid giveaway winners lookup")
	}
	viewers := make([]uint64, len(req.ViewerIDs))
	for i, viewer := range req.ViewerIDs {
		id, err := bus.NonZeroUserID(viewer)
		if err != nil {
			return modulesrpc.GiveawayReply{}, err
		}
		viewers[i] = id
	}
	ids, err := g.repo.WonWithin(ctx, userID, viewers, time.UnixMilli(req.SinceMs).UTC())
	return modulesrpc.GiveawayReply{ViewerIDs: formatGiveawayIDs(ids)}, err
}

func formatGiveawayIDs(ids []uint64) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = strconv.FormatUint(id, 10)
	}
	return out
}
