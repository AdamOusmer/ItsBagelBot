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
// siblings re-fire on redelivery. A publish/marshal failure on the emit path is
// an infrastructure error and does nack.
//
// The hot path is allocation-free for plain chat that produces no output: the
// envelope and the module Context are pooled, and the emit sink only allocates
// when a module actually emits an Output.
package pipeline

import (
	"ItsBagelBot/app/worker/module"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"

	"github.com/bytedance/sonic"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"
)

// Pipeline holds the dependencies shared by every message.
type Pipeline struct {
	log      *zap.Logger
	pub      message.Publisher
	proj     projection.Reader
	registry *module.Registry

	// botID is the bot's own Twitch user id; its own chat messages are skipped so
	// the bot never reacts to itself (reply loops, self-bagel, etc.).
	botID string

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
	botID string,
	outgressPremium, outgressStandard string,
) *Pipeline {
	return &Pipeline{
		log:              log,
		pub:              pub,
		proj:             proj,
		registry:         registry,
		botID:            botID,
		outgressPremium:  outgressPremium,
		outgressStandard: outgressStandard,
	}
}

// Process is the handler the weighted consumer hands each message to. It decodes
// the envelope, runs the modules registered for the event type, and publishes
// whatever they emit. It returns nil (ack) once those publishes have gone
// through, and an error (nack) only on an infrastructure failure.
func (p *Pipeline) Process(msg *message.Message) error {
	ctx := msg.Context()

	// Decode the envelope into a pooled *lane.Envelope so the plain-chat path
	// allocates nothing here.
	env := module.GetEnvelope()
	defer module.PutEnvelope(env)
	if err := sonic.Unmarshal(msg.Payload, env); err != nil {
		// A malformed envelope is poison: redelivering it forever helps no one,
		// so drop it (ack) and move on.
		p.log.Warn("dropping malformed envelope", zap.String("message_id", msg.UUID), zap.Error(err))
		if txn := newrelic.FromContext(ctx); txn != nil {
			txn.NoticeError(err)
		}
		return nil
	}

	// The bot sees its own chat sends via EventSub; never react to them.
	if p.botID != "" && env.Type == "channel.chat.message" && env.ChatterUserID == p.botID {
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

	if txn := newrelic.FromContext(ctx); txn != nil {
		txn.AddAttribute("event.type", env.Type)
		txn.AddAttribute("event.broadcaster_id", broadcasterID)
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

	mctx := module.GetContext()
	defer module.PutContext(mctx)
	mctx.Env = *env
	mctx.Regress = regress
	mctx.BroadcasterID = broadcasterID
	mctx.Log = p.log

	// emit is the sink each module hands its Outputs to. It marshals and
	// publishes inline onto the lane matching the regress status, so a premium
	// broadcaster's reply rides the premium lane end to end. The first publish or
	// marshal failure is captured and short-circuits the rest: an infrastructure
	// error must nack so the event is redelivered, and once one publish has
	// failed there is no point attempting the siblings.
	var emitErr error
	emit := func(o *module.Output) {
		if emitErr != nil || o == nil || o.Type == "" {
			return
		}
		subject := p.outgressStandard
		if regress.IsPremium() {
			subject = p.outgressPremium
		}
		body, err := buildOutgress(o)
		if err != nil {
			emitErr = err
			return
		}
		if err := bus.PublishRaw(ctx, p.pub, subject, body); err != nil {
			emitErr = err
		}
	}

	for _, m := range mods {
		if !p.enabled(m, views, mctx) {
			continue
		}
		if err := m.Handle(ctx, mctx, emit); err != nil {
			// Logic error: log and skip this module, do not nack (would re-fire
			// the siblings that already succeeded on redelivery).
			p.log.Error("module failed",
				zap.String("module", moduleName(m)),
				zap.String("type", env.Type),
				zap.Uint64("broadcaster_id", broadcasterID),
				zap.Error(err))
			if txn := newrelic.FromContext(ctx); txn != nil {
				txn.AddAttribute("module.failed", moduleName(m))
				txn.NoticeError(err)
			}
			continue
		}
	}

	// nil = ack; a publish/marshal failure on the emit path = nack.
	return emitErr
}

// buildOutgress translates a module Output into the marshaled bytes of the full
// outgress.Message wire contract. The inner Payload is built from a small typed
// struct rather than a map so sonic escapes emoji and quotes in the body
// correctly. This runs only when a module actually emits, so the allocation it
// costs never touches the no-output plain-chat path.
func buildOutgress(o *module.Output) ([]byte, error) {
	var msg outgress.Message

	switch o.Type {
	case outgress.TypeChat:
		payload, err := sonic.Marshal(struct {
			BroadcasterID string `json:"broadcaster_id"`
			Message       string `json:"message"`
		}{o.BroadcasterID, o.Text})
		if err != nil {
			return nil, err
		}
		msg = outgress.Message{
			Type:          outgress.TypeChat,
			BroadcasterID: o.BroadcasterID,
			Payload:       payload,
		}
	case outgress.TypeAnnounce:
		payload, err := sonic.Marshal(struct {
			Message string `json:"message"`
		}{o.Text})
		if err != nil {
			return nil, err
		}
		msg = outgress.Message{
			Type:          outgress.TypeAnnounce,
			BroadcasterID: o.BroadcasterID,
			Color:         o.Color,
			Payload:       payload,
		}
	case outgress.TypeShoutout:
		msg = outgress.Message{
			Type:          outgress.TypeShoutout,
			BroadcasterID: o.BroadcasterID,
			To:            o.To,
			Payload:       []byte("{}"),
		}
	default:
		msg = outgress.Message{
			Type:          o.Type,
			BroadcasterID: o.BroadcasterID,
		}
	}

	return sonic.Marshal(msg)
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
