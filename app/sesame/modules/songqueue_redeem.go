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
		if !qc.livePermits(ctx, cfg.AllowOffline) {
			r.refund("song requests only work while live, your points were refunded")
			return nil
		}
		return r.apply(ctx)
	}
}

// decodeSongqueueRedemption pulls the redeem binding and the redemption event,
// returning ok=false for anything that is not this module's configured reward.
// One guard per reason, in the order they can fail: no store, no binding, an
// unreadable event, someone else's reward.
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

// redeemBinding is the channel-points path's config, and whether it names a
// reward at all: an unbound path has nothing a redemption could match.
func (qc songQueueCmd) redeemBinding() (songqueueRedeem, bool) {
	if qc.cfg.Redeem == nil {
		return songqueueRedeem{}, false
	}
	if qc.cfg.Redeem.RewardID == "" {
		return songqueueRedeem{}, false
	}
	return *qc.cfg.Redeem, true
}

// redemptionOf reads the redemption off the envelope. An empty or unreadable
// payload is not this module's business either way.
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
	// Same reconcile as the chat path: the position in the reply must count
	// only songs still ahead of this one.
	r.qc.syncWithPlayer(ctx)
	// A redemption event carries no badges, so the quota tier resolves from
	// what the context knows: the broadcaster shortcut still applies, everyone
	// else redeems under the everyone tier.
	pos, err := r.qc.store.Add(ctx, r.qc.c.BroadcasterID, r.qc.entry(*track), engine.SongQueueLimits{MaxDepth: r.qc.maxDepth, PerRequester: r.qc.quotaFor()})
	if err != nil {
		switch {
		case errors.Is(err, engine.ErrSongQuotaReached):
			r.refund("you are at your song limit for now, your points were refunded")
		case errors.Is(err, engine.ErrSongQueueFull):
			r.refund("the song queue is full, your points were refunded")
		default:
			r.qc.log.Warn("songqueue: redeem add failed", r.qc.bid(), zap.Error(err))
			r.refund("could not queue that track, your points were refunded")
		}
		return nil
	}
	// Same contract as the chat path: the redemption only fulfils when the
	// track is audibly queued. A player refusal rolls the entry back and
	// refunds, since points for an inaudible request are points eaten.
	if failure := r.qc.pushToPlayer(ctx, track.ID); failure != "" {
		if _, _, rbErr := r.qc.store.RetractOwn(ctx, r.qc.c.BroadcasterID, r.ev.UserID); rbErr != nil {
			r.qc.log.Warn("songqueue: redeem rollback after player refusal failed", r.qc.bid(), zap.Error(rbErr))
		}
		r.refund(failure + ", your points were refunded")
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

const defaultSongqueueRedeemReply = "@{user} queued {track}, position #{pos}."

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
