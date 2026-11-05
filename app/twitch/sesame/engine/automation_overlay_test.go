// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/automation"
	automationrpc "ItsBagelBot/internal/domain/rpc/automation"
	automationsrpc "ItsBagelBot/internal/domain/rpc/automations"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
)

type overlayCall struct{ channel, payload string }

type fakeOverlay struct {
	mu        sync.Mutex
	calls     []overlayCall
	receivers int64
	err       error
}

func (f *fakeOverlay) PublishOverlay(_ context.Context, channel, payload string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return 0, f.err
	}
	f.calls = append(f.calls, overlayCall{channel, payload})
	return f.receivers, nil
}

func overlayFixture(sink OverlayPublisher, autos ...automation.Automation) *autoFixture {
	fx := newAutoFixture(autos...)
	fx.p.automations.Overlay = sink
	return fx
}

func showText(content string) automation.Action {
	return act("overlay_show", obj{"kind": "text", "content": content, "seconds": 5, "position": "top"})
}

func decodeItem(t *testing.T, payload string) automation.OverlayItem {
	t.Helper()
	var item automation.OverlayItem
	require.NoError(t, codec.UnmarshalFromString(payload, &item))
	return item
}

func TestOverlayShowPublishesRenderedContentOnTheRunsOwnChannel(t *testing.T) {
	sink := &fakeOverlay{receivers: 2}
	fx := overlayFixture(sink, manual("main", showText("hi {arg:who}")))
	rec := fx.run(t, "main", "req-show", map[string]string{"who": "<b>you</b>"})
	assert.Equal(t, []string{"0:overlay_show:ok:"}, statuses(rec))
	require.Len(t, sink.calls, 1)
	assert.Equal(t, "overlay:1", sink.calls[0].channel)
	assert.Equal(t, automation.OverlayItem{
		Version: 1, Kind: "text", Content: "hi <b>you</b>", Seconds: 5, Position: "top",
	}, decodeItem(t, sink.calls[0].payload))
}

func TestOverlayTemplatesCannotPickAnotherChannel(t *testing.T) {
	sink := &fakeOverlay{receivers: 1}
	fx := overlayFixture(sink, manual("main", showText("overlay:2 {arg:who}")))
	reply := fx.p.RunNamed(context.Background(), automationsrpc.RunRequest{
		BroadcasterID: "1", Name: "main", Args: map[string]string{"who": "overlay:2"}, RequestID: "req-tenant", Source: automationsrpc.SourceDashboard,
	})
	require.True(t, reply.Accepted, reply.Refusal)
	fx.awaitRecord(t, reply.RunID)
	require.Len(t, sink.calls, 1)
	assert.Equal(t, "overlay:1", sink.calls[0].channel)
}

func TestOverlayNoClientSucceedsWithDetailAndCanRetry(t *testing.T) {
	sink := &fakeOverlay{}
	fx := overlayFixture(sink, manual("main", showText("hi")))
	rec := fx.run(t, "main", "req-none", nil)
	assert.Equal(t, []string{"0:overlay_show:ok:no_overlay"}, statuses(rec))
	assert.Equal(t, automationrpc.OutcomeOK, rec.Outcome)

	sink.mu.Lock()
	sink.receivers = 1
	sink.mu.Unlock()
	retried := fx.run(t, "main", "req-none", nil)
	assert.Equal(t, []string{"0:overlay_show:ok:"}, statuses(retried))
}

