package engine

import (
	"context"
	"fmt"

	"ItsBagelBot/app/sesame/automod"
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/moderation"
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

	// AutomodEnforce arms the automod gate: false shadow-logs verdicts, true
	// emits the ban/timeout action and skips dispatch for an actioned line.
	AutomodEnforce bool

	// ShieldEnabled lets a confirmed mass-raid escalate to channel-level Shield
	// Mode (one PUT) instead of per-account bans. It is a separate, stricter gate
	// than AutomodEnforce because Shield Mode is broadcaster-visible and aggressive;
	// it only takes effect when AutomodEnforce is also on. Off by default.
	ShieldEnabled bool
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
	pub      bus.Publisher
	proj     projection.Reader
	registry *Registry

	live     IsLiveChecker
	cooldown CooldownStore
	dedup    DedupStore
	uses     *useReporter
	loyalty  LoyaltyStore

	botID            string
	outgressPremium  string
	outgressStandard string

	automod        *automod.Gate
	automodEnforce bool
	reputation     Reputation
	campaign       Campaign

	// shieldEnabled gates the mass-raid Shield Mode escalation; raidGate dedups it
	// per channel so one raid activates Shield Mode once, not on every folded burst.
	shieldEnabled bool
	raidGate      *raidCooldown
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
		dedup:            d.Dedup,
		loyalty:          d.Loyalty,
		botID:            cfg.BotID,
		outgressPremium:  cfg.OutgressPremium,
		outgressStandard: cfg.OutgressStandard,
		automod:          d.Automod,
		automodEnforce:   cfg.AutomodEnforce,
		reputation:       d.Reputation,
		campaign:         d.Campaign,
		shieldEnabled:    cfg.ShieldEnabled,
		raidGate:         newRaidCooldown(raidCooldownTTL),
	}
	if p.dedup == nil {
		p.dedup = NoopDedup{}
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
// the event handlers registered for the type, and publishes what they emit. It
// reads as a short sequence of guards and stages; the loops and the failure
// bookkeeping live in the helpers below.
func (p *Pipeline) Process(msg *message.Message) (err error) {
	ctx := msg.Context()

	// Decode into a pooled envelope so the plain-chat path allocates nothing here.
	env := GetEnvelope()
	defer PutEnvelope(env)
	if err := sonic.Unmarshal(msg.Payload, env); err != nil {
		return p.dropPoison(ctx, msg.UUID, err)
	}
	if !p.eligible(env) {
		return nil
	}

	broadcasterID, ok := env.BroadcasterID()
	if !ok {
		return nil
	}
	traceEvent(ctx, env.Type, broadcasterID)

	release, proceed := p.claimDedup(ctx, env, broadcasterID)
	if !proceed {
		return nil
	}
	if release != nil {
		defer func() { release(err) }()
	}

	views, err := p.moduleViews(ctx, env.Type, broadcasterID)
	if err != nil {
		return err // infrastructure failure: nack
	}

	mctx := p.leaseContext(env, broadcasterID)
	defer PutContext(mctx)

	var emitErr error
	emit := p.newEmit(ctx, p.laneSubject(mctx.Regress), &emitErr)
	p.runStages(ctx, mctx, views, emit)

	// nil = ack; a publish/marshal failure on the emit path = nack.
	return emitErr
}

// eligible reports whether this envelope needs any work: the bot's own chat is
// never reacted to, and chat always runs (command dispatch is engine-internal,
// not a registered handler), so only a non-chat type with no handler bails out.
func (p *Pipeline) eligible(env *lane.Envelope) bool {
	isChat := env.Type == chatType
	if p.isOwnChat(env, isChat) {
		return false
	}
	return isChat || len(p.registry.For(env.Type)) > 0
}

// leaseContext populates a pooled module Context for one envelope; the caller
// returns it with PutContext.
func (p *Pipeline) leaseContext(env *lane.Envelope, broadcasterID uint64) *module.Context {
	mctx := GetContext()
	mctx.Env = *env
	mctx.Regress = module.RegressFromLane(env.Lane)
	mctx.BroadcasterID = broadcasterID
	mctx.Log = p.log
	return mctx
}

// newEmit builds the sink command Run and event handlers hand their Outputs
// to. The first publish or marshal failure is captured in *emitErr (which
// nacks) and short-circuits the rest. The sink only builds an outgress message
// when a handler actually emits, so the no-output hot path stays free.
func (p *Pipeline) newEmit(ctx context.Context, subject string, emitErr *error) module.Emit {
	return func(o *module.Output) {
		if *emitErr != nil {
			return
		}
		if o == nil || o.Type == "" {
			return
		}
		if p.floorSuppressed(o) {
			return
		}
		if perr := p.publishOutput(ctx, subject, o); perr != nil {
			*emitErr = perr
		}
	}
}

// runStages executes the moderation gate, command dispatch and the event
// handlers under the shared skip rules: an actioned line (the chatter is being
// moderated) skips dispatch and handlers, and a folded duplicate cohort
// (Senders present) never dispatches a command.
func (p *Pipeline) runStages(ctx context.Context, mctx *module.Context, views map[string]projection.ModuleView, emit module.Emit) {
	env := &mctx.Env
	soloChat := env.Type == chatType && len(env.Senders) == 0

	actioned := p.moderateChat(ctx, mctx, views, emit)
	if soloChat && !actioned {
		p.dispatch(ctx, mctx, views, emit)
	}
	if len(p.registry.For(env.Type)) > 0 && !actioned {
		// Event handlers can emit localized system text too (for example the
		// stream-online bagel announcement). Command dispatch resolves locale for
		// baked commands, but non-command events never pass through that path.
		p.ensureLocale(ctx, mctx)
		p.runHandlers(ctx, views, mctx, emit)
	}
}

// claimDedup claims the event's dedup key when it carries an EventID. proceed
// reports whether processing should continue: false means another replica
// already claimed this event. The returned release (nil when nothing was
// claimed) puts the claim back when processing ended in an error, so
// redelivery can retry the event. A claim-store error fails open: the event
// processes anyway.
func (p *Pipeline) claimDedup(ctx context.Context, env *lane.Envelope, broadcasterID uint64) (release func(error), proceed bool) {
	if env.EventID == "" {
		return nil, true
	}

	dedupKey := fmt.Sprintf("sesame:dedup:%d:%s", broadcasterID, env.EventID)
	claimed, err := p.dedup.Claim(ctx, dedupKey)
	if err != nil {
		p.log.Warn("dedup claim failed; processing event", zap.String("dedup_key", dedupKey), zap.Error(err))
		notice(ctx, err)
		return nil, true
	}
	if !claimed {
		return nil, false
	}

	return func(procErr error) {
		if procErr == nil {
			return
		}
		if relErr := p.dedup.Release(ctx, dedupKey); relErr != nil {
			p.log.Warn("dedup release failed", zap.String("dedup_key", dedupKey), zap.Error(relErr))
			notice(ctx, relErr)
		}
	}, true
}

// laneSubject picks the outgress lane a message's emissions ride: premium vs
// standard is a routing lane, not a feature switch.
func (p *Pipeline) laneSubject(regress module.Regress) string {
	if regress.IsPremium() {
		return p.outgressPremium
	}
	return p.outgressStandard
}

// floorSuppressed applies the send-time floor guard: the bot must never SAY
// floor content, no matter what a runtime variable ({args}, {touser}, an API
// result) injected into a saved-clean template. Save-time validation covers
// the template; this covers the expansion. Only outbound text carriers pay the
// check.
func (p *Pipeline) floorSuppressed(o *module.Output) bool {
	if o.Text == "" {
		return false
	}
	switch o.Type {
	case outgress.TypeChat, outgress.TypeAnnounce, outgress.TypePin:
	default:
		return false
	}
	term, hit := moderation.CheckFloor(o.Text)
	if hit {
		p.log.Warn("suppressed outgoing message carrying floor content",
			zap.String("term", term),
			zap.String("broadcaster_id", o.BroadcasterID))
	}
	return hit
}

// publishOutput translates one Output to the outgress wire contract and
// publishes it on the lane subject.
func (p *Pipeline) publishOutput(ctx context.Context, subject string, o *module.Output) error {
	body, err := buildOutgress(o)
	if err != nil {
		return err
	}
	return bus.PublishRaw(ctx, p.pub, subject, body)
}

// ensureLocale loads the broadcaster's locale for handler-emitted system text,
// reusing the value when command dispatch already populated it.
func (p *Pipeline) ensureLocale(ctx context.Context, mctx *module.Context) {
	if mctx.Locale != "" {
		return
	}
	if u, err := p.proj.User(ctx, mctx.BroadcasterID); err == nil {
		mctx.Locale = u.Locale
	}
}

// dropPoison logs a malformed envelope and acks it: redelivering poison forever
// helps no one.
func (p *Pipeline) dropPoison(ctx context.Context, msgID string, err error) error {
	p.log.Warn("dropping malformed envelope", zap.String("message_id", msgID), zap.Error(err))
	notice(ctx, err)
	return nil
}

// isOwnChat reports whether this is the bot's own chat message (seen via
// EventSub), which must never be reacted to.
func (p *Pipeline) isOwnChat(env *lane.Envelope, isChat bool) bool {
	return p.botID != "" && isChat && env.ChatterUserID == p.botID
}

// moduleViews fetches the broadcaster's ModuleView set, but only when a
// name-gated handler or command owner needs it; an event nobody name-gated skips
// the read entirely and returns nil. The automod module registers a chat handler,
// so chat needs the read whenever it is wired (its row carries the enable toggle
// and per-channel config the gate runs under).
func (p *Pipeline) moduleViews(ctx context.Context, eventType string, broadcasterID uint64) (map[string]projection.ModuleView, error) {
	if !p.registry.NeedsModuleViews(eventType) {
		return nil, nil
	}
	list, err := p.proj.Modules(ctx, broadcasterID)
	if err != nil {
		return nil, err
	}
	views := make(map[string]projection.ModuleView, len(list))
	for _, v := range list {
		views[v.Name] = v
	}
	return views, nil
}

// automodModuleName is the module that carries a broadcaster's automod settings
// (profile, block/allow terms) and enable toggle. It is a real registered module
// (app/sesame/modules/automod.go, MODULE_CATALOG id "automod" on the dashboard);
// its handler is a no-op because the gate runs inline before dispatch, so the
// pipeline reads the row directly here instead of through enabled().
const automodModuleName = "automod"

// automodConfigFrom extracts the broadcaster's automod Config from the fetched
// ModuleViews. nil views (no name-gated module needs chat) or an absent row
// yields nil (the global default: KindDefault ships enabled). A row present but
// disabled maps to a Config that opts the gate out for that channel, the same
// enable toggle every module has.
func automodConfigFrom(views map[string]projection.ModuleView) *automod.Config {
	if views == nil {
		return nil
	}
	mv, ok := views[automodModuleName]
	if !ok {
		return nil
	}
	cfg := automod.ParseConfig(mv.Configs)
	if !mv.IsEnabled {
		if cfg == nil {
			cfg = &automod.Config{}
		}
		cfg.Disabled = true
	}
	return cfg
}

// dispatch runs the command stage; a gate store error is logged and skipped like
// a handler error, never nacked.
func (p *Pipeline) dispatch(ctx context.Context, mctx *module.Context, views map[string]projection.ModuleView, emit module.Emit) {
	if err := p.dispatchCommand(ctx, mctx, views, emit); err != nil {
		p.log.Error("command dispatch failed", zap.Uint64("broadcaster_id", mctx.BroadcasterID), zap.Error(err))
		notice(ctx, err)
	}
}

// runHandlers runs each enabled module's handler for the message's event type in
// registration order. A handler's logic error is logged and skipped, never
// nacked (that would re-fire the siblings that already succeeded on redelivery).
func (p *Pipeline) runHandlers(ctx context.Context, views map[string]projection.ModuleView, mctx *module.Context, emit module.Emit) {
	eventType := mctx.Env.Type
	for _, m := range p.registry.For(eventType) {
		if !p.enabled(m, views, mctx) {
			continue
		}
		handle := m.Events[eventType]
		if handle == nil {
			continue
		}
		if err := handle(ctx, mctx, emit); err != nil {
			p.handlerFailed(ctx, mctx, m, err)
		}
	}
}

// handlerFailed records a handler's logic error to the log and NR. The event type
// and broadcaster id come from the Context.
func (p *Pipeline) handlerFailed(ctx context.Context, mctx *module.Context, m module.Module, err error) {
	p.log.Error("module handler failed",
		zap.String("module", moduleLabel(m)),
		zap.String("type", mctx.Env.Type),
		zap.Uint64("broadcaster_id", mctx.BroadcasterID),
		zap.Error(err))
	if txn := newrelic.FromContext(ctx); txn != nil {
		txn.AddAttribute("module.failed", moduleLabel(m))
		txn.NoticeError(err)
	}
}

// traceEvent tags the current New Relic transaction with the event identity.
func traceEvent(ctx context.Context, eventType string, broadcasterID uint64) {
	if txn := newrelic.FromContext(ctx); txn != nil {
		txn.AddAttribute("event.type", eventType)
		txn.AddAttribute("event.broadcaster_id", broadcasterID)
	}
}

// notice records err on the current New Relic transaction, if any.
func notice(ctx context.Context, err error) {
	if txn := newrelic.FromContext(ctx); txn != nil {
		txn.NoticeError(err)
	}
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
		mv, ok := views[m.Name]
		return enabledByDefault(mv, ok, mctx)
	case module.KindOptIn:
		mv, ok := views[m.Name]
		return enabledOptIn(mv, ok, mctx)
	default:
		return false
	}
}

// enabledByDefault gates a KindDefault module: it ships enabled (no row means
// on, no config), and only an explicit row can disable it.
func enabledByDefault(mv projection.ModuleView, ok bool, mctx *module.Context) bool {
	if !ok {
		mctx.Config = nil
		return true
	}
	if !mv.IsEnabled {
		return false
	}
	mctx.Config = mv.Configs
	return true
}

// enabledOptIn gates a KindOptIn module: it runs only when its row enables it.
func enabledOptIn(mv projection.ModuleView, ok bool, mctx *module.Context) bool {
	if !ok || !mv.IsEnabled {
		return false
	}
	mctx.Config = mv.Configs
	return true
}
