// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/codec"

	"go.uber.org/zap"
)

func songqueueRedemption(d engine.Deps, log *zap.Logger) module.EventHandler {
	return func(ctx context.Context, c *module.Context, emit module.Emit) error {
		qc, cfg, ev, ok := decodeSongqueueRedemption(d, c, log)
		if !ok {
			return nil
		}
		r := songqueueRedeemRun{qc: qc, cfg: cfg, ev: ev, emit: emit}
		if !cfg.Enabled {
			r.refund("song requests via channel points are turned off, your points were refunded")
			return nil
		}
		if !qc.livePermits(ctx, cfg.AllowOffline) {
			r.refund("song requests only work while live, your points were refunded")
			return nil
		}
		return r.apply(ctx)
	}
}

func decodeSongqueueRedemption(d engine.Deps, c *module.Context, log *zap.Logger) (songQueueCmd, songqueueRedeem, redemptionEvent, bool) {
	var none songQueueCmd
	qc, ok := newSongQueueCmd(d, c, log)
	if !ok {
		return none, songqueueRedeem{}, redemptionEvent{}, false
	}
	cfg, bound := qc.redeemBinding()
	if !bound {
		return none, songqueueRedeem{}, redemptionEvent{}, false
	}
	ev, read := redemptionOf(c)
	if !read {
		return none, songqueueRedeem{}, ev, false
	}
	if ev.Reward.ID != cfg.RewardID {
		return none, songqueueRedeem{}, ev, false
	}
	return qc, cfg, ev, true
}

func (qc songQueueCmd) redeemBinding() (songqueueRedeem, bool) {
	if qc.cfg.Redeem == nil {
		return songqueueRedeem{}, false
	}
	if qc.cfg.Redeem.RewardID == "" {
		return songqueueRedeem{}, false
	}
	return *qc.cfg.Redeem, true
}

func redemptionOf(c *module.Context) (redemptionEvent, bool) {
	if len(c.Env.Event) == 0 {
		return redemptionEvent{}, false
	}
	var ev redemptionEvent
	if err := codec.Unmarshal(c.Env.Event, &ev); err != nil {
		return ev, false
	}
	return ev, true
}

type songqueueRedeemRun struct {
	qc   songQueueCmd
	cfg  songqueueRedeem
	ev   redemptionEvent
	emit module.Emit
}

func (r songqueueRedeemRun) apply(ctx context.Context) error {
	query := strings.TrimSpace(r.ev.UserInput)
	if query == "" {
		r.refund("type a song name or Spotify link, your points were refunded")
		return nil
	}
	r.qc.c.Env.ChatterUserID = r.ev.UserID
	r.qc.c.Env.ChatterUserLogin = r.ev.UserLogin
	r.qc.c.Env.ChatterUserName = r.ev.UserName

	track, failure := r.qc.resolveTrack(ctx, query)
	if failure != "" {
		r.refund(failure + ", your points were refunded")
		return nil
	}
	r.qc.syncWithPlayer(ctx)
	pos, err := r.qc.store.Add(ctx, r.qc.c.BroadcasterID, r.qc.entry(*track), engine.SongQueueLimits{MaxDepth: r.qc.maxDepth, PerRequester: r.qc.quotaFor()})
	if err != nil {
		switch {
		case errors.Is(err, engine.ErrSongQuotaReached):
			r.refund("you are at your song limit for now, your points were refunded")
		case errors.Is(err, engine.ErrSongQueueFull):
			r.refund("the song queue is full, your points were refunded")
		default:
			r.qc.log.Warn("songqueue: redeem add failed", r.qc.c.BID(), zap.Error(err))
			r.refund("could not queue that track, your points were refunded")
		}
		return nil
	}
	if failure := r.qc.pushToPlayer(ctx, track.ID); failure != "" {
		if _, _, rbErr := r.qc.store.RetractOwn(ctx, r.qc.c.BroadcasterID, r.ev.UserID); rbErr != nil {
			r.qc.log.Warn("songqueue: redeem rollback after player refusal failed", r.qc.c.BID(), zap.Error(rbErr))
		}
		r.refund(failure + ", your points were refunded")
		return nil
	}
	r.chat(renderSongqueueRedeemReply(songqueueRedeemReplyParams{
		locale: r.qc.c.Locale,
		text:   r.cfg.ReplyMessage,
		event:  r.ev,
		track:  track.Name,
		pos:    pos,
	}))
	emitRedemptionStatus(r.emit, r.ev, goveeSuccessStatus(r.cfg.OnRedeem))
	return nil
}

func (r songqueueRedeemRun) refund(reason string) {
	user := strings.TrimPrefix(displayName(r.ev.UserName, r.ev.UserLogin), "@")
	r.chat("@" + user + " " + reason)
	emitRedemptionStatus(r.emit, r.ev, outgress.RedemptionCanceled)
}

func (r songqueueRedeemRun) chat(text string) {
	r.emit(&module.Output{
		Type:          outgress.TypeChat,
		BroadcasterID: r.ev.BroadcasterUserID,
		Text:          text,
	})
}

const defaultSongqueueRedeemReply = "@{user} queued {track}, position #{pos}."

type songqueueRedeemReplyParams struct {
	locale string
	text   string
	event  redemptionEvent
	track  string
	pos    int
}

func renderSongqueueRedeemReply(p songqueueRedeemReplyParams) string {
	text := p.text
	if strings.TrimSpace(text) == "" {
		text = defaultSongqueueRedeemReply
	}
	user := strings.TrimPrefix(displayName(p.event.UserName, p.event.UserLogin), "@")
	return module.KV(
		"user", user,
		"track", p.track,
		"input", sanitizeRewardInput(p.event.UserInput),
		"pos", strconv.Itoa(p.pos),
	).WithLocale(module.Locale(p.locale)).ExpandString(text)
}
