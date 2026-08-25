// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"
	"time"

	"ItsBagelBot/app/outgress/internal/twitch"
	"ItsBagelBot/internal/domain/rpc/manage"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/env"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"
)

// Channel-points reward management runs under the broadcaster's own token and
// hits Helix synchronously in the RPC handler, mirroring the manage verbs: these
// are low-frequency dashboard operations (a streamer edits a handful of rewards),
// so they do not ride a lane or pay a rate bucket. Per-redemption status updates,
// which CAN be high volume, go the other way — sesame emits them on the outgress
// lane as TypeRedemptionUpdate, rate-limited by the worker.
//
// The mutation verbs (create/update/delete) additionally sit behind the web
// tier's end-user claim: the body's broadcaster_id names whose OWN token the
// mutation would run under, so a wire id that disagrees with the authenticated
// user is an impersonation attempt, not a field to trust. Reads (list) stay
// open to internal service callers, which have no end-user identity to sign.
const rewardHandleTimeout = 6 * time.Second

// errUnauthorized is the only failure text a rejected caller ever sees; whether
// the claim was missing, stale, replayed or mismatched is attacker-useful
// probing feedback and goes to the log instead.
const errUnauthorized = "unauthorized"

type channelPoints struct {
	twitch *twitch.Client
	log    *zap.Logger
	// claimKey is the WEB_TIER_CLAIM_KEY the console signs end-user claims
	// with. Empty fails closed: every mutation verb answers unauthorized until
	// the key ships, never downgraded to trusting wire ids.
	claimKey []byte
}

// SubscribeChannelPoints registers the channel-points reward verbs under prefix:
//
//	<prefix>.channelpoints.list    {broadcaster_id}                 -> {rewards}
//	<prefix>.channelpoints.create  {broadcaster_id, reward}         -> {reward}
//	<prefix>.channelpoints.update  {broadcaster_id, reward_id, reward} -> {reward}
//	<prefix>.channelpoints.delete  {broadcaster_id, reward_id}      -> {}
func SubscribeChannelPoints(nc *nats.Conn, tw *twitch.Client, prefix, queueGroup string, app *newrelic.Application, log *zap.Logger) error {
	cp := &channelPoints{
		twitch:   tw,
		log:      log,
		claimKey: []byte(env.Get("WEB_TIER_CLAIM_KEY", "")),
	}

	if err := bus.QueueSubscribeJSON[manage.RewardRequest, manage.RewardReply](nc, prefix+".channelpoints.list", queueGroup, rewardHandleTimeout, app, log, cp.handleList); err != nil {
		return err
	}

	gated := map[string]func(context.Context, *zap.Logger, *nats.Msg) manage.RewardReply{
		"channelpoints.create": cp.handleCreate,
		"channelpoints.update": cp.handleUpdate,
		"channelpoints.delete": cp.handleDelete,
	}
	for verb, handle := range gated {
		subject := prefix + "." + verb
		if err := cp.subscribeGated(nc, subject, queueGroup, app, handle); err != nil {
			return err
		}
	}
	return nil
}

// subscribeGated registers one claim-gated mutation verb on the raw-message RPC
// path: the gate needs the request HEADERS, which QueueSubscribeJSON's decoded
// envelope cannot carry. The transaction/context/reply plumbing here mirrors
// what QueueSubscribeJSON does for the typed verbs.
func (cp *channelPoints) subscribeGated(nc *nats.Conn, subject, queueGroup string, app *newrelic.Application, handle func(context.Context, *zap.Logger, *nats.Msg) manage.RewardReply) error {
	return bus.QueueSubscribeRPC(nc, subject, queueGroup, func(msg *nats.Msg) {
		txn := app.StartTransaction("rpc " + subject)
		defer txn.End()

		ctx, cancel := context.WithTimeout(newrelic.NewContext(context.Background(), txn), rewardHandleTimeout)
		defer cancel()

		reply := handle(ctx, cp.log.Named(subject), msg)

		body, err := codec.Marshal(reply)
		if err == nil {
			err = msg.Respond(body)
		}
		if err != nil {
			txn.NoticeError(err)
			cp.log.Warn("channelpoints reply failed", zap.String("subject", subject), zap.Error(err))
		}
	})
}

// claimMatches authenticates the end-user claim on msg and requires its uid to
// equal broadcasterID — the mutation may only run under the caller's own
// channel. An empty key refuses everything (see channelPoints.claimKey).
func (cp *channelPoints) claimMatches(log *zap.Logger, msg *nats.Msg, broadcasterID string) bool {
	if len(cp.claimKey) == 0 {
		log.Warn("reward mutation refused: no web claim key configured",
			zap.String("subject", msg.Subject))
		return false
	}
	claim, err := bus.VerifyUserClaim(msg, cp.claimKey, bus.DefaultCallerSkew)
	if err != nil {
		log.Warn("reward mutation refused", zap.String("subject", msg.Subject),
			zap.String("cause", "user claim: "+err.Error()))
		return false
	}
	if claim.UserID != broadcasterID {
		log.Warn("reward mutation refused: body broadcaster disagrees with authenticated user claim",
			zap.String("subject", msg.Subject),
			zap.String("broadcaster_id", broadcasterID), zap.String("claim_uid", claim.UserID))
		return false
	}
	return true
}

