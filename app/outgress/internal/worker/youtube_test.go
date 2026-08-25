// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"ItsBagelBot/pkg/codec"
	"context"
	"errors"
	"sync"
	"testing"

	"ItsBagelBot/app/outgress/internal/youtube"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/ratelimit"

	"go.uber.org/zap"
)

// fakeYtBudget records takes and can be primed to refuse or error.
type fakeYtBudget struct {
	takes  int64
	refuse bool
	err    error
}

func (f *fakeYtBudget) Take(_ context.Context, cost int64) (bool, error) {
	f.takes += cost
	if f.err != nil {
		return false, f.err
	}
	return !f.refuse, nil
}

// fakeManager admits everything unless primed to refuse; it pins which bucket
// keys pacing pays into without Valkey.
type fakeManager struct {
	denied  map[string]bool
	lastKey string
}

func (f *fakeManager) Allow(_ context.Context, req ratelimit.Request) (bool, error) {
	key := req.Key
	if key == "" && req.DynamicPrefix != "" {
		key = req.DynamicPrefix + req.Bucket.Value
	}
	f.lastKey = key
	if f.denied[key] {
		return false, nil
	}
	return true, nil
}

func (f *fakeManager) AllowOrdered(ctx context.Context, first, second ratelimit.Request) (uint8, error) {
	if ok, _ := f.Allow(ctx, first); !ok {
		return 1, nil
	}
	if ok, _ := f.Allow(ctx, second); !ok {
		return 2, nil
	}
	return 0, nil
}

// recordingClient captures the call a handler fires and answers with a
// canned reply.
type recordingClient struct {
	mu       sync.Mutex
	calls    int
	lastChat string
	lastText string
	lastMsg  string
	lastTo   string
	lastDur  int64
	reply    error
}

func (r *recordingClient) record() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	return r.reply
}

func (r *recordingClient) SendChatMessage(_ context.Context, liveChatID, text string) error {
	r.lastChat, r.lastText = liveChatID, text
	return r.record()
}

func (r *recordingClient) DeleteChatMessage(_ context.Context, msgID string) error {
	r.lastMsg = msgID
	return r.record()
}

func (r *recordingClient) Ban(_ context.Context, liveChatID, target string) error {
	r.lastChat, r.lastTo = liveChatID, target
	return r.record()
}

func (r *recordingClient) Timeout(_ context.Context, liveChatID, target string, dur int64) error {
	r.lastChat, r.lastTo, r.lastDur = liveChatID, target, dur
	return r.record()
}

var _ youtubeAPI = (*recordingClient)(nil)

func newYouTubeWorker(client youtubeAPI, budget ytBudget, manager ratelimit.Manager) *Worker {
	w := New(Config{Log: zap.NewNop(), Limiter: manager})
	w.SetYouTube(client, budget, youtube.NewChatDirectory())
	return w
}

func chatPayload(text string) *outgress.Message {
	raw, _ := codec.Marshal(map[string]string{"message": text})
	return &outgress.Message{
		Type:          outgress.TypeYouTubeChat,
		BroadcasterID: "UCbroadcaster",
		LiveChatID:    "chat-1",
		Payload:       raw,
	}
}

func TestYouTubeChatSendsAndSpendsBudget(t *testing.T) {
	client := &recordingClient{}
	budget := &fakeYtBudget{}
	manager := &fakeManager{}
	w := newYouTubeWorker(client, budget, manager)

	if err := w.processYouTubeChat(context.Background(), chatPayload("!points")); err != nil {
		t.Fatalf("send failed: %v", err)
	}
	if client.lastText != "!points" || client.lastChat != "chat-1" {
		t.Fatalf("sent %q to %q", client.lastText, client.lastChat)
	}
	if got := budget.takes; got != youtube.QuotaUnitsPerAction {
		t.Fatalf("budget spent = %d, want %d", got, youtube.QuotaUnitsPerAction)
	}
	if manager.lastKey != "ratelimit:yt:chat:UCbroadcaster" {
		t.Fatalf("pacing key = %q", manager.lastKey)
	}
}

