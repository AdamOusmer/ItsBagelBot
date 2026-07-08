package rpc

import (
	"context"
	"errors"
	"time"

	"ItsBagelBot/app/outgress/internal/twitch"
	"ItsBagelBot/internal/domain/rpc/manage"
	"ItsBagelBot/pkg/bus"

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
const rewardHandleTimeout = 6 * time.Second

type channelPoints struct {
	twitch *twitch.Client
	log    *zap.Logger
}

// SubscribeChannelPoints registers the channel-points reward verbs under prefix:
//
//	<prefix>.channelpoints.list    {broadcaster_id}                 -> {rewards}
//	<prefix>.channelpoints.create  {broadcaster_id, reward}         -> {reward}
//	<prefix>.channelpoints.update  {broadcaster_id, reward_id, reward} -> {reward}
//	<prefix>.channelpoints.delete  {broadcaster_id, reward_id}      -> {}
func SubscribeChannelPoints(nc *nats.Conn, tw *twitch.Client, prefix, queueGroup string, app *newrelic.Application, log *zap.Logger) error {
	cp := &channelPoints{twitch: tw, log: log}

	verbs := []struct {
		verb    string
		handler func(context.Context, manage.RewardRequest) manage.RewardReply
	}{
		{"channelpoints.list", cp.handleList},
		{"channelpoints.create", cp.handleCreate},
		{"channelpoints.update", cp.handleUpdate},
		{"channelpoints.delete", cp.handleDelete},
	}
	for _, v := range verbs {
		subject := prefix + "." + v.verb
		if err := bus.QueueSubscribeJSON[manage.RewardRequest, manage.RewardReply](nc, subject, queueGroup, rewardHandleTimeout, app, log, v.handler); err != nil {
			return err
		}
	}
	return nil
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

func (cp *channelPoints) handleCreate(ctx context.Context, req manage.RewardRequest) manage.RewardReply {
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

func (cp *channelPoints) handleUpdate(ctx context.Context, req manage.RewardRequest) manage.RewardReply {
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

func (cp *channelPoints) handleDelete(ctx context.Context, req manage.RewardRequest) manage.RewardReply {
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
