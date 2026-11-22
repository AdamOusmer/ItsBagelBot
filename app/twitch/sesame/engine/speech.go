// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"time"

	"ItsBagelBot/internal/domain/voiceplan"
	"ItsBagelBot/internal/projection"
)

type SpeechRequest struct {
	Kind          voiceplan.AlertKind
	BroadcasterID uint64
	Identity      string
	TraceID       string
	Text          string
	Style         string
	Voice         string
}

type SpeechQueue struct {
	Voice AlertVoicePublisher
	Proj  projection.Reader
}

func (d Deps) Speech() SpeechQueue {
	return SpeechQueue{Voice: d.AlertVoice, Proj: d.Proj}
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
	})
}

func (r SpeechRequest) jobID() string {
	digest := sha256.Sum256([]byte(string(r.Kind) + ":" + r.TraceID + ":" + r.Identity))
	return hex.EncodeToString(digest[:16])
}
