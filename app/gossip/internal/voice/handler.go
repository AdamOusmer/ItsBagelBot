// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package voice

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/domain/voiceplan"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/monitor"
	"ItsBagelBot/pkg/valkey"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	valkey_go "github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

const (
	usersTimeout = 3 * time.Second
	durationKey  = "duration_ms"
	tokensKey    = "audio_tokens"

	outcomeAttribute = "voice.outcome"
)

type jobDropReason string

const (
	jobRefused jobDropReason = "refused"
	jobInvalid jobDropReason = "invalid"
	jobExpired jobDropReason = "expired"
	jobPaused  jobDropReason = "paused"
	jobEmpty   jobDropReason = "empty"
)

const emptyRetryDelay = 2 * time.Second

var errEmptyAudio = errors.New("speech provider returned no audio")
var errHeld = errors.New("speech job is held")

var eventIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

type Synthesizer interface {
	Synthesize(context.Context, gossiprpc.Request) gossiprpc.TTSSynthesizeReply
}

type Handler struct {
	RPC     *nats.Conn
	Speech  Synthesizer
	Overlay *bus.Overlays
	Log     *zap.Logger
	Safety  valkey.SpeechSafety
	Store   valkey_go.Client
}

func (h Handler) Handle(ctx context.Context, msg bus.GossipJobMessage) error {
	job, ok := decodeJob(msg)
	if !ok {
		h.logDrop(ctx, job, jobInvalid)
		return bus.ErrDropJob
	}
	if err := h.admit(ctx, job); err != nil {
		return err
	}
	clip, err := h.findOrSynthesize(ctx, job, msg.Delivered)
	if err != nil {
		return err
	}
	return h.deliver(ctx, job, clip)
}

func (h Handler) admit(ctx context.Context, job voiceplan.AlertJob) error {
	if wait := time.Until(job.NotBefore); wait > 0 {
		return bus.JobRetryError{Delay: wait, Err: errHeld}
	}
	gate, err := h.Safety.Gate(ctx, h.Store, job.BroadcasterID)
	if err != nil {
		return err
	}
	if gate != valkey.SpeechGateOpen {
		return h.dropGated(ctx, job, jobPaused, string(gate))
	}
	return nil
}

func (h Handler) dropGated(ctx context.Context, job voiceplan.AlertJob, reason jobDropReason, breaker string) error {
	txn := newrelic.FromContext(ctx)
	txn.AddAttribute("tts.gate", string(reason))
	txn.AddAttribute("tts.breaker", breaker)
	h.logDrop(ctx, job, reason)
	return bus.ErrDropJob
}

func decodeJob(msg bus.GossipJobMessage) (voiceplan.AlertJob, bool) {
	var job voiceplan.AlertJob
	if msg.Kind != bus.GossipJobTTS || codec.Unmarshal(msg.Data, &job) != nil {
		return job, false
	}
	return job, validJob(job)
}

func validJob(job voiceplan.AlertJob) bool {
	if !eventIDPattern.MatchString(job.EventID) || strings.TrimSpace(job.Text) == "" {
		return false
	}
	_, err := bus.NonZeroUserID(job.BroadcasterID)
	return err == nil && job.Kind.Valid()
}

func (h Handler) findOrSynthesize(ctx context.Context, job voiceplan.AlertJob, delivered uint64) (bus.OverlayClip, error) {
	ref := bus.OverlayClip{Key: job.BroadcasterID + "/" + job.EventID}
	clip, err := h.Overlay.GetClip(ctx, ref)
	if err != nil || clip.Key != "" {
		return clip, err
	}
	reply := h.Speech.Synthesize(ctx, gossiprpc.Request{
		ChannelID: job.BroadcasterID, RequestID: job.EventID, Text: job.Text, Style: job.Style,
		Voice: cmp.Or(job.Voice, voiceplan.DefaultVoice),
	})
	if err := h.recordRefusal(ctx, job, reply); err != nil {
		return bus.OverlayClip{}, err
	}
	if err := h.synthesisError(ctx, job, reply, delivered); err != nil {
		return bus.OverlayClip{}, err
	}
	return h.saveClip(ctx, ref, reply)
}

func (h Handler) recordRefusal(ctx context.Context, job voiceplan.AlertJob, reply gossiprpc.TTSSynthesizeReply) error {
	newrelic.FromContext(ctx).AddAttribute("tts.gate", cmp.Or(reply.Gate, "pass"))
	if !reply.RefusalClass.CountsTowardBreaker() {
		return nil
	}
	return h.Safety.Refusal(ctx, h.Store, job.BroadcasterID, job.EventID)
}

func (h Handler) synthesisError(ctx context.Context, job voiceplan.AlertJob, reply gossiprpc.TTSSynthesizeReply, delivered uint64) error {
	if reply.RefusalClass == gossiprpc.RefusalEmpty {
		return h.emptyOutcome(ctx, job, delivered)
	}
	reason := cmp.Or(reply.Reason, reply.Error)
	if reply.RateLimited {
		err := errors.New(cmp.Or(reason, "speech synthesis rate limited"))
		return bus.JobRetryError{Delay: time.Duration(reply.RetryAfterMS) * time.Millisecond, Err: err}
	}
	if drop := synthesisDropReason(reply); drop != "" {
		h.logDrop(ctx, job, drop)
		return bus.ErrDropJob
	}
	if reason != "" {
		return fmt.Errorf("TTS synthesis failed: %s", reason)
	}
	return nil
}

