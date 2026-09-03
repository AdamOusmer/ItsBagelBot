// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"testing"

	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/ratelimit"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func newTestWorker(t *testing.T, cfg Config) (*Worker, *observer.ObservedLogs) {
	t.Helper()
	core, logs := observer.New(zapcore.DebugLevel)
	cfg.Log = zap.New(core)
	if cfg.BotID == "" {
		cfg.BotID = "1111"
	}
	return New(cfg), logs
}

// The identity flip must prefer the configured bot id over producer-controlled
// wire data, falling back to SenderID only when nothing is configured.
func TestBotIdentityPrefersConfigured(t *testing.T) {
	w := New(Config{Log: zap.NewNop(), BotID: "999"})
	payload := &outgress.Message{SenderID: "42"}

	got, ok := w.botIdentity("test", payload)
	if !ok || got != "999" {
		t.Fatalf("botIdentity = %q,%v; want configured 999 to win over spoofable SenderID", got, ok)
	}

	empty := New(Config{Log: zap.NewNop()})
	got, ok = empty.botIdentity("test", payload)
	if !ok || got != "42" {
		t.Fatalf("botIdentity fallback = %q,%v; want SenderID when unconfigured", got, ok)
	}

	if _, ok := empty.botIdentity("test", &outgress.Message{}); ok {
		t.Fatal("botIdentity with neither id configured must report ok=false")
	}
}

// The last-mile floor gate: viewer-typed text carrying floor content must be
// dropped by BOTH composition funnels before any send path runs, and clean
// text must pass.
func TestBotSpeechFloorGate(t *testing.T) {
	w, _ := newTestWorker(t, Config{})

	blocked := []string{
		"viewer clipped: grabify.link/xyz → https://clips.twitch.tv/AbCdEf",
		"/announce join grabify.link now",
	}
	for _, text := range blocked {
		ctx := context.Background()
		// Both funnels must return nil (drop, no nack) without reaching the
		// send path — the worker carries no registry or limiter here, so any
		// attempt to actually send would panic the test instead of passing.
		if err := w.sendBotLine(ctx, "123", text); err != nil {
			t.Errorf("sendBotLine(%q) = %v; want silent drop", text, err)
		}
		if err := w.sendBotChat(ctx, "123", text); err != nil {
			t.Errorf("sendBotChat(%q) = %v; want silent drop", text, err)
		}
		if w.botSpeechAllowed(ctx, "123", text) {
			t.Errorf("botSpeechAllowed(%q) = true; want floor hit", text)
		}
	}

	// CheckFloor's predicate is hate lexicon + IP-logger hosts only; scam
	// phrasing is deliberately excluded there (giveaway commands legitimately
	// say "claim your prize"), so this passes the gate too.
	clean := []string{
		"viewer clipped: sick play → https://clips.twitch.tv/AbCdEf",
		"get free nitro at once → https://clips.twitch.tv/AbCdEf",
	}
	for _, text := range clean {
		if !w.botSpeechAllowed(context.Background(), "123", text) {
			t.Errorf("clean line %q blocked by floor gate", text)
		}
	}
}

// Non-numeric BroadcasterIDs are rejected in processPayload before any key is
// built or bucket paid, for every Twitch-typed message (batch items included
// via processBatchItem).
func TestProcessPayloadRejectsMalformedBroadcasterID(t *testing.T) {
	w, logs := newTestWorker(t, Config{})

	payloads := []*outgress.Message{
		{Type: outgress.TypeChat, BroadcasterID: "../outgress:channel:x"},
		{Type: outgress.TypeAnnounce, BroadcasterID: "12a4"},
		{Type: outgress.TypeClip, BroadcasterID: "1e3"},
	}
	for _, p := range payloads {
		if err := w.processPayload(context.Background(), p); err != nil {
			t.Errorf("processPayload(%s/%q) = %v; want silent drop", p.Type, p.BroadcasterID, err)
		}
	}
	if n := logs.FilterMessage("dropping message with invalid broadcaster id").Len(); n != len(payloads) {
		t.Fatalf("malformed-id drop log count = %d, want %d", n, len(payloads))
	}

	// YouTube/Discord types carry foreign id shapes and must not be rejected
	// as malformed (they proceed past the boundary — here only far enough that
	// the absence of the drop log proves the gate did not fire).
	logs2 := logs.FilterMessage("dropping message with invalid broadcaster id")
	yt := &outgress.Message{Type: outgress.TypeYouTubeChat, BroadcasterID: "UCabcdefgh"}
	if err := w.processPayload(context.Background(), yt); err != nil {
		t.Errorf("processPayload(youtube) = %v", err)
	}
	if n := logs2.Len(); n != len(payloads) {
		t.Fatalf("youtube type wrongly floored: drop log count = %d, want %d", n, len(payloads))
	}
}

// countingLimiter records Allow calls and answers with a canned verdict.
type countingLimiter struct {
	calls   int
	allowed bool
}

func (c *countingLimiter) Allow(context.Context, ratelimit.Request) (bool, error) {
	c.calls++
	return c.allowed, nil
}

func (c *countingLimiter) AllowOrdered(context.Context, ratelimit.Request, ratelimit.Request) (uint8, error) {
	c.calls++
	if c.allowed {
		return 0, nil
	}
	return 2, nil
}

// A per-channel guard denial must cut announce/shoutout before ANY shared
// quota is paid and consume the job without an error (no nack, no redelivery
// into a closed bucket).
func TestAnnounceShoutoutGuardDenialDropsBeforeSharedTake(t *testing.T) {
	denied := &countingLimiter{allowed: false}
	w, logs := newTestWorker(t, Config{})
	w.SetGuardLimiter(denied)

	announce := &outgress.Message{Type: outgress.TypeAnnounce, BroadcasterID: "123"}
	if err := w.processAnnounce(context.Background(), announce); err != nil {
		t.Fatalf("processAnnounce under guard denial = %v; want nil (drop)", err)
	}
	shoutout := &outgress.Message{Type: outgress.TypeShoutout, BroadcasterID: "123", To: "someone"}
	if err := w.processShoutout(context.Background(), shoutout); err != nil {
		t.Fatalf("processShoutout under guard denial = %v; want nil (drop)", err)
	}

	// Exactly one guard token attempted per job; the shared Helix take would
	// have needed w.limiter, which is deliberately nil here — reaching it
	// would panic rather than pass.
	if denied.calls != 2 {
		t.Fatalf("guard Allow calls = %d, want 2 (one per job)", denied.calls)
	}
	for _, msg := range []string{
		"dropping announce: per-channel guard exhausted",
		"dropping shoutout: per-channel guard exhausted",
	} {
		if logs.FilterMessage(msg).Len() != 1 {
			t.Errorf("missing warn log %q", msg)
		}
	}

	// Fail-open contract: nil limiter admits.
	open := &countingLimiter{allowed: true}
	_ = open
	if !AllowFailOpen(context.Background(), nil, AnnounceGuard("123"), zap.NewNop()) {
		t.Fatal("AllowFailOpen with nil manager must admit")
	}
}