func (cp *channelPoints) handleList(ctx context.Context, req manage.RewardRequest) manage.RewardReply {
	if req.BroadcasterID == "" {
		return manage.RewardReply{Error: "bad request"}
	}
	rewards, err := cp.twitch.ListCustomRewards(ctx, req.BroadcasterID)
	if err != nil {
		return cp.fail("channelpoints list", req.BroadcasterID, err)
	}
	out := make([]manage.Reward, 0, len(rewards))
	for _, r := range rewards {
		out = append(out, fromTwitch(r))
	}
	return manage.RewardReply{Rewards: out}
}

func (cp *channelPoints) handleCreate(ctx context.Context, log *zap.Logger, msg *nats.Msg) manage.RewardReply {
	var req manage.RewardRequest
	if err := codec.Unmarshal(msg.Data, &req); err != nil {
		return manage.RewardReply{Error: "bad request"}
	}
	if !cp.claimMatches(log, msg, req.BroadcasterID) {
		return manage.RewardReply{Error: errUnauthorized}
	}
	if req.BroadcasterID == "" || req.Reward == nil {
		return manage.RewardReply{Error: "bad request"}
	}
	created, err := cp.twitch.CreateCustomReward(ctx, req.BroadcasterID, toTwitch(*req.Reward))
	if err != nil {
		return cp.fail("channelpoints create", req.BroadcasterID, err)
	}
	reward := fromTwitch(created)
	return manage.RewardReply{Reward: &reward}
}

func (cp *channelPoints) handleUpdate(ctx context.Context, log *zap.Logger, msg *nats.Msg) manage.RewardReply {
	var req manage.RewardRequest
	if err := codec.Unmarshal(msg.Data, &req); err != nil {
		return manage.RewardReply{Error: "bad request"}
	}
	if !cp.claimMatches(log, msg, req.BroadcasterID) {
		return manage.RewardReply{Error: errUnauthorized}
	}
	if req.BroadcasterID == "" || req.RewardID == "" || req.Reward == nil {
		return manage.RewardReply{Error: "bad request"}
	}
	updated, err := cp.twitch.UpdateCustomReward(ctx, req.BroadcasterID, req.RewardID, toTwitch(*req.Reward))
	if err != nil {
		return cp.fail("channelpoints update", req.BroadcasterID, err)
	}
	reward := fromTwitch(updated)
	return manage.RewardReply{Reward: &reward}
}

func (cp *channelPoints) handleDelete(ctx context.Context, log *zap.Logger, msg *nats.Msg) manage.RewardReply {
	var req manage.RewardRequest
	if err := codec.Unmarshal(msg.Data, &req); err != nil {
		return manage.RewardReply{Error: "bad request"}
	}
	if !cp.claimMatches(log, msg, req.BroadcasterID) {
		return manage.RewardReply{Error: errUnauthorized}
	}
	if req.BroadcasterID == "" || req.RewardID == "" {
		return manage.RewardReply{Error: "bad request"}
	}
	if err := cp.twitch.DeleteCustomReward(ctx, req.BroadcasterID, req.RewardID); err != nil {
		return cp.fail("channelpoints delete", req.BroadcasterID, err)
	}
	return manage.RewardReply{}
}

// fail maps a Helix error to the reply. A missing-scope rejection (the grant
// predates channel:manage:redemptions) and a no-token case both mean the
// broadcaster must re-consent, so both set MissingScope for the reconnect CTA.
func (cp *channelPoints) fail(op, broadcasterID string, err error) manage.RewardReply {
	if errors.Is(err, twitch.ErrMissingScope) || errors.Is(err, twitch.ErrNoUserToken) {
		return manage.RewardReply{MissingScope: true, Error: "reconnect required"}
	}
	cp.log.Warn(op+" failed", zap.String("broadcaster_id", broadcasterID), zap.Error(err))
	return manage.RewardReply{Error: "twitch request failed"}
}

func toTwitch(r manage.Reward) twitch.CustomReward {
	return twitch.CustomReward{
		ID:                         r.ID,
		Title:                      r.Title,
		Cost:                       r.Cost,
		Prompt:                     r.Prompt,
		IsEnabled:                  r.IsEnabled,
		IsPaused:                   r.IsPaused,
		BackgroundColor:            r.BackgroundColor,
		IsUserInputRequired:        r.IsUserInputRequired,
		ShouldSkipQueue:            r.ShouldSkipQueue,
		MaxPerStreamEnabled:        r.MaxPerStreamEnabled,
		MaxPerStream:               r.MaxPerStream,
		MaxPerUserPerStreamEnabled: r.MaxPerUserPerStreamEnabled,
		MaxPerUserPerStream:        r.MaxPerUserPerStream,
		GlobalCooldownEnabled:      r.GlobalCooldownEnabled,
		GlobalCooldownSeconds:      r.GlobalCooldownSeconds,
	}
}

func fromTwitch(r twitch.CustomReward) manage.Reward {
	return manage.Reward{
		ID:                         r.ID,
		Title:                      r.Title,
		Cost:                       r.Cost,
		Prompt:                     r.Prompt,
		IsEnabled:                  r.IsEnabled,
		IsPaused:                   r.IsPaused,
		BackgroundColor:            r.BackgroundColor,
		IsUserInputRequired:        r.IsUserInputRequired,
		ShouldSkipQueue:            r.ShouldSkipQueue,
		MaxPerStreamEnabled:        r.MaxPerStreamEnabled,
		MaxPerStream:               r.MaxPerStream,
		MaxPerUserPerStreamEnabled: r.MaxPerUserPerStreamEnabled,
		MaxPerUserPerStream:        r.MaxPerUserPerStream,
		GlobalCooldownEnabled:      r.GlobalCooldownEnabled,
		GlobalCooldownSeconds:      r.GlobalCooldownSeconds,
	}
}
