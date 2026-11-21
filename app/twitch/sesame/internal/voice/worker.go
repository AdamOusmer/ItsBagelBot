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
	"unicode/utf8"

	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	overlaysrpc "ItsBagelBot/internal/domain/rpc/overlays"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/domain/voiceplan"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/monitor"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"
)

const (
	usersTimeout   = 3 * time.Second
	gossipTimeout  = 26 * time.Second
	publishTimeout = time.Second
	durationKey    = "duration_ms"
	tokensKey      = "audio_tokens"

	outcomeAttribute = "voice.outcome"
)

type jobDropReason string

const (
	jobRefused    jobDropReason = "refused"
	jobInvalid    jobDropReason = "invalid"
	jobExpired    jobDropReason = "expired"
	jobNotAllowed jobDropReason = "not_allowed"
)

var eventIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

type Publisher struct {
	RPC  *nats.Conn
	Jobs *bus.TTSJobs
}

func (p Publisher) Enabled(ctx context.Context, id string) (bool, error) {
	access, err := check(ctx, p.RPC, id)
	return access.allowed(), err
}

func (p Publisher) Publish(ctx context.Context, job voiceplan.AlertJob) error {
	ctx, cancel := context.WithTimeout(ctx, publishTimeout)
	defer cancel()
	return p.Jobs.Publish(ctx, bus.TTSMessage{BroadcasterID: job.BroadcasterID, EventID: job.EventID, Value: job})
}

type Worker struct {
	RPC     *nats.Conn
	Gossip  string
	Overlay *bus.TTSOverlay
	Log     *zap.Logger
}

func (w Worker) Process(ctx context.Context, payload []byte) error {
	var job voiceplan.AlertJob
	if codec.Unmarshal(payload, &job) != nil || !validJob(job) {
		w.logDrop(ctx, job, jobInvalid)
		return nil
	}

	clip, err := w.findOrSynthesize(ctx, job)
	if err != nil || clip.Key == "" {
		return err
	}
	return w.deliver(ctx, job, clip)
}

func validJob(job voiceplan.AlertJob) bool {
	if !eventIDPattern.MatchString(job.EventID) || strings.TrimSpace(job.Text) == "" {
		return false
	}
	_, err := bus.NonZeroUserID(job.BroadcasterID)
	return err == nil && job.Kind.Valid()
}

type voiceAccess struct {
	settings  overlaysrpc.TtsSettings
	remaining int64
}

func (a voiceAccess) allowed() bool {
	return a.settings.Enabled && a.remaining > 0
}

func check(ctx context.Context, nc *nats.Conn, id string) (voiceAccess, error) {
	settings, err := bus.RequestJSONTimeout[overlaysrpc.TtsSettingsReply](ctx, nc,
		overlaysrpc.TtsSettingsSubject, overlaysrpc.TtsUserRequest{UserID: id}, usersTimeout)
	if err != nil {
		return voiceAccess{}, err
	}
	if !settings.Configured || !settings.Settings.Enabled {
		return voiceAccess{}, nil
	}
	req := usersrpc.VoiceCheckRequest{UserID: id}
	reply, err := bus.RequestJSONTimeout[usersrpc.VoiceCheckReply](ctx, nc, usersrpc.VoiceCheckSubject, req, usersTimeout)
	if bus.NotFoundReply(err) {
		return voiceAccess{}, nil
	}
	return voiceAccess{settings: settings.Settings, remaining: reply.RemainingTokens}, err
}

func (w Worker) findOrSynthesize(ctx context.Context, job voiceplan.AlertJob) (bus.TTSClip, error) {
	ref := bus.TTSClip{Key: job.BroadcasterID + "/" + job.EventID}
	clip, err := w.Overlay.GetClip(ctx, ref)
	if err != nil || clip.Key != "" {
		return clip, err
	}
	allowance, err := check(ctx, w.RPC, job.BroadcasterID)
	if err != nil {
		return bus.TTSClip{}, err
	}
	req := synthesisRequest(job, allowance)
	if !allowance.allowed() || req.Text == "" {
		w.logDrop(ctx, job, jobNotAllowed)
		return bus.TTSClip{}, nil
	}
	return w.synthesize(ctx, ref, req, job)
}

func (w Worker) synthesize(ctx context.Context, ref bus.TTSClip, req gossiprpc.Request, job voiceplan.AlertJob) (bus.TTSClip, error) {
	rpcCtx, cancel := context.WithTimeout(ctx, gossipTimeout)
	defer cancel()
	var reply gossiprpc.TTSSynthesizeReply
	err := bus.RequestInto(rpcCtx, w.RPC, bus.RPCRequest{Subject: w.Gossip, Body: req, DecodeErrorReply: true}, &reply)
	if err != nil {
		return bus.TTSClip{}, err
	}
	if audio, err := w.synthesisOutcome(ctx, job, reply); !audio {
		return bus.TTSClip{}, err
	}
	return w.saveClip(ctx, ref, reply)
}