func TestYouTubeChatPaysPacingBucket(t *testing.T) {
	client := &recordingClient{}
	manager := &fakeManager{denied: map[string]bool{"ratelimit:yt:chat:UCbroadcaster": true}}
	w := newYouTubeWorker(client, &fakeYtBudget{}, manager)

	err := w.processYouTubeChat(context.Background(), chatPayload("!points"))
	var expected expectedNackError
	if !errors.As(err, &expected) {
		t.Fatalf("err = %v, want an expected nack from the pacing bucket", err)
	}
	if client.calls != 0 {
		t.Fatal("API fired despite pacing denial")
	}
}

func TestYouTubeBudgetRefusalDropsWithoutNack(t *testing.T) {
	client := &recordingClient{}
	w := newYouTubeWorker(client, &fakeYtBudget{refuse: true}, &fakeManager{})

	if err := w.processYouTubeChat(context.Background(), chatPayload("hi")); err != nil {
		t.Fatalf("budget refusal must drop (nil), got %v", err)
	}
	if client.calls != 0 {
		t.Fatal("API fired despite exhausted budget")
	}
}

func TestYouTubeChatWithoutKnownChatDrops(t *testing.T) {
	client := &recordingClient{}
	w := newYouTubeWorker(client, &fakeYtBudget{}, &fakeManager{})
	payload := chatPayload("hi")
	payload.LiveChatID = "" // no producer-supplied id, empty directory

	if err := w.processYouTubeChat(context.Background(), payload); err != nil {
		t.Fatalf("unknown chat must drop (nil), got %v", err)
	}
	if client.calls != 0 {
		t.Fatal("API fired with no target chat")
	}
}

func TestYouTubePermanentErrorsDrop(t *testing.T) {
	for name, reply := range map[string]error{
		"quota":     youtube.ErrQuotaExhausted,
		"ended":     youtube.ErrChatEnded,
		"not found": youtube.ErrChatNotFound,
		"auth":      youtube.ErrAuth,
	} {
		t.Run(name, func(t *testing.T) {
			client := &recordingClient{reply: reply}
			w := newYouTubeWorker(client, &fakeYtBudget{}, &fakeManager{})
			if err := w.processYouTubeDelete(context.Background(), &outgress.Message{
				Type: outgress.TypeYouTubeDelete, BroadcasterID: "UCb", MsgID: "m-1",
			}); err != nil {
				t.Fatalf("permanent error must drop (nil), got %v", err)
			}
		})
	}
}

func TestYouTubeRateLimitNacks(t *testing.T) {
	client := &recordingClient{reply: youtube.ErrRateLimited}
	w := newYouTubeWorker(client, &fakeYtBudget{}, &fakeManager{})

	if err := w.processYouTubeDelete(context.Background(), &outgress.Message{
		Type: outgress.TypeYouTubeDelete, BroadcasterID: "UCb", MsgID: "m-1",
	}); err == nil {
		t.Fatal("rate limit must nack for paced redelivery")
	}
}

func TestYouTubeTimeoutCarriesDuration(t *testing.T) {
	client := &recordingClient{}
	w := newYouTubeWorker(client, &fakeYtBudget{}, &fakeManager{})

	raw, _ := codec.Marshal(map[string]any{"reason": "spam", "duration_seconds": 600})
	payload := &outgress.Message{
		Type: outgress.TypeYouTubeTimeout, BroadcasterID: "UCb",
		To: "UCraider", LiveChatID: "chat-1", Payload: raw,
	}
	if err := w.processYouTubeTimeout(context.Background(), payload); err != nil {
		t.Fatalf("timeout failed: %v", err)
	}
	if client.lastTo != "UCraider" || client.lastDur != 600 {
		t.Fatalf("timeout target=%s duration=%d", client.lastTo, client.lastDur)
	}
}

func TestSetYouTubeRegistersActionsOnlyAfterAttach(t *testing.T) {
	ytTypes := []string{
		outgress.TypeYouTubeChat, outgress.TypeYouTubeDelete,
		outgress.TypeYouTubeBan, outgress.TypeYouTubeTimeout,
	}

	w := New(Config{Log: zap.NewNop(), Limiter: &fakeManager{}})
	for _, typ := range ytTypes {
		if _, ok := w.actions.Lookup(typ); ok {
			t.Fatalf("%s registered before SetYouTube", typ)
		}
	}

	w.SetYouTube(&recordingClient{}, &fakeYtBudget{}, youtube.NewChatDirectory())
	for _, typ := range ytTypes {
		if _, ok := w.actions.Lookup(typ); !ok {
			t.Fatalf("%s not registered after SetYouTube", typ)
		}
	}
}
