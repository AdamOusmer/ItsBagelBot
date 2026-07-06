package engine

import (
	"context"
	"fmt"
	"time"

	"ItsBagelBot/app/sesame/automod"
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"
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
	pub      message.Publisher
	proj     projection.Reader
	registry *Registry

	live     IsLiveChecker
	cooldown CooldownStore
	dedup    DedupStore
	uses     *useReporter

	botID            string
	outgressPremium  string
	outgressStandard string

	automod        *automod.Gate
	automodEnforce bool
	reputation     Reputation

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
		botID:            cfg.BotID,
		outgressPremium:  cfg.OutgressPremium,
		outgressStandard: cfg.OutgressStandard,
		automod:          d.Automod,
		automodEnforce:   cfg.AutomodEnforce,
		reputation:       d.Reputation,
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

	isChat := env.Type == chatType
	if p.isOwnChat(env, isChat) {
		return nil
	}

	// Chat always runs (command dispatch is engine-internal, not a registered
	// handler), so only a non-chat type with no handler bails out here.
	handlers := p.registry.For(env.Type)
	if !isChat && len(handlers) == 0 {
		return nil
	}

	broadcasterID, ok := env.BroadcasterID()
	if !ok {
		return nil
	}
	traceEvent(ctx, env.Type, broadcasterID)

	var dedupKey string
	if env.EventID != "" {
		dedupKey = fmt.Sprintf("sesame:dedup:%d:%s", broadcasterID, env.EventID)
		claimed, claimErr := p.dedup.Claim(ctx, dedupKey)
		if claimErr != nil {
			p.log.Warn("dedup claim failed; processing event", zap.String("dedup_key", dedupKey), zap.Error(claimErr))
			notice(ctx, claimErr)
		} else if !claimed {
			return nil
		} else {
			defer func() {
				if err != nil {
					if relErr := p.dedup.Release(ctx, dedupKey); relErr != nil {
						p.log.Warn("dedup release failed", zap.String("dedup_key", dedupKey), zap.Error(relErr))
						notice(ctx, relErr)
					}
				}
			}()
		}
	}

	views, err := p.moduleViews(ctx, env.Type, broadcasterID)
	if err != nil {
		return err // infrastructure failure: nack
	}

	mctx := GetContext()
	defer PutContext(mctx)
	mctx.Env = *env
	mctx.Regress = module.RegressFromLane(env.Lane)
	mctx.BroadcasterID = broadcasterID
	mctx.Log = p.log

	// emit is the sink command Run and event handlers hand their Outputs to. The
	// first publish or marshal failure is captured in emitErr (which nacks) and
	// short-circuits the rest. It stays a closure here so the no-output hot path
	// allocates nothing beyond it.
	var emitErr error
	regress := mctx.Regress
	emit := func(o *module.Output) {
		if emitErr != nil || o == nil || o.Type == "" {
			return
		}
		subject := p.outgressStandard
		if regress.IsPremium() {
			subject = p.outgressPremium
		}
		body, berr := buildOutgress(o)
		if berr != nil {
			emitErr = berr
			return
		}
		if perr := bus.PublishRaw(ctx, p.pub, subject, body); perr != nil {
			emitErr = perr
		}
	}

	// Automod gate: inspect the chat line before dispatch. With enforcement on, a
	// ban/timeout verdict is emitted and command dispatch + handlers are skipped
	// for this line (the chatter is being actioned); a verdict we cannot yet
	// enforce is logged. With enforcement off it is shadow-logged only. Cohorts
	// (Senders present) are handled by a later phase, not inspected here.
	// amCfg is the broadcaster's automod config (nil = global default), read from
	// the "automod" ModuleView. The automod module (app/sesame/modules/automod.go)
	// registers a chat handler, so the registry marks chat as needing ModuleViews
	// and the row arrives here through the standard module path.
	amCfg := automodConfigFrom(views)

	actioned := false
	if isChat && p.automod != nil && len(env.Senders) == 0 {
		if v := p.automod.InspectWith(mctx.Chatter(), env.Text, amCfg); v.Action != automod.ActionNone {
			// Tier-2 reputation escalation: a repeat offender's timeout becomes a
			// ban, then this hit is recorded against the chatter.
			if p.reputation != nil {
				v = escalateByReputation(v, p.reputation.Score(ctx, env.ChatterUserID))
				p.reputation.Bump(ctx, env.ChatterUserID)
			}
			if p.automodEnforce {
				actioned = p.emitAutomod(v, env, emit)
			}
			p.log.Info("automod verdict",
				zap.String("action", v.Action.String()),
				zap.String("rule", v.Rule),
				zap.Bool("enforced", actioned),
				zap.Uint64("broadcaster_id", broadcasterID),
				zap.String("chatter_id", env.ChatterUserID))
		}
	}

	// A folded duplicate cohort (Senders present) is plain chat the ingress
	// squash collapsed identical lines from many chatters into env.Senders. Fan
	// reputation out over every sender so a coordinated duplicate flood builds each
	// participant's score, then inspect the shared text once: a hostile cohort (a
	// slur/scam/IP-logger line posted in unison) is a raid. A large one escalates
	// to channel-level Shield Mode instead of banning account by account; a small
	// one is banned directly. A clean cohort (hype copypasta) trips nothing and
	// only builds reputation. Command dispatch is skipped (a cohort is never a
	// command).
	if isChat && len(env.Senders) > 0 {
		if p.reputation != nil {
			for i := range env.Senders {
				p.reputation.Bump(ctx, env.Senders[i].ChatterUserID)
			}
		}
		if p.automod != nil {
			// Cohort senders are untrusted viewers: the squash folds only plain
			// duplicate chat, so a trusted VIP/mod is judged on content here too.
			if v := p.automod.InspectWith(module.RoleEveryone, env.Text, amCfg); v.Action != automod.ActionNone {
				if p.automodEnforce {
					actioned = p.emitCohort(v, broadcasterID, env, emit)
				}
				p.log.Info("automod cohort verdict",
					zap.String("action", v.Action.String()),
					zap.String("rule", v.Rule),
					zap.Int("cohort", len(env.Senders)),
					zap.Bool("enforced", actioned),
					zap.Uint64("broadcaster_id", broadcasterID))
			}
		}
	}

	if isChat && !actioned && len(env.Senders) == 0 {
		p.dispatch(ctx, mctx, views, emit)
	}
	if len(handlers) > 0 && !actioned {
		// Event handlers can emit localized system text too (for example the
		// stream-online bagel announcement). Command dispatch resolves locale for
		// baked commands, but non-command events never pass through that path.
		// Reuse the value when dispatch already populated it and otherwise load it
		// from the projected user before invoking handlers.
		if mctx.Locale == "" {
			if u, uerr := p.proj.User(ctx, broadcasterID); uerr == nil {
				mctx.Locale = u.Locale
			}
		}
		p.runHandlers(ctx, views, mctx, emit)
	}

	// nil = ack; a publish/marshal failure on the emit path = nack.
	return emitErr
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

// repEscalateThreshold is the reputation score at or above which a repeat
// offender's timeout is upgraded to a ban.
const repEscalateThreshold = 3

// escalateByReputation raises a verdict against a repeat offender: a chatter
// whose reputation score meets the threshold has a timeout upgraded to a ban.
// Other verdicts are unchanged.
func escalateByReputation(v automod.Verdict, score int) automod.Verdict {
	if score >= repEscalateThreshold && v.Action == automod.ActionTimeout {
		v.Action = automod.ActionBan
		v.Rule += "+repeat"
	}
	return v
}

// emitAutomod translates an enforced single-chatter verdict into an outgress
// moderation action and emits it, returning whether an action was emitted.
func (p *Pipeline) emitAutomod(v automod.Verdict, env *lane.Envelope, emit module.Emit) bool {
	return p.emitModeration(v, env.BroadcasterUserID, env.ChatterUserID, emit)
}

// emitModeration maps a verdict to a ban/timeout Output against one target and
// emits it, returning whether an action was actually emitted. Only ban and
// timeout are wired to Helix today (phase 0); delete/restrict/warn are left for
// the caller to log until their outgress path lands.
func (p *Pipeline) emitModeration(v automod.Verdict, broadcasterID, targetUserID string, emit module.Emit) bool {
	o := GetOutput()
	switch v.Action {
	case automod.ActionBan:
		o.Type = outgress.TypeBan
	case automod.ActionTimeout:
		o.Type = outgress.TypeTimeout
		o.Duration = float64(v.Seconds)
	default:
		PutOutput(o)
		return false
	}
	o.BroadcasterID = broadcasterID
	o.TargetUserID = targetUserID
	o.Reason = "automod:" + v.Rule
	emit(o)
	PutOutput(o)
	return true
}

// massRaid parameters. A folded cohort of this many distinct hostile senders is a
// mass raid: it escalates to one channel-level Shield Mode call rather than a ban
// per account, which at Twitch's 800-action/min Helix budget a large raid would
// blow. massRaidBanCap bounds how many per-account bans one fold may still emit
// (cleanup within budget); Shield Mode, when armed, gates the rest at the channel.
const (
	massRaidThreshold = 15
	massRaidBanCap    = 40
	raidCooldownTTL   = 60 * time.Second
)

// isMassRaid reports whether a folded cohort with a punishing content verdict is
// large enough to warrant channel-level Shield Mode. A delete/warn verdict (e.g. a
// caps heuristic on hype copypasta) is never a raid, so only timeout and ban
// qualify.
func isMassRaid(v automod.Verdict, distinctSenders int) bool {
	return distinctSenders >= massRaidThreshold &&
		(v.Action == automod.ActionTimeout || v.Action == automod.ActionBan)
}

// emitCohort enforces a hostile folded cohort. A mass raid (large + punishing)
// escalates to one Shield Mode activation (deduped per channel) when Shield Mode
// is armed, then a bounded prefix of the cohort is banned as cleanup; a smaller
// cohort is banned outright. Returns whether any action was emitted.
func (p *Pipeline) emitCohort(v automod.Verdict, broadcasterID uint64, env *lane.Envelope, emit module.Emit) bool {
	acted := false
	if p.shieldEnabled && isMassRaid(v, len(env.Senders)) && p.raidGate.trip(broadcasterID, time.Now()) {
		p.emitShield(env.BroadcasterUserID, emit)
		p.log.Warn("automod shield mode",
			zap.Uint64("broadcaster_id", broadcasterID),
			zap.Int("cohort", len(env.Senders)),
			zap.String("rule", v.Rule))
		acted = true
	}

	limit := len(env.Senders)
	if limit > massRaidBanCap {
		limit = massRaidBanCap
	}
	for i := 0; i < limit; i++ {
		id := env.Senders[i].ChatterUserID
		if id == "" {
			continue
		}
		if p.emitModeration(v, env.BroadcasterUserID, id, emit) {
			acted = true
		}
	}
	return acted
}

// emitShield activates a channel's Shield Mode, the mass-raid channel-level
// defense. broadcasterID is the raw string channel id; outgress adds the
// moderator id and the {"is_active":true} body.
func (p *Pipeline) emitShield(broadcasterID string, emit module.Emit) {
	o := GetOutput()
	o.Type = outgress.TypeShieldMode
	o.BroadcasterID = broadcasterID
	o.Reason = "automod:mass_raid"
	emit(o)
	PutOutput(o)
}

// banData is the inner object of a Helix Ban User request body. Duration is
// omitted for a permanent ban and set (in seconds) for a timeout; reason is
// optional.
type banData struct {
	UserID   string `json:"user_id"`
	Duration int    `json:"duration,omitempty"`
	Reason   string `json:"reason,omitempty"`
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
	case outgress.TypeClip:
		// The Create Clip call takes no body: broadcaster_id, title and duration
		// all ride the query string, which outgress builds. This payload carries
		// what outgress needs — the title and duration to pass to Twitch, the
		// clipper login, and the broadcaster's custom reply template — to compose
		// the reply posted with the clip URL (outgress expands its {clip} token).
		// Duration 0 (plain !clip) and an empty reply are omitted.
		payload, err := sonic.Marshal(struct {
			Title    string  `json:"title,omitempty"`
			Clipper  string  `json:"clipper,omitempty"`
			Duration float64 `json:"duration,omitempty"`
			Reply    string  `json:"reply,omitempty"`
		}{o.Text, o.To, o.Duration, o.Template})
		if err != nil {
			return nil, err
		}
		msg = outgress.Message{
			Type:          outgress.TypeClip,
			BroadcasterID: o.BroadcasterID,
			Payload:       payload,
		}
	case outgress.TypeBan, outgress.TypeTimeout:
		// Helix Ban User body: {"data":{"user_id","duration","reason"}}. A ban
		// omits duration (permanent); a timeout sets it (whole seconds; Output
		// shares the Duration field with clip, which carries a fraction).
		// broadcaster_id and moderator_id are added by outgress on the query
		// string, not here.
		payload, err := sonic.Marshal(struct {
			Data banData `json:"data"`
		}{banData{UserID: o.TargetUserID, Duration: int(o.Duration), Reason: o.Reason}})
		if err != nil {
			return nil, err
		}
		msg = outgress.Message{
			Type:          o.Type,
			BroadcasterID: o.BroadcasterID,
			Payload:       payload,
		}
	case outgress.TypeShieldMode:
		// Helix Update Shield Mode Status body: {"is_active":true}. The automod
		// only ever activates (mass-raid escalation); deactivation stays a human
		// decision. broadcaster_id and moderator_id ride the query string, added
		// by outgress.
		msg = outgress.Message{
			Type:          outgress.TypeShieldMode,
			BroadcasterID: o.BroadcasterID,
			Payload:       []byte(`{"is_active":true}`),
		}
	default:
		msg = outgress.Message{
			Type:          o.Type,
			BroadcasterID: o.BroadcasterID,
		}
	}

	return sonic.Marshal(msg)
}
