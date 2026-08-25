// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/codec"

	"go.uber.org/zap"
)

// songqueueRedemption queues a track from a channel-points redemption of the
// bound reward. Live-only (unless AllowOffline) and the path enable switch
// match the chat path and govee's gate polarity: offline while live-only
// refunds, a disabled path refunds so viewers do not lose points silently.
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
		if !songqueueLivePermits(ctx, d.Live, cfg.AllowOffline, c.BroadcasterID, log) {
			r.refund("song requests only work while live, your points were refunded")
			return nil
		}
		return r.apply(ctx)
	}
}

// decodeSongqueueRedemption pulls the redeem binding and the redemption event,
// returning ok=false for anything that is not this module's configured reward.
func decodeSongqueueRedemption(d engine.Deps, c *module.Context, log *zap.Logger) (songQueueCmd, songqueueRedeem, redemptionEvent, bool) {
	qc, ok := newSongQueueCmd(d, c, log)
	if !ok || qc.cfg.Redeem == nil || qc.cfg.Redeem.RewardID == "" || len(c.Env.Event) == 0 {
		return songQueueCmd{}, songqueueRedeem{}, redemptionEvent{}, false
	}
	var ev redemptionEvent
	if err := codec.Unmarshal(c.Env.Event, &ev); err != nil {
		return songQueueCmd{}, songqueueRedeem{}, ev, false
	}
	if ev.Reward.ID != qc.cfg.Redeem.RewardID {
		return songQueueCmd{}, songqueueRedeem{}, ev, false
	}
	return qc, *qc.cfg.Redeem, ev, true
}

// songqueueRedeemRun is one redemption in flight.
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
	// Resolve against the redeemer, not the chat chatter fields.
	r.qc.c.Env.ChatterUserID = r.ev.UserID
	r.qc.c.Env.ChatterUserLogin = r.ev.UserLogin
	r.qc.c.Env.ChatterUserName = r.ev.UserName

	track, failure := r.qc.resolveTrack(ctx, query)
	if failure != "" {
		r.refund(failure + ", your points were refunded")
		return nil
	}
	pos, err := r.qc.store.Add(ctx, r.qc.c.BroadcasterID, r.qc.entry(*track), r.qc.maxDepth)
	if err != nil {
		switch {
		case errors.Is(err, engine.ErrSongAlreadyQueued):
			r.refund("you already have a song in the queue, your points were refunded")
		case errors.Is(err, engine.ErrSongQueueFull):
			r.refund("the song queue is full, your points were refunded")
		default:
			r.qc.log.Warn("songqueue: redeem add failed", r.qc.bid(), zap.Error(err))
			r.refund("could not queue that track, your points were refunded")
		}
		return nil
	}
	r.chat(renderSongqueueRedeemReply(r.cfg.ReplyMessage, r.ev, track.Name, pos))
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

const defaultSongqueueRedeemReply = "@{user} queued {track} — position #{pos}."

// renderSongqueueRedeemReply fills {user}/{track}/{input}/{pos} from the
// redemption, falling back to defaultSongqueueRedeemReply when blank.
func renderSongqueueRedeemReply(tmpl string, ev redemptionEvent, track string, pos int) string {
	if strings.TrimSpace(tmpl) == "" {
		tmpl = defaultSongqueueRedeemReply
	}
	user := strings.TrimPrefix(displayName(ev.UserName, ev.UserLogin), "@")
	return module.ExpandString(tmpl, func(key string) (string, bool) {
		switch key {
		case "user":
			return user, true
		case "track":
			return track, true
		case "input":
			return ev.UserInput, true
		case "pos":
			return strconv.Itoa(pos), true
		default:
			return "", false
		}
	})
}