func TestOverlayRefusesUnsafeRenderedURLs(t *testing.T) {
	cases := []string{"http://evil.example/a.png", "javascript:alert(1)", "data:text/html,x", "", "https://user:pw@evil.example/a"}
	for _, bad := range cases {
		t.Run(bad, func(t *testing.T) {
			sink := &fakeOverlay{receivers: 1}
			fx := overlayFixture(sink, manual("main",
				act("overlay_show", obj{"kind": "image", "content": "{arg:u}", "seconds": 5, "position": "top"}),
				act("overlay_show", obj{"kind": "video", "content": "{arg:u}", "seconds": 5, "position": "top"}),
				act("overlay_sound", obj{"url": "{arg:u}", "volume": 50}),
			))
			rec := fx.run(t, "main", "req-bad", map[string]string{"u": bad})
			assert.Equal(t, []string{
				"0:overlay_show:failed:bad_url", "1:overlay_show:failed:bad_url", "2:overlay_sound:failed:bad_url",
			}, statuses(rec))
			assert.Empty(t, sink.calls)
		})
	}
}

func TestOverlaySoundCarriesVolumeIncludingZero(t *testing.T) {
	sink := &fakeOverlay{receivers: 1}
	fx := overlayFixture(sink, manual("main", act("overlay_sound", obj{"url": "https://cdn.example/a.mp3", "volume": 0})))
	fx.run(t, "main", "req-sound", nil)
	require.Len(t, sink.calls, 1)
	assert.Contains(t, sink.calls[0].payload, `"volume":0`)
}

func TestOverlayTextIsTruncatedAndEmptyIsSkipped(t *testing.T) {
	sink := &fakeOverlay{receivers: 1}
	fx := overlayFixture(sink, manual("main", showText("{arg:long}"), showText("{arg:none}")))
	rec := fx.run(t, "main", "req-long", map[string]string{"long": strings.Repeat("a", 2000)})
	assert.Equal(t, []string{"0:overlay_show:ok:", "1:overlay_show:skipped:empty"}, statuses(rec))
	require.Len(t, sink.calls, 1)
	assert.Len(t, decodeItem(t, sink.calls[0].payload).Content, automation.MaxOverlayTextRunes)
}

func TestOverlayRedeliveryDoesNotShowTwice(t *testing.T) {
	sink := &fakeOverlay{receivers: 1}
	fx := overlayFixture(sink, manual("main", showText("hi")))
	fx.run(t, "main", "req-dup", nil)
	second := fx.run(t, "main", "req-dup", nil)
	assert.Equal(t, []string{"0:overlay_show:skipped:duplicate"}, statuses(second))
	assert.Len(t, sink.calls, 1)
}

func TestOverlayFailuresAreReported(t *testing.T) {
	broken := overlayFixture(&fakeOverlay{err: errors.New("NOPERM")}, manual("main", showText("hi")))
	assert.Equal(t, []string{"0:overlay_show:failed:overlay_unavailable"}, statuses(broken.run(t, "main", "req-err", nil)))

	unwired := newAutoFixture(manual("main", showText("hi")))
	assert.Equal(t, []string{"0:overlay_show:failed:no_overlay_path"}, statuses(unwired.run(t, "main", "req-nil", nil)))
}

func TestValkeyOverlayCountsLiveSubscribers(t *testing.T) {
	addr := localValkeyAddr(t)
	client, err := valkey.NewClient(valkey.ClientOption{InitAddress: []string{addr}, DisableCache: true})
	require.NoError(t, err)
	t.Cleanup(client.Close)
	overlay := NewValkeyOverlay(client)
	ctx := context.Background()

	got, err := overlay.PublishOverlay(ctx, "overlay:1", "{}")
	require.NoError(t, err)
	assert.Zero(t, got)

	received := make(chan string, 16)
	subCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		_ = client.Receive(subCtx, client.B().Subscribe().Channel("overlay:1").Build(), func(m valkey.PubSubMessage) {
			received <- m.Channel + "=" + m.Message
		})
	}()
	require.Eventually(t, func() bool {
		own, err := overlay.PublishOverlay(ctx, "overlay:1", `{"v":1}`)
		return err == nil && own == 1
	}, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, `overlay:1={"v":1}`, <-received)

	other, err := overlay.PublishOverlay(ctx, "overlay:2", "{}")
	require.NoError(t, err)
	assert.Zero(t, other)
}
