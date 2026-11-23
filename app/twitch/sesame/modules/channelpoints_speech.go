// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"cmp"
	"context"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/domain/voiceplan"

	"go.uber.org/zap"
)

func (a rewardAward) speak(ctx context.Context, emit module.Emit) error {
	live, err := a.speechLive(ctx)
	if err != nil || !live {
		return a.refuseSpeech(ctx, emit, "offline", err)
	}
	req, ok, err := a.speechRequest(ctx)
	if err != nil {
		return a.refuseSpeech(ctx, emit, "unavailable", err)
	}
	if !ok {
		return a.refuseSpeech(ctx, emit, "blocked", nil)
	}
	speech := a.d.Speech()
	allowed, err := speech.Allowed(ctx, a.c.BroadcasterID)
	if err != nil || !allowed {
		return a.refuseSpeech(ctx, emit, "unavailable", err)
	}
	if err := speech.Publish(ctx, req); err != nil {
		return err
	}
	a.award(ctx)
	a.record(ctx, "ok")
	binding := a.binding
	binding.OnRedeem = cmp.Or(binding.OnRedeem, voiceplan.DefaultRewardResolution)
	emitRedemptionResolution(binding, a.ev, emit)
	return nil
}

func (a rewardAward) refuseSpeech(ctx context.Context, emit module.Emit, result rewardResult, err error) error {
	if err != nil {
		moduleLog(a.d).Warn("channelpoints: speech admission failed", a.c.BID(), zap.Error(err))
	}
	a.record(ctx, result)
	emit(a.ev.Resolve(outgress.RedemptionCanceled))
	return nil
}

func (a rewardAward) speechLive(ctx context.Context) (bool, error) {
	cfg, err := readAlertConfig(ctx, &a.d, a.c.BroadcasterID)
	if err != nil {
		return false, err
	}
	liveOnly := settingBool(cfg["ttsLiveOnly"], voiceplan.DefaultLiveOnly)
	return loyaltyLive(ctx, a.d, a.c.BroadcasterID, liveOnly), nil
}

func (a rewardAward) speechRequest(ctx context.Context) (engine.SpeechRequest, bool, error) {
	if !speechAvailable(a.c) {
		return engine.SpeechRequest{}, false, nil
	}
	opts := speechOptions{
		queue: a.d.Speech(), voice: a.binding.Voice, style: a.binding.Style, maxChars: voiceplan.MaxSpeechRunes,
		prefixName: a.binding.prefixName(),
	}
	in := viewerSpeech{text: speechText(a.ev.UserInput), name: speechText(displayName(a.ev.UserName, a.ev.UserLogin)), viewerID: a.ev.UserID}
	return opts.request(ctx, a.c, voiceplan.SpeechReward, in)
}

func (b rewardBinding) prefixName() bool {
	if b.Prefix == nil {
		return voiceplan.DefaultPrefixName
	}
	return *b.Prefix
}