func (h Handler) emptyOutcome(ctx context.Context, job voiceplan.AlertJob, delivered uint64) error {
	if delivered > 1 {
		h.logDrop(ctx, job, jobEmpty)
		return bus.ErrDropJob
	}
	return bus.JobRetryError{Delay: emptyRetryDelay, Err: errEmptyAudio}
}

var providerJobDrops = map[string]jobDropReason{
	"refused": jobRefused, "invalid": jobInvalid, "unknown voice": jobInvalid, "unknown model": jobInvalid,
}

func synthesisDropReason(reply gossiprpc.TTSSynthesizeReply) jobDropReason {
	switch jobDropReason(reply.Reason) {
	case jobRefused, jobInvalid:
		return jobDropReason(reply.Reason)
	}
	if strings.HasPrefix(reply.Error, "invalid ") {
		return jobInvalid
	}
	return providerJobDrops[reply.Error]
}

func (h Handler) saveClip(ctx context.Context, ref bus.OverlayClip, reply gossiprpc.TTSSynthesizeReply) (bus.OverlayClip, error) {
	if !validAudio(reply) {
		return bus.OverlayClip{}, errors.New("invalid TTS audio response")
	}
	tokens := reply.AudioTokens
	if tokens == 0 {
		tokens = (reply.DurationMS*voiceplan.TokensPerSecond + 999) / 1000
	}
	ref.Metadata = map[string]string{
		durationKey: strconv.FormatInt(reply.DurationMS, 10),
		tokensKey:   strconv.FormatInt(tokens, 10),
	}
	return h.Overlay.PutClip(ctx, bus.OverlayClipData{OverlayClip: ref, WAV: reply.Audio})
}

func validAudio(reply gossiprpc.TTSSynthesizeReply) bool {
	validDuration := reply.DurationMS > 0 && reply.DurationMS <= voiceplan.MaxClipSeconds*1000
	validTokens := reply.AudioTokens >= 0 && reply.AudioTokens <= voiceplan.SecondsToTokens(voiceplan.MaxClipSeconds)
	validWAV := reply.MimeType == "audio/wav" && len(reply.Audio) > 0
	return validWAV && validDuration && validTokens
}

func (h Handler) deliver(ctx context.Context, job voiceplan.AlertJob, clip bus.OverlayClip) error {
	expires := clip.StoredAt.Add(bus.OverlayClipTTL)
	if h.clipExpired(ctx, job, expires) {
		return nil
	}
	duration, durErr := strconv.ParseInt(clip.Metadata[durationKey], 10, 64)
	tokens, tokenErr := strconv.ParseInt(clip.Metadata[tokensKey], 10, 64)
	if err := errors.Join(durErr, tokenErr); err != nil {
		return err
	}
	req := usersrpc.VoiceRecordRequest{
		UserID: job.BroadcasterID, RequestID: job.EventID, AudioTokens: tokens, DurationMS: duration,
	}
	if _, err := bus.RequestJSONTimeout[usersrpc.VoiceReply](ctx, h.RPC, usersrpc.VoiceRecordSubject, req, usersTimeout); err != nil {
		return err
	}
	if h.clipExpired(ctx, job, expires) {
		return nil
	}
	event := voiceplan.OverlayEvent{
		ClipKey: clip.Key, EventID: job.EventID, TraceID: job.TraceID, Kind: job.Kind, DurationMS: duration, ExpiresAt: expires,
	}
	msg := bus.OverlayMessage{Tier: bus.OverlayPremium, Kind: bus.OverlayKindTTS, UserID: job.BroadcasterID, EventID: job.EventID, Value: event}
	if err := h.Overlay.Publish(ctx, msg); err != nil {
		return err
	}
	h.logDelivered(ctx, job, event)
	return nil
}

func (h Handler) clipExpired(ctx context.Context, job voiceplan.AlertJob, expires time.Time) bool {
	expired := !time.Now().Before(expires)
	if expired {
		h.logDrop(ctx, job, jobExpired)
	}
	return expired
}

func (h Handler) logDrop(ctx context.Context, job voiceplan.AlertJob, reason jobDropReason) {
	newrelic.FromContext(ctx).AddAttribute(outcomeAttribute, string(reason))
	if h.Log == nil {
		return
	}
	monitor.TxnLogger(ctx, h.Log).Info("voice job dropped", jobFields(job, zap.String("reason", string(reason)))...)
}

func (h Handler) logDelivered(ctx context.Context, job voiceplan.AlertJob, event voiceplan.OverlayEvent) {
	txn := newrelic.FromContext(ctx)
	txn.AddAttribute(outcomeAttribute, "delivered")
	txn.AddAttribute("voice.duration_ms", event.DurationMS)
	if h.Log == nil {
		return
	}
	monitor.TxnLogger(ctx, h.Log).Info("voice job delivered", jobFields(job, zap.Int64(durationKey, event.DurationMS))...)
}

func jobFields(job voiceplan.AlertJob, extra zap.Field) []zap.Field {
	return []zap.Field{
		zap.String("broadcaster_id", job.BroadcasterID), zap.String("event_id", job.EventID),
		zap.String("trace_id", job.TraceID), zap.String("kind", string(job.Kind)), extra,
	}
}
