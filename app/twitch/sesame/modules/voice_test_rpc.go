// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/internal/voice"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/internal/domain/rpc"
	sesamerpc "ItsBagelBot/internal/domain/rpc/sesame"
	"ItsBagelBot/internal/domain/voiceplan"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/ratelimit"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
)

var voiceTestBucket = ratelimit.NewSpec(1, 1.0/10)

type voiceTestLimit func(context.Context, ratelimit.Request) (bool, error)

type voiceTestRPC struct {
	nc    *nats.Conn
	deps  engine.Deps
	allow voiceTestLimit
}

func SubscribeVoiceTestRPC(w bus.RPCWiring, d engine.Deps, allow voiceTestLimit) error {
	h := &voiceTestRPC{nc: w.NC, deps: d, allow: allow}
	return bus.ServeForUser[sesamerpc.VoiceTestRequest, sesamerpc.VoiceTestReply](w.Within(5*time.Second), sesamerpc.VoiceTestSubject, h.send)
}

func (h *voiceTestRPC) send(ctx context.Context, req sesamerpc.VoiceTestRequest, id uint64) (sesamerpc.VoiceTestReply, error) {
	if id == 0 {
		return sesamerpc.VoiceTestReply{}, bus.ErrInvalidUserID
	}
	if req.ActorID != req.BroadcasterUserID {
		return sesamerpc.VoiceTestReply{Refusal: rpc.Refused(rpc.CodeForbidden, "forbidden")}, nil
	}
	if h.deps.AlertVoice == nil {
		return sesamerpc.VoiceTestReply{Refusal: rpc.Refused(rpc.CodeUnavailable, "voice unavailable")}, nil
	}
	return h.trySend(ctx, id, req.DisplayName)
}

func (h *voiceTestRPC) trySend(ctx context.Context, id uint64, displayName string) (sesamerpc.VoiceTestReply, error) {
	broadcaster := strconv.FormatUint(id, 10)
	bucket := voiceTestBucket.ForDynamicKey("ratelimit:sesame:voice-test:", "sesame:voice-test", broadcaster)
	allowed, err := h.allow(ctx, bucket)
	if err != nil {
		return sesamerpc.VoiceTestReply{}, err
	}
	if !allowed {
		return sesamerpc.VoiceTestReply{Status: sesamerpc.VoiceTestRateLimited}, nil
	}
	allowance, err := voice.Check(ctx, h.nc, broadcaster)
	if err != nil {
		return sesamerpc.VoiceTestReply{}, err
	}
	if status := testAllowanceStatus(allowance); status != "" {
		return sesamerpc.VoiceTestReply{Status: status}, nil
	}
	if err := h.publish(ctx, id, testSpeechSettings{speaker: displayName, voice: allowance.Settings.Voice}); err != nil {
		return sesamerpc.VoiceTestReply{}, err
	}
	return sesamerpc.VoiceTestReply{Status: sesamerpc.VoiceTestQueued}, nil
}

func testAllowanceStatus(access voice.Access) sesamerpc.VoiceTestStatus {
	switch {
	case access.Allowance.Reason != "ok":
		return sesamerpc.VoiceTestDisabled
	case !access.Configured:
		return sesamerpc.VoiceTestNoLink
	case access.Allowance.RemainingTokens < voiceplan.TokensPerSecond:
		return sesamerpc.VoiceTestNoMinutes
	case !access.Allowed():
		return sesamerpc.VoiceTestDisabled
	default:
		return ""
	}
}

type testSpeechSettings struct {
	speaker string
	voice   string
}

func (h *voiceTestRPC) publish(ctx context.Context, id uint64, settings testSpeechSettings) error {
	user, err := h.deps.Proj.User(ctx, id)
	if err != nil {
		return err
	}
	c := &module.Context{BroadcasterID: id, Locale: user.Locale, Log: h.deps.Log}
	traceID := newrelic.FromContext(ctx).GetTraceMetadata().TraceID
	s := alertSpeech{d: &h.deps, c: c, kind: "cheer", traceID: traceID}
	if err := s.loadConfig(ctx); err != nil {
		return err
	}
	var seed [16]byte
	rand.Read(seed[:])
	in := alertSpeechInput{speaker: settings.speaker, message: i18n.T(c.Locale, "alerts.voice.test")}
	job := s.job(in, hex.EncodeToString(seed[:]))
	job.Voice = cmp.Or(job.Voice, settings.voice)
	return s.publish(ctx, job)
}
