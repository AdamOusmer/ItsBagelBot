// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"sync"
	"testing"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"go.uber.org/zap"
)

const (
	premiumSubj  = "outgress.premium"
	standardSubj = "outgress.standard"
)

type captured struct {
	subject string
	id      string
	msg     outgress.Message
}

type fakePublisher struct {
	mu      sync.Mutex
	got     []captured
	failErr error
}

func (p *fakePublisher) PublishOwned(_ context.Context, subject string, payload []byte) error {
	return p.PublishOwnedWithID(context.Background(), subject, "", payload)
}

func (p *fakePublisher) PublishOwnedWithID(_ context.Context, subject, id string, payload []byte) error {
	if p.failErr != nil {
		return p.failErr
	}
	var om outgress.Message
	_ = codec.Unmarshal(payload, &om)
	p.mu.Lock()
	p.got = append(p.got, captured{subject: subject, id: id, msg: om})
	p.mu.Unlock()
	return nil
}

func (p *fakePublisher) snapshot() []captured {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]captured(nil), p.got...)
}

func (p *fakePublisher) Flush(context.Context) error { return nil }

func (p *fakePublisher) Close() error { return nil }

func (p *fakePublisher) types() map[string]int {
	var counts map[string]int
	for _, c := range p.snapshot() {
		if counts == nil {
			counts = map[string]int{}
		}
		counts[c.msg.Type]++
	}
	return counts
}

func (p *fakePublisher) chatTexts(t *testing.T) []string {
	t.Helper()
	var texts []string
	for _, c := range p.snapshot() {
		if c.msg.Type == outgress.TypeChat {
			texts = append(texts, chatMessageText(t, c.msg))
		}
	}
	return texts
}

func newPipelineWith(pub bus.Publisher, reader projection.Reader, mods ...module.Module) *Pipeline {
	reg := NewRegistry(zap.NewNop(), mods...)
	d := Deps{Proj: reader, Live: liveAlways{}, Cooldown: NoopCooldown{}, Pub: pub, Log: zap.NewNop()}
	return NewPipeline(d, reg, Config{OutgressPremium: premiumSubj, OutgressStandard: standardSubj})
}
