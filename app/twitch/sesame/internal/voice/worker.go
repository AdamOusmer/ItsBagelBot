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
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/domain/voiceplan"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

const (
	usersTimeout   = 3 * time.Second
	gossipTimeout  = 26 * time.Second
	publishTimeout = time.Second
	durationKey    = "duration_ms"
	tokensKey      = "audio_tokens"
)

var eventIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

type Publisher struct {
	RPC  *nats.Conn
	Jobs *bus.TTSJobs
}

func (p Publisher) Enabled(ctx context.Context, id string) (bool, error) {
	reply, err := check(ctx, p.RPC, id)
	return reply.Allowed, err
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
	_, known := job.Kind.Defaults()
	return err == nil && known
}

func check(ctx context.Context, nc *nats.Conn, id string) (usersrpc.VoiceCheckReply, error) {
	req := usersrpc.VoiceCheckRequest{UserID: id}
	reply, err := bus.RequestJSONTimeout[usersrpc.VoiceCheckReply](ctx, nc, usersrpc.VoiceCheckSubject, req, usersTimeout)
	if bus.NotFoundReply(err) {
		return usersrpc.VoiceCheckReply{}, nil
	}
	return reply, err
}

func (w Worker) findOrSynthesize(ctx context.Context, job voiceplan.AlertJob) (bus.TTSClip, error) {
	ref := bus.TTSClip{Key: job.BroadcasterID + "/" + job.EventID}
	clip, err := w.Overlay.GetClip(ctx, ref)
	if err != nil || clip.Key != "" {
		return clip, err
	}
	allowance, err := check(ctx, w.RPC, job.BroadcasterID)
	if err != nil || !allowance.Allowed {
		return bus.TTSClip{}, err
	}
	req := synthesisRequest(job, allowance.VoiceAllowance)
	if req.Text == "" {
		return bus.TTSClip{}, nil
	}
	return w.synthesize(ctx, ref, req)
}

func (w Worker) synthesize(ctx context.Context, ref bus.TTSClip, req gossiprpc.Request) (bus.TTSClip, error) {
	rpcCtx, cancel := context.WithTimeout(ctx, gossipTimeout)
	defer cancel()
	var reply gossiprpc.TTSSynthesizeReply
	err := bus.RequestInto(rpcCtx, w.RPC, bus.RPCRequest{Subject: w.Gossip, Body: req, DecodeErrorReply: true}, &reply)
	if err != nil {
		return bus.TTSClip{}, err
	}
	if audio, err := w.synthesisOutcome(req, reply); !audio {
		return bus.TTSClip{}, err
	}
	return w.saveClip(ctx, ref, reply)
}

func (w Worker) synthesisOutcome(req gossiprpc.Request, reply gossiprpc.TTSSynthesizeReply) (bool, error) {
	reason := cmp.Or(reply.Reason, reply.Error)
	switch {
	case reply.RateLimited:
		err := errors.New(cmp.Or(reason, "speech synthesis rate limited"))
		return false, bus.TTSRetryError{Delay: time.Duration(reply.RetryAfterMS) * time.Millisecond, Err: err}
	case terminalSynthesisFailure(reply):
		w.Log.Debug("TTS synthesis rejected", zap.String("event_id", req.RequestID), zap.String("reason", reason))
		return false, nil
	case reason != "":
		return false, fmt.Errorf("TTS synthesis failed: %s", reason)
	default:
		return true, nil
	}
}

func terminalSynthesisFailure(reply gossiprpc.TTSSynthesizeReply) bool {
	switch reply.Reason {
	case "invalid", "refused":
		return true
	}
	switch reply.Error {
	case "invalid", "refused", "unknown voice", "unknown model":
		return true
	}
	return strings.HasPrefix(reply.Error, "invalid ")
}

func synthesisRequest(job voiceplan.AlertJob, allowance usersrpc.VoiceAllowance) gossiprpc.Request {
	seconds := min(int64(allowance.MaxSeconds), allowance.RemainingTokens/voiceplan.TokensPerSecond)
	runeLimit := min(max(seconds, 0)*voiceplan.CharsPerSecond, voiceplan.MaxSpeechRunes)
	return gossiprpc.Request{
		ChannelID: job.BroadcasterID, RequestID: job.EventID,
		Text: trimSpeech(job.Text, int(runeLimit)), Style: job.Style,
		Voice: cmp.Or(job.Voice, allowance.Voice, voiceplan.DefaultVoice),
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
	if w.clipExpired(job, expires) {
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
	if w.clipExpired(job, expires) {
		return nil
	}
	event := voiceplan.OverlayEvent{
		ClipKey: clip.Key, EventID: job.EventID, Kind: job.Kind, DurationMS: duration, ExpiresAt: expires,
	}
	return w.Overlay.Publish(ctx, bus.TTSMessage{BroadcasterID: job.BroadcasterID, EventID: job.EventID, Value: event})
}

func (w Worker) clipExpired(job voiceplan.AlertJob, expires time.Time) bool {
	expired := !time.Now().Before(expires)
	if expired {
		w.Log.Debug("TTS clip expired before delivery", zap.String("event_id", job.EventID), zap.Time("expires_at", expires))
	}
	return expired
}
