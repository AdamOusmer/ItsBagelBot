// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"sync"

	"ItsBagelBot/app/twitch/sesame/module"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"go.uber.org/zap"
)

const urlFetchTokenPrefix = "urlfetch:"

const maxUrlFetchTokens = 8

func (p *Pipeline) fetchUrlValues(ctx context.Context, c *module.Context, command string, names []string) map[string]string {
	seg := startStage(ctx, "sesame.urlfetch")
	if seg != nil {
		seg.AddAttribute("command", command)
		seg.AddAttribute("broadcaster_id", c.BroadcasterID)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sink := &urlTokenSink{results: make(map[string]string, len(names)), cancel: cancel}
	for _, name := range names {
		p.launchTokenFetch(ctx, c, name, sink)
	}
	sink.wg.Wait()

	endStage(seg, sink.verdict())
	return sink.results
}

type urlTokenSink struct {
	mu       sync.Mutex
	results  map[string]string
	failures int

	wg     sync.WaitGroup
	cancel context.CancelFunc
}

type tokenOutcome struct {
	name    string
	text    string
	visible bool
	failed  bool
	release func()
}

func (s *urlTokenSink) record(o tokenOutcome) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if o.failed {
		s.failures++
	}
	if o.visible {
		s.results[o.name] = o.text
	}
	if o.release != nil {
		o.release()
	}
}

func (s *urlTokenSink) verdict() string {
	if s.failures > 0 {
		return "fallback"
	}
	return "ok"
}

func (p *Pipeline) launchTokenFetch(ctx context.Context, c *module.Context, name string, sink *urlTokenSink) {
	dup, release := p.claimedUrlValue(ctx, c, name)
	if dup {
		sink.record(tokenOutcome{name: name, visible: true, failed: true})
		return
	}
	sink.wg.Add(1)
	go func() {
		defer sink.wg.Done()
		text, visible, failed := p.resolveUrlToken(ctx, c, name)
		out := tokenOutcome{name: name, text: text, visible: visible, failed: failed}
		if failed {
			out.release = release
			defer sink.cancel()
		}
		sink.record(out)
	}()
}

func (p *Pipeline) claimedUrlValue(ctx context.Context, c *module.Context, name string) (dup bool, release func()) {
	if p.dedup == nil {
		return false, func() {}
	}
	return p.dedup.Claim(ctx, EffectRef{Identity: EventIdentity(&c.Env), Effect: urlFetchTokenPrefix + name})
}

func (p *Pipeline) resolveUrlToken(ctx context.Context, c *module.Context, name string) (text string, visible, failed bool) {
	reply, err := p.customFetch.Fetch(ctx, gossiprpc.Request{
		DefID:     name,
		ChannelID: c.Env.BroadcasterUserID,
		IsPremium: c.Regress.IsPremium(),
	})
	if err != nil {
		p.log.Warn("urlfetch token failed",
			module.BIDField(c.BroadcasterID),
			zap.String("def", name),
			zap.Error(err),
		)
		return "", true, true
	}
	if reply.Status == gossiprpc.FetchBadDef {
		return "", false, true
	}
	if fetchYieldedValue(reply) {
		return ExternalVar(reply.Values[0]), true, false
	}
	return "", true, true
}

func fetchYieldedValue(reply gossiprpc.CustomFetchReply) bool {
	return reply.Status == gossiprpc.FetchOK && len(reply.Values) > 0 && reply.Values[0] != ""
}
