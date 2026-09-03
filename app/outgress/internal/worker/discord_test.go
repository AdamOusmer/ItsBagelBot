// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"ItsBagelBot/pkg/codec"
	"context"
	"errors"
	"sync"
	"testing"

	"ItsBagelBot/app/outgress/internal/discord"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/ratelimit"

	"go.uber.org/zap"
)

// discordRecordingClient captures the call a handler fires and answers with a
// canned reply.
type discordRecordingClient struct {
	mu       sync.Mutex
	calls    int
	lastChan string
	lastText string
	lastTTS  bool
	reply    error
}

func (r *discordRecordingClient) SendMessage(_ context.Context, channelID, content string, tts bool) error {
	r.lastChan, r.lastText, r.lastTTS = channelID, content, tts
	r.calls++
	return r.reply
}

var _ discordAPI = (*discordRecordingClient)(nil)

func newDiscordWorker(client discordAPI, manager ratelimit.Manager) *Worker {
	w := New(Config{Log: zap.NewNop(), Limiter: manager})
	w.SetDiscord(client)
	return w
}

func discordChatPayload(content string) *outgress.Message {
	raw, _ := codec.Marshal(map[string]any{"content": content})
	return &outgress.Message{
		Type:      outgress.TypeDiscordChat,
		ChannelID: "1234567890",
		Payload:   raw,
	}
}

func TestDiscordChatSends(t *testing.T) {
	client := &discordRecordingClient{}
	manager := &fakeManager{}
	w := newDiscordWorker(client, manager)

	if err := w.processDiscordChat(context.Background(), discordChatPayload("Stream is live!")); err != nil {
		t.Fatalf("send failed: %v", err)
	}
	if client.lastText != "Stream is live!" || client.lastChan != "1234567890" {
		t.Fatalf("sent %q to %q", client.lastText, client.lastChan)
	}
	if client.lastTTS {
		t.Fatal("tts should default to false")
	}
}

func TestDiscordChatPaysPerChannelThenGlobalBucket(t *testing.T) {
	client := &discordRecordingClient{}
	manager := &fakeManager{}
	w := newDiscordWorker(client, manager)

	if err := w.processDiscordChat(context.Background(), discordChatPayload("hi")); err != nil {
		t.Fatalf("send failed: %v", err)
	}
	if manager.lastKey != "ratelimit:discord:global" {
		t.Fatalf("last bucket key = %q, want the global ceiling paid last", manager.lastKey)
	}
}

func TestDiscordChatPacingDenialNacksBeforeAPI(t *testing.T) {
	client := &discordRecordingClient{}
	manager := &fakeManager{denied: map[string]bool{"ratelimit:discord:chat:1234567890": true}}
	w := newDiscordWorker(client, manager)

	err := w.processDiscordChat(context.Background(), discordChatPayload("hi"))
	var expected expectedNackError
	if !errors.As(err, &expected) {
		t.Fatalf("err = %v, want an expected nack from the pacing bucket", err)
	}
	if client.calls != 0 {
		t.Fatal("API fired despite pacing denial")
	}
}

func TestDiscordGlobalDenialNacksBeforeAPI(t *testing.T) {
	client := &discordRecordingClient{}
	manager := &fakeManager{denied: map[string]bool{"ratelimit:discord:global": true}}
	w := newDiscordWorker(client, manager)

	err := w.processDiscordChat(context.Background(), discordChatPayload("hi"))
	var expected expectedNackError
	if !errors.As(err, &expected) {
		t.Fatalf("err = %v, want an expected nack from the global bucket", err)
	}
	if client.calls != 0 {
		t.Fatal("API fired despite global denial")
	}
}

func TestDiscordChatMalformedOrOversizedDropsWithoutCall(t *testing.T) {
	client := &discordRecordingClient{}
	w := newDiscordWorker(client, &fakeManager{})

	if err := w.processDiscordChat(context.Background(), &outgress.Message{
		Type: outgress.TypeDiscordChat, ChannelID: "1",
		Payload: []byte(`{not json`),
	}); err != nil {
		t.Fatalf("malformed payload must drop (nil), got %v", err)
	}

	big := make([]byte, discordContentMaxBytes+1)
	for i := range big {
		big[i] = 'a'
	}
	if err := w.processDiscordChat(context.Background(), discordChatPayload(string(big))); err != nil {
		t.Fatalf("oversized content must drop (nil), got %v", err)
	}
	if client.calls != 0 {
		t.Fatal("API fired on invalid payload")
	}
}

func TestDiscordChatWithoutChannelDrops(t *testing.T) {
	client := &discordRecordingClient{}
	w := newDiscordWorker(client, &fakeManager{})
	payload := discordChatPayload("hi")
	payload.ChannelID = ""

	if err := w.processDiscordChat(context.Background(), payload); err != nil {
		t.Fatalf("missing channel must drop (nil), got %v", err)
	}
	if client.calls != 0 {
		t.Fatal("API fired with no target channel")
	}
}

func TestDiscordPermanentErrorsDrop(t *testing.T) {
	for name, reply := range map[string]error{
		"auth":         discord.ErrAuth,
		"forbidden":    discord.ErrForbidden,
		"not found":    discord.ErrChannelNotFound,
		"bad request":  discord.ErrBadRequest,
		"rate limited": discord.ErrRateLimited,
	} {
		t.Run(name, func(t *testing.T) {
			wantDrop := name != "rate limited"
			client := &discordRecordingClient{reply: reply}
			w := newDiscordWorker(client, &fakeManager{})

			err := w.processDiscordChat(context.Background(), discordChatPayload("hi"))
			if wantDrop && err != nil {
				t.Fatalf("permanent error must drop (nil), got %v", err)
			}
			if !wantDrop && err == nil {
				t.Fatal("rate limit must nack for paced redelivery")
			}
		})
	}
}

func TestSetDiscordRegistersActionsOnlyAfterAttach(t *testing.T) {
	w := New(Config{Log: zap.NewNop(), Limiter: &fakeManager{}})
	if _, ok := w.actions.Lookup(outgress.TypeDiscordChat); ok {
		t.Fatal("discord_chat registered before SetDiscord")
	}

	w.SetDiscord(&discordRecordingClient{})
	if _, ok := w.actions.Lookup(outgress.TypeDiscordChat); !ok {
		t.Fatal("discord_chat not registered after SetDiscord")
	}
}
