// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"strings"

	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/pkg/codec"

	"github.com/valkey-io/valkey-go"
)

const detailNoOverlay = "no_overlay"

// OverlayPublisher reports how many dashboard pods hold a live overlay stream for the channel.
type OverlayPublisher interface {
	PublishOverlay(ctx context.Context, channel, payload string) (receivers int64, err error)
}

type ValkeyOverlay struct{ client valkey.Client }

func NewValkeyOverlay(client valkey.Client) *ValkeyOverlay { return &ValkeyOverlay{client: client} }

// Goes to the primary: PUBLISH counts subscribers of the node it lands on, and dashboards subscribe there.
func (o *ValkeyOverlay) PublishOverlay(ctx context.Context, channel, payload string) (int64, error) {
	return o.client.Do(ctx, o.client.B().Publish().Channel(channel).Message(payload).Build()).AsInt64()
}

func (x *actionExec) overlayShow(s step, p automation.OverlayShowAction) error {
	content := strings.TrimSpace(x.expand(p.Content))
	if automation.OverlayKindIsMedia(p.Kind) {
		return x.overlayMedia(s, automation.OverlayItem{
			Kind: p.Kind, Content: content, Seconds: p.Seconds, Position: p.Position,
		})
	}
	if content == "" {
		return skipped("empty")
	}
	return x.publishOverlay(s, automation.OverlayItem{
		Kind: p.Kind, Content: truncateRunes(content, automation.MaxOverlayTextRunes), Seconds: p.Seconds, Position: p.Position,
	})
}

func (x *actionExec) overlaySound(s step, p automation.OverlaySoundAction) error {
	volume := p.Volume
	return x.overlayMedia(s, automation.OverlayItem{
		Kind: automation.OverlayKindSound, Content: strings.TrimSpace(x.expand(p.URL)), Volume: &volume,
	})
}

func (x *actionExec) overlayMedia(s step, item automation.OverlayItem) error {
	if !automation.OverlayMediaURL(item.Content) {
		return failure("bad_url")
	}
	return x.publishOverlay(s, item)
}

func (x *actionExec) publishOverlay(s step, item automation.OverlayItem) error {
	sink := x.p.automations.Overlay
	if sink == nil {
		return failure("no_overlay_path")
	}
	item.Version = automation.OverlayWireVersion
	payload, err := codec.MarshalToString(item)
	if err != nil {
		return failure("bad_item")
	}
	channel := automation.OverlayChannel(strconv.FormatUint(x.run.BroadcasterID, 10))
	return x.once(s, func() error {
		receivers, err := sink.PublishOverlay(x.ctx, channel, payload)
		if err != nil {
			return failure("overlay_unavailable")
		}
		if receivers == 0 {
			return succeeded(detailNoOverlay)
		}
		return nil
	})
}
