// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strconv"
	"time"

	"ItsBagelBot/app/twitch/sesame/automod"
	"ItsBagelBot/internal/domain/voiceplan"
	"ItsBagelBot/internal/moderation"
	"ItsBagelBot/internal/projection"

	"github.com/newrelic/go-agent/v3/newrelic"
)

type SpeechRequest struct {
	Kind            voiceplan.AlertKind
	BroadcasterID   uint64
	Identity        string
	TraceID         string
	Text            string
	Style           string
	Voice           string
	ViewerID        string
	Source          voiceplan.SpeechSource
	SourceMessageID string
}

type SpeechQueue struct {
	Voice AlertVoicePublisher
	Proj  projection.Reader
}

func (d Deps) Speech() SpeechQueue {
	return SpeechQueue{Voice: d.AlertVoice, Proj: d.Proj}
}

type SpeechSubject struct {
	BroadcasterID uint64
	ViewerID      string
	Source        voiceplan.SpeechSource
}

func (s SpeechSubject) viewer() bool {
	return s.Source == voiceplan.SourceViewer && s.ViewerID != ""
}

const (
	speechGateAttr    = "tts.gate"
	speechBreakerAttr = "tts.breaker"
	speechGatePass    = "pass"
	speechBlockManual = "manual"
)

func (q SpeechQueue) Screen(ctx context.Context, subject SpeechSubject, in moderation.SpeechInput, policy moderation.SpeechPolicy) (string, bool, error) {
	txn := newrelic.FromContext(ctx)
	blocked, err := q.manuallyBlocked(ctx, subject)
	if err != nil {
		return "", false, err
	}
	if blocked {
		txn.AddAttribute(speechBreakerAttr, speechBlockManual)
		return "", false, nil
	}
	in.Message = automod.StripLinks(in.Message)
	verdict := moderation.CheckSpeech(in, policy)
	txn.AddAttribute(speechGateAttr, cmp.Or(string(verdict.Reason), speechGatePass))
	return verdict.Message, !verdict.Blocked, nil
}

func (q SpeechQueue) manuallyBlocked(ctx context.Context, subject SpeechSubject) (bool, error) {
	source, ok := q.Voice.(interface {
		BlockedViewers(context.Context, string) ([]string, error)
	})
	if !ok || !subject.viewer() {
		return false, nil
	}
	blocked, err := source.BlockedViewers(ctx, strconv.FormatUint(subject.BroadcasterID, 10))
	return slices.Contains(blocked, subject.ViewerID), err
}

func (q SpeechQueue) Available() bool {
	if voice, ok := q.Voice.(interface{ Available() bool }); ok {
		return voice.Available()
	}
	return q.Voice != nil
}

// Fails closed: chat, reward and recipe speech stay Premium even where the surface itself is free.
func (q SpeechQueue) Premium(ctx context.Context, broadcasterID uint64) bool {
	if !q.Available() || q.Proj == nil {
		return false
	}
	user, err := q.Proj.User(ctx, broadcasterID)
	return err == nil && user.Premium()
}

func (q SpeechQueue) Allowed(ctx context.Context, broadcasterID uint64) (bool, error) {
	if !q.Premium(ctx, broadcasterID) {
		return false, nil
	}
	return q.Voice.Enabled(ctx, strconv.FormatUint(broadcasterID, 10))
}

func (q SpeechQueue) Publish(ctx context.Context, r SpeechRequest) error {
	return q.Voice.Publish(ctx, voiceplan.AlertJob{
		EventID: r.jobID(), TraceID: r.TraceID, BroadcasterID: strconv.FormatUint(r.BroadcasterID, 10), Kind: r.Kind,
		Text: r.Text, Style: cmp.Or(r.Style, voiceplan.Style(voiceplan.DefaultSpeechStyle)), Voice: r.Voice, CreatedAt: time.Now().UTC(),
		ViewerID: r.ViewerID, Source: r.Source, SourceMessageID: r.SourceMessageID,
	})
}

func (r SpeechRequest) Subject() SpeechSubject {
	return SpeechSubject{BroadcasterID: r.BroadcasterID, ViewerID: r.ViewerID, Source: r.Source}
}

func (r SpeechRequest) jobID() string {
	digest := sha256.Sum256([]byte(string(r.Kind) + ":" + r.TraceID + ":" + r.Identity))
	return hex.EncodeToString(digest[:16])
}
