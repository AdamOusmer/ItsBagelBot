// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
package engine

import (
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/projection"
	"context"
	"github.com/newrelic/go-agent/v3/newrelic"
)

func traceEnvelope(ctx context.Context, env *lane.Envelope, id uint64) {
	if txn := newrelic.FromContext(ctx); txn != nil {
		txn.AddAttribute("event.type", env.Type)
		txn.AddAttribute("event.lane", env.Lane)
		txn.AddAttribute("event.broadcaster_id", id)
		txn.AddAttribute("event.origin", env.Origin)
		if env.Origin == "trial" {
			txn.AddAttribute("event.trial_generation", env.TrialGeneration)
		}
	}
}
func (p *Pipeline) ensureLocale(ctx context.Context, c *module.Context) { c.EnsureLocale(ctx) }
func (p *Pipeline) readLocale(ctx context.Context, id uint64) (string, error) {
	u, err := p.proj.User(ctx, id)
	return u.Locale, err
}

// Keep older Reader implementations and fakes compatible.
type channelLoader interface {
	LoadChannel(context.Context, uint64, bool) (map[string]projection.ModuleView, projection.User, error)
}

func (p *Pipeline) loadMessageProjections(ctx context.Context, c *module.Context) (map[string]projection.ModuleView, error) {
	loader, ok := p.proj.(channelLoader)
	if !ok || !p.needsMessageLocale(c) {
		return p.tracedModuleViews(ctx, c.Env.Type, c.BroadcasterID)
	}
	segment := startStage(ctx, "sesame.dependency.projection")
	views, u, err := loader.LoadChannel(ctx, c.BroadcasterID, p.registry.NeedsModuleViews(c.Env.Type))
	endStageForError(segment, err, "error")
	if err == nil {
		c.Locale = u.Locale
		c.LocaleLoaded = true
	}
	return views, err
}
func (p *Pipeline) needsMessageLocale(c *module.Context) bool {
	if c.Env.Type != chatType {
		return len(p.registry.For(c.Env.Type)) > 0
	}
	if len(c.Env.Senders) > 0 {
		return false
	}
	name, _, ok := parseCommand(c.Env.Text)
	if !ok {
		return false
	}
	bc, _, baked := p.registry.ResolveCommand(name)
	return baked && !bc.Owner.Trial && permits(c, bc.Cmd.AllowedUserID, bc.Cmd.Perm) && (!bc.Owner.Beta || c.Regress.IsPremium())
}

// Resolve at emit time, after a baked command or a lazy chat handler has loaded
// locale. Capturing the empty Context locale before stages loses that value.
func (s *emitState) outputLocale(ctx context.Context) string {
	if s.mctx == nil {
		return s.locale
	}
	s.mctx.EnsureLocale(ctx)
	return s.mctx.Locale
}

// Activity observers localize handled-command events even when dispatch was
// gated or found no active command. Ordinary silent chat has no such output.
func (p *Pipeline) observerLocale(ctx context.Context, c *module.Context) string {
	if c.Command != "" && len(p.observers) > 0 {
		c.EnsureLocale(ctx)
	}
	return c.Locale
}

func (p *Pipeline) ensureHandlerLocale(ctx context.Context, c *module.Context) {
	if c.Env.Type != chatType {
		c.EnsureLocale(ctx)
	}
}
