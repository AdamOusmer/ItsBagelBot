package engine

import (
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/bytedance/sonic"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"
)

// Config carries the per-service knobs the pipeline needs beyond its Deps: the
// bot's own id (to skip its own chat), the two outgress lane subjects, and
// whether to run the command-use counter.
type Config struct {
	BotID            string
	OutgressPremium  string
	OutgressStandard string
	// CountUses starts the command-use reporter (a background flusher). Off in
	// tests so they leak no goroutine and publish no counter events.
	CountUses bool
}

// Pipeline is the per-message stage the consumer hands each decoded message to.
// It is the single point every message flows through once the consumer has
// handed it off: decode, dispatch a command if the line is one, run the event
// handlers registered for the type, and publish what they emit. Command dispatch
// is folded in here (it reads the registry's command index directly), so there
// is no separate router module and no Bind step.
//
// Ack discipline mirrors the worker: Process returns nil (ack) once the emitted
// requests are published; an infrastructure failure before publishing (a
// ModuleView read) returns an error (nack) for redelivery; a single handler's
// logic error, or a command gate's store error, is logged and skipped, never
// nacked, so one misbehaving path cannot re-fire its siblings; a publish/marshal
// failure on the emit path does nack.
//
// The hot path is allocation-free above the JSON decoder floor for a plain chat
// line that emits nothing: the envelope and the module Context are pooled, and
// the emit sink only builds an outgress message when a handler actually emits.
type Pipeline struct {
	log      *zap.Logger
	pub      message.Publisher
	proj     projection.Reader
	registry *Registry

	live     IsLiveChecker
	cooldown CooldownStore
	uses     *useReporter

	botID            string
	outgressPremium  string
	outgressStandard string
}

// NewPipeline wires a Pipeline from the shared Deps, a pre-built registry, and
// the per-service Config. It pulls its projection reader, live checker, cooldown
// store, publisher and logger from d, so main constructs those once.
func NewPipeline(d Deps, registry *Registry, cfg Config) *Pipeline {
	p := &Pipeline{
		log:              d.Log,
		pub:              d.Pub,
		proj:             d.Proj,
		registry:         registry,
		live:             d.Live,
		cooldown:         d.Cooldown,
		botID:            cfg.BotID,
		outgressPremium:  cfg.OutgressPremium,
		outgressStandard: cfg.OutgressStandard,
	}
	if cfg.CountUses && d.Pub != nil {
		p.uses = newUseReporter(d.Pub, d.Log)
	}
	return p
}

// Close flushes and stops the command-use reporter. Safe when it was never
// started (CountUses false).
func (p *Pipeline) Close() {
	if p.uses != nil {
		p.uses.Close()
	}
}

// chatType is the one EventSub type that carries command dispatch.
const chatType = "channel.chat.message"

// Process decodes one message, dispatches a command when the line is one, runs
// the event handlers registered for the type, and publishes what they emit.
func (p *Pipeline) Process(msg *message.Message) error {
	ctx := msg.Context()

	// Decode into a pooled envelope so the plain-chat path allocates nothing here.
	env := GetEnvelope()
	defer PutEnvelope(env)
	if err := sonic.Unmarshal(msg.Payload, env); err != nil {
		// A malformed envelope is poison: redelivering it forever helps no one, so
		// drop it (ack) and move on.
		p.log.Warn("dropping malformed envelope", zap.String("message_id", msg.UUID), zap.Error(err))
		if txn := newrelic.FromContext(ctx); txn != nil {
			txn.NoticeError(err)
		}
		return nil
	}

	isChat := env.Type == chatType

	// The bot sees its own chat sends via EventSub; never react to them.
	if p.botID != "" && isChat && env.ChatterUserID == p.botID {
		return nil
	}

	// Event handlers registered for this type. Chat always runs (command dispatch
	// is engine-internal, not a registered handler), so only non-chat types with
	// no handler can bail out here with no work.
	handlers := p.registry.For(env.Type)
	if !isChat && len(handlers) == 0 {
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
	// name-gated handler is registered for this type. Plain chat hits only core
	// handlers, so this read is skipped entirely.
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

	mctx := GetContext()
	defer PutContext(mctx)
	mctx.Env = *env
	mctx.Regress = regress
	mctx.BroadcasterID = broadcasterID
	mctx.Log = p.log

	// emit is the sink command Run and event handlers hand their Outputs to. It
	// builds and publishes inline onto the lane matching the regress status. The
	// first publish or marshal failure is captured and short-circuits the rest:
	// an infrastructure error must nack, and once one publish has failed there is
	// no point attempting the siblings.
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

	// Command dispatch stage: chat only, always on. A gate store error is logged
	// and skipped like a handler error, never nacked.
	if isChat {
		if err := p.dispatchCommand(ctx, mctx, views, emit); err != nil {
			p.log.Error("command dispatch failed",
				zap.Uint64("broadcaster_id", broadcasterID), zap.Error(err))
			if txn := newrelic.FromContext(ctx); txn != nil {
				txn.NoticeError(err)
			}
		}
	}

	// Event handlers (the non-command path).
	for _, m := range handlers {
		if !p.enabled(m, views, mctx) {
			continue
		}
		handle := m.Events[env.Type]
		if handle == nil {
			continue
		}
		if err := handle(ctx, mctx, emit); err != nil {
			// Logic error: log and skip this module, do not nack (would re-fire the
			// siblings that already succeeded on redelivery).
			p.log.Error("module handler failed",
				zap.String("module", moduleLabel(m)),
				zap.String("type", env.Type),
				zap.Uint64("broadcaster_id", broadcasterID),
				zap.Error(err))
			if txn := newrelic.FromContext(ctx); txn != nil {
				txn.AddAttribute("module.failed", moduleLabel(m))
				txn.NoticeError(err)
			}
			continue
		}
	}

	// nil = ack; a publish/marshal failure on the emit path = nack.
	return emitErr
}

// enabled applies the per-module enable gate and wires the module's config into
// the context: a core module is always on; a KindDefault module runs unless its
// ModuleView disables it; a KindOptIn module runs only when its ModuleView
// enables it. There is no premium gate: premium vs standard is a routing lane
// (see emit), not a feature switch, so every module is available on both.
func (p *Pipeline) enabled(m module.Module, views map[string]projection.ModuleView, mctx *module.Context) bool {
	switch m.Kind {
	case module.KindCore:
		mctx.Config = nil
		return true
	case module.KindDefault:
		if mv, ok := views[m.Name]; ok {
			if !mv.IsEnabled {
				return false
			}
			mctx.Config = mv.Configs
		} else {
			mctx.Config = nil // ships enabled: no row means on, no config
		}
		return true
	case module.KindOptIn:
		mv, ok := views[m.Name]
		if !ok || !mv.IsEnabled {
			return false
		}
		mctx.Config = mv.Configs
		return true
	default:
		return false
	}
}

// buildOutgress translates a module Output into the marshaled bytes of the full
// outgress.Message wire contract. The inner Payload is built from a small typed
// struct rather than a map so sonic escapes emoji and quotes in the body
// correctly. This runs only when a handler actually emits, so the allocation it
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
