// Package pipeline is the worker's processing stage. It is not the consumer:
// the weighted consumer (bus.ConsumeWeighted) owns the subscription and the
// goroutine pool, pulls up to N messages in flight, and hands each one to
// Pipeline.Process. The pipeline is the per-message work — decode, route to the
// modules registered for the event type, run them, publish what they produced —
// and is the single starting point every message flows through once the consumer
// has handed it off. The behavior lives in pluggable modules (app/worker/module);
// the pipeline only owns the registry and the per-message orchestration, in the
// consumer's own goroutine.
//
// Ack discipline: Process returns nil only after the resulting requests have
// been published to outgress (the JetStream publish is synchronous, so a nil
// return means the broker has them). Infrastructure failures before that
// (projection/Valkey/RPC) return an error, which the consumer turns into a nack
// so the event is redelivered. A single module's logic error is logged and that
// module is skipped, never nacked, so one misbehaving module cannot make its
// siblings re-fire on redelivery.
package pipeline

import (
	"context"
	"encoding/json"

	"ItsBagelBot/app/worker/module"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"

	"github.com/ThreeDotsLabs/watermill/message"
	"go.uber.org/zap"
)

// Pipeline holds the dependencies shared by every message.
type Pipeline struct {
	log      *zap.Logger
	pub      message.Publisher
	proj     projection.Reader
	registry *module.Registry

	outgressPremium  string
	outgressStandard string
}

// NewPipeline constructs a Pipeline around a pre-built module registry. The
// caller (main) owns module construction and registration so the pipeline stays
// decoupled from concrete modules.
func NewPipeline(
	log *zap.Logger,
	pub message.Publisher,
	proj projection.Reader,
	registry *module.Registry,
	outgressPremium, outgressStandard string,
) *Pipeline {
	return &Pipeline{
		log:              log,
		pub:              pub,
		proj:             proj,
		registry:         registry,
		outgressPremium:  outgressPremium,
		outgressStandard: outgressStandard,
	}
}

// Process is the handler the weighted consumer hands each message to. It decodes
// the envelope, runs the modules registered for the event type, and publishes
// whatever they produced. It returns nil (ack) once those publishes have gone
// through, and an error (nack) only on an infrastructure failure.
func (p *Pipeline) Process(msg *message.Message) error {
	ctx := msg.Context()

	var env lane.Envelope
	if err := json.Unmarshal(msg.Payload, &env); err != nil {
		// A malformed envelope is poison: redelivering it forever helps no one,
		// so drop it (ack) and move on.
		p.log.Warn("dropping malformed envelope", zap.String("message_id", msg.UUID), zap.Error(err))
		return nil
	}

	mods := p.registry.For(env.Type)
	if len(mods) == 0 {
		// No module cares about this type: ack and ignore, no I/O.
		return nil
	}

	broadcasterID, ok := env.BroadcasterID()
	if !ok {
		return nil
	}

	regress := module.RegressFromLane(env.Lane)

	// Resolve the broadcaster's module toggles + configs once, and only when a
	// name-gated module is registered for this type. Plain chat hits only core
	// modules, so this read is skipped entirely.
	var views map[string]projection.ModuleView
	if p.registry.NeedsModuleViews(env.Type) {
		list, err := p.proj.Modules(ctx, broadcasterID)
		if err != nil {
			return err // infrastructure failure: nack
		}
		views = make(map[string]projection.ModuleView, len(list))
		for _, v := range list {
			views[v.Name] = v
		}
	}

	mctx := &module.Context{
		Env:           env,
		Regress:       regress,
		BroadcasterID: broadcasterID,
		Log:           p.log,
	}

	var out []*outgress.Message
	for _, m := range mods {
		if !p.enabled(m, views, mctx) {
			continue
		}
		res, err := m.Handle(ctx, mctx)
		if err != nil {
			// Logic error: log and skip this module, do not nack (would re-fire
			// the siblings that already succeeded on redelivery).
			p.log.Error("module failed",
				zap.String("module", moduleName(m)),
				zap.String("type", env.Type),
				zap.Uint64("broadcaster_id", broadcasterID),
				zap.Error(err))
			continue
		}
		out = append(out, res...)
	}

	for _, m := range out {
		if m == nil {
			continue
		}
		if err := p.emit(ctx, regress, m); err != nil {
			return err // publish failure: nack so the event is redelivered
		}
	}
	return nil
}

// enabled applies the generic per-module gates and wires the module's config into
// the context: a core module (no name) is always on; a named module is gated by
// its ModuleView (or its Defaulted state when no row exists); a PremiumOnly
// module runs only on the premium regress.
func (p *Pipeline) enabled(m module.Module, views map[string]projection.ModuleView, mctx *module.Context) bool {
	if name := m.Name(); name != "" {
		mv, ok := views[name]
		if ok {
			if !mv.IsEnabled {
				return false
			}
			mctx.Config = mv.Configs
		} else {
			if d, isDefaulted := m.(module.Defaulted); !isDefaulted || !d.DefaultEnabled() {
				return false
			}
			mctx.Config = nil
		}
	} else {
		mctx.Config = nil
	}

	if pm, ok := m.(module.PremiumOnly); ok && pm.PremiumOnly() && !mctx.Regress.IsPremium() {
		return false
	}
	return true
}

func moduleName(m module.Module) string {
	if n := m.Name(); n != "" {
		return n
	}
	return "core"
}

// emit publishes an outgress message onto the lane matching the regress status,
// so a premium broadcaster's reply rides the premium lane end to end.
// Stream-lane traffic that produces an action is treated as standard. The
// JetStream publish is synchronous, so a nil return means outgress has it.
func (p *Pipeline) emit(ctx context.Context, regress module.Regress, out *outgress.Message) error {
	subject := p.outgressStandard
	if regress.IsPremium() {
		subject = p.outgressPremium
	}
	return bus.PublishJSON(ctx, p.pub, subject, out)
}