func (w Worker) synthesisOutcome(ctx context.Context, job voiceplan.AlertJob, reply gossiprpc.TTSSynthesizeReply) (bool, error) {
	reason := cmp.Or(reply.Reason, reply.Error)
	if reply.RateLimited {
		err := errors.New(cmp.Or(reason, "speech synthesis rate limited"))
		return false, bus.TTSRetryError{Delay: time.Duration(reply.RetryAfterMS) * time.Millisecond, Err: err}
	}
	if drop := synthesisDropReason(reply); drop != "" {
		w.logDrop(ctx, job, drop)
		return false, nil
	}
	if reason != "" {
		return false, fmt.Errorf("TTS synthesis failed: %s", reason)
	}
	return true, nil
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

func (w Worker) logDrop(ctx context.Context, job voiceplan.AlertJob, reason jobDropReason) {
	newrelic.FromContext(ctx).AddAttribute(outcomeAttribute, string(reason))
	if w.Log == nil {
		return
	}
	monitor.TxnLogger(ctx, w.Log).Info("voice job dropped", jobFields(job, zap.String("reason", string(reason)))...)
}

func (w Worker) logDelivered(ctx context.Context, job voiceplan.AlertJob, event voiceplan.OverlayEvent) {
	txn := newrelic.FromContext(ctx)
	txn.AddAttribute(outcomeAttribute, "delivered")
	txn.AddAttribute("voice.duration_ms", event.DurationMS)
	if w.Log == nil {
		return
	}
	monitor.TxnLogger(ctx, w.Log).Info("voice job delivered", jobFields(job, zap.Int64(durationKey, event.DurationMS))...)
}

func jobFields(job voiceplan.AlertJob, extra zap.Field) []zap.Field {
	return []zap.Field{
		zap.String("broadcaster_id", job.BroadcasterID), zap.String("event_id", job.EventID),
		zap.String("trace_id", job.TraceID), zap.String("kind", string(job.Kind)), extra,
	}
}

func synthesisRequest(job voiceplan.AlertJob, access voiceAccess) gossiprpc.Request {
	seconds := min(int64(access.settings.MaxSeconds), access.remaining/voiceplan.TokensPerSecond)
	runeLimit := min(max(seconds, 0)*voiceplan.CharsPerSecond, voiceplan.MaxSpeechRunes)
	return gossiprpc.Request{
		ChannelID: job.BroadcasterID, RequestID: job.EventID,
		Text: trimSpeech(job.Text, int(runeLimit)), Style: job.Style,
		Voice: cmp.Or(job.Voice, access.settings.Voice, voiceplan.DefaultVoice),
	}
}

func trimSpeech(text string, limit int) string {
	end := 0
	for end < len(text) && limit > 0 {
		_, width := utf8.DecodeRuneInString(text[end:])
		end += width
		limit--
	}
	if end < len(text) {
		text = withoutPartialTag(text[:end])
	}
	return strings.TrimSpace(text)
}

func withoutPartialTag(text string) string {
	open := strings.LastIndexByte(text, '<')
	if open > strings.LastIndexByte(text, '>') {
		return text[:open]
	}
	return text
}

func (w Worker) saveClip(ctx context.Context, ref bus.TTSClip, reply gossiprpc.TTSSynthesizeReply) (bus.TTSClip, error) {
	if !validAudio(reply) {
		return bus.TTSClip{}, errors.New("invalid TTS audio response")
	}
	tokens := reply.AudioTokens
	if tokens == 0 {
		tokens = (reply.DurationMS*voiceplan.TokensPerSecond + 999) / 1000
	}
	ref.Metadata = map[string]string{
		durationKey: strconv.FormatInt(reply.DurationMS, 10),
		tokensKey:   strconv.FormatInt(tokens, 10),
	}
	return w.Overlay.PutClip(ctx, bus.TTSClipData{TTSClip: ref, WAV: reply.Audio})
}

func validAudio(reply gossiprpc.TTSSynthesizeReply) bool {
	validDuration := reply.DurationMS > 0 && reply.DurationMS <= voiceplan.MaxClipSeconds*1000
	validTokens := reply.AudioTokens >= 0 && reply.AudioTokens <= voiceplan.SecondsToTokens(voiceplan.MaxClipSeconds)
	validWAV := reply.MimeType == "audio/wav" && len(reply.Audio) > 0
	return validWAV && validDuration && validTokens
}

func (w Worker) deliver(ctx context.Context, job voiceplan.AlertJob, clip bus.TTSClip) error {
	expires := clip.StoredAt.Add(bus.TTSClipTTL)
	if w.clipExpired(ctx, job, expires) {
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
	if _, err := bus.RequestJSONTimeout[usersrpc.VoiceReply](ctx, w.RPC, usersrpc.VoiceRecordSubject, req, usersTimeout); err != nil {
		return err
	}
	if w.clipExpired(ctx, job, expires) {
		return nil
	}
	event := voiceplan.OverlayEvent{
		ClipKey: clip.Key, EventID: job.EventID, TraceID: job.TraceID, Kind: job.Kind, DurationMS: duration, ExpiresAt: expires,
	}
	if err := w.Overlay.Publish(ctx, bus.TTSMessage{BroadcasterID: job.BroadcasterID, EventID: job.EventID, Value: event}); err != nil {
		return err
	}
	w.logDelivered(ctx, job, event)
	return nil
}

func (w Worker) clipExpired(ctx context.Context, job voiceplan.AlertJob, expires time.Time) bool {
	expired := !time.Now().Before(expires)
	if expired {
		w.logDrop(ctx, job, jobExpired)
	}
	return expired
}
