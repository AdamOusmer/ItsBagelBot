// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package voice

import (
	"cmp"
	"context"
	"strings"
	"time"
	"unicode/utf8"

	overlaysrpc "ItsBagelBot/internal/domain/rpc/overlays"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/domain/voiceplan"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/monitor"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

const (
	usersTimeout   = 3 * time.Second
	publishTimeout = time.Second
)

type Publisher struct {
	RPC  *nats.Conn
	Jobs *bus.GossipJobs
	Log  *zap.Logger
}

func (p Publisher) Enabled(ctx context.Context, id string) (bool, error) {
	access, err := Check(ctx, p.RPC, id)
	return access.Allowed(), err
}

func (p Publisher) Publish(ctx context.Context, job voiceplan.AlertJob) error {
	access, err := Check(ctx, p.RPC, job.BroadcasterID)
	if err != nil {
		return err
	}
	job = resolve(job, access)
	if !access.Allowed() || job.Text == "" {
		p.logSkipped(ctx, job)
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, publishTimeout)
	defer cancel()
	return p.Jobs.Publish(ctx, bus.GossipJob{Kind: bus.GossipJobTTS, UserID: job.BroadcasterID, EventID: job.EventID, Value: job})
}

func (p Publisher) BlockedViewers(ctx context.Context, id string) ([]string, error) {
	reply, err := bus.RequestJSONTimeout[overlaysrpc.TtsSettingsReply](ctx, p.RPC,
		overlaysrpc.TtsSettingsSubject, overlaysrpc.TtsUserRequest{UserID: id}, usersTimeout)
	return reply.BlockedViewers, err
}

func (p Publisher) logSkipped(ctx context.Context, job voiceplan.AlertJob) {
	if p.Log == nil {
		return
	}
	monitor.TxnLogger(ctx, p.Log).Info("voice job dropped", zap.String("broadcaster_id", job.BroadcasterID),
		zap.String("event_id", job.EventID), zap.String("trace_id", job.TraceID), zap.String("kind", string(job.Kind)),
		zap.String("reason", "not_allowed"))
}

type Access struct {
	Settings   overlaysrpc.TtsSettings
	Configured bool
	Allowance  usersrpc.VoiceCheckReply
}

func (a Access) Allowed() bool {
	linked := a.Configured && a.Settings.Enabled
	return linked && a.Allowance.Allowed
}

func Check(ctx context.Context, nc *nats.Conn, id string) (Access, error) {
	settings, err := bus.RequestJSONTimeout[overlaysrpc.TtsSettingsReply](ctx, nc,
		overlaysrpc.TtsSettingsSubject, overlaysrpc.TtsUserRequest{UserID: id}, usersTimeout)
	if err != nil {
		return Access{}, err
	}
	access := Access{Settings: settings.Settings, Configured: settings.Configured}
	if !access.Settings.Enabled {
		return access, nil
	}
	req := usersrpc.VoiceCheckRequest{UserID: id}
	reply, err := bus.RequestJSONTimeout[usersrpc.VoiceCheckReply](ctx, nc, usersrpc.VoiceCheckSubject, req, usersTimeout)
	if bus.NotFoundReply(err) {
		return access, nil
	}
	access.Allowance = reply
	return access, err
}

func resolve(job voiceplan.AlertJob, access Access) voiceplan.AlertJob {
	seconds := min(int64(access.Settings.MaxSeconds), access.Allowance.RemainingTokens/voiceplan.TokensPerSecond)
	runeLimit := min(max(seconds, 0)*voiceplan.CharsPerSecond, voiceplan.MaxSpeechRunes)
	job.Text = trimSpeech(job.Text, int(runeLimit))
	job.Voice = cmp.Or(job.Voice, access.Settings.Voice, voiceplan.DefaultVoice)
	return job
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
