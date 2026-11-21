// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"time"

	"ItsBagelBot/app/twitch/sesame/automod"
	"ItsBagelBot/app/twitch/sesame/engine/scope"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/analytics"
	"ItsBagelBot/internal/channelruntime"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/idempotency"

	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"
)

type Config struct {
	BotID            string
	OutgressPremium  string
	OutgressStandard string
	CountUses        bool

	AutomodEnforce bool

	ShieldEnabled bool

	AdaptiveEnabled bool

	AutoRefundChannel string
}

type Pipeline struct {
	runtime      *channelruntime.Client
	trialCounts  CounterBumper
	log          *zap.Logger
	pub          bus.Publisher
	proj         projection.Reader
	localeLookup func(context.Context, uint64) (string, error)
	registry     *Registry

	live          IsLiveChecker
	cooldown      CooldownStore
	uses          *useReporter
	loyalty       LoyaltyStore
	dedup         *EventDedup
	followage     FollowageLookup
	accountAge    AccountAgeLookup
	streamInfo    StreamInfoLookup
	channelCounts ChannelCountsLookup
	viewers       ViewerLookup
	stats         *botStats
	customFetch   UrlFetchCaller
	quotes        QuotesStore
	gossip        GossipCaller
	accounts      TwitchAccountLookup
	emotes        scope.EmoteSource
	roster        *chatterRoster

	botID            string
	botUserID        uint64
	outgressPremium  string
	outgressStandard string

	automod        *automod.Gate
	automodEnforce bool
	automodBeta    bool

	automationsBeta bool
	reputation      Reputation
	campaign        Campaign

	shieldEnabled   bool
	adaptiveEnabled bool
	raidGate        *raidCooldown

	nuke *Nuke

	special           *SpecialSet
	autoRefundChannel string

	observers []*observerLane

	analytics *analytics.Recorder

	chatLineCounter ChatLineCounter

	automations AutomationDeps
	autoExec    autoExecState
	autoSegment time.Duration

	queueWaiters     *queueWaiters
	queueTiming      queueTiming
	durable          *durableRuns
	countdownCancel  context.CancelFunc
	countdownDone    chan struct{}
	completionNeeded bool
	completions      completionSet
	speech           SpeechQueue
}

func NewPipeline(d Deps, registry *Registry, cfg Config) *Pipeline {
	p := &Pipeline{
		runtime:           d.Runtime,
		trialCounts:       d.Stats,
		log:               d.Log,
		pub:               d.Pub,
		proj:              d.Proj,
		registry:          registry,
		live:              d.Live,
		cooldown:          d.Cooldown,
		loyalty:           d.Loyalty,
		dedup:             d.Dedup,
		followage:         d.Followage,
		accountAge:        d.AccountAge,
		streamInfo:        d.StreamInfo,
		channelCounts:     d.ChannelCounts,
		viewers:           d.Viewers,
		customFetch:       d.CustomFetch,
		quotes:            d.Quotes,
		gossip:            d.Gossip,
		accounts:          d.TwitchAccounts,
		botID:             cfg.BotID,
		botUserID:         parseBotUserID(cfg.BotID),
		outgressPremium:   cfg.OutgressPremium,
		outgressStandard:  cfg.OutgressStandard,
		automod:           d.Automod,
		automodEnforce:    cfg.AutomodEnforce,
		automodBeta:       moduleBeta(registry, automodModuleName),
		automationsBeta:   moduleBeta(registry, AutomationsModuleName),
		reputation:        d.Reputation,
		campaign:          d.Campaign,
		shieldEnabled:     cfg.ShieldEnabled,
		adaptiveEnabled:   cfg.AdaptiveEnabled,
		raidGate:          newRaidCooldown(raidCooldownTTL),
		emotes:            d.Emotes,
		roster:            newChatterRoster(),
		nuke:              d.Nuke,
		special:           d.Special,
		autoRefundChannel: cfg.AutoRefundChannel,
		chatLineCounter:   d.ChatLines,
		automations:       withAutomationDefaults(d.Automations),
		analytics:         d.Analytics,
		queueWaiters:      newQueueWaiters(),
		queueTiming:       defaultQueueTiming,
		speech:            d.Speech(),
	}
	p.localeLookup = p.readLocale
	p.completionNeeded = needsCompletion(d, cfg)
	if d.Automod == nil && d.Log != nil {
		d.Log.Warn("automod gate not wired; chat moderation disabled")
	}
	if cfg.CountUses && d.Pub != nil {
		p.uses = newUseReporter(d.Pub, d.Log)
	}
	if d.Nuke != nil {
		d.Nuke.setShield(p.shieldDecision)
	}
	if d.Stats != nil {
		p.stats = newBotStats(d.Stats, d.Log)
	}
	p.durable = newDurableRuns(p, p.automations)
	p.startCountdownWorker()
	return p
}

func (p *Pipeline) Close() {
	defer p.dedup.Finish(context.Background())
	if p.countdownCancel != nil {
		p.countdownCancel()
		<-p.countdownDone
	}
	p.durable.halt()
	p.queueWaiters.close()
	p.durable.handOff()
	p.closeObservers()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p.Finish(ctx)
	if p.uses != nil {
		p.uses.Close()
	}
	if p.stats != nil {
		p.stats.Close()
	}
}

const chatType = "channel.chat.message"

func (p *Pipeline) Process(msg *bus.Message) (err error) {
	ctx, cancel := messageContext(msg)
	defer cancel()
	ctx = withTrace(ctx)

	env := GetEnvelope()
	defer PutEnvelope(env)
	if err := decodeEnvelope(ctx, msg.Payload, env); err != nil {
		traceResult(ctx, "invalid")
		return p.dropPoison(ctx, msg.UUID, err)
	}
	switch env.Type {
	case "channel.cheer", "channel.subscription.message":
		ctx = bus.WithOriginTrace(ctx, msg.Metadata)
	}
	if !p.completionNeeded {
		return p.processAcknowledged(ctx, env)
	}
	ctx, work := p.startCompletion(ctx, msg)
	defer p.completions.deliveries.FinishOnReturn(work, msg.DeferCompletion(), &err)
	defer finishDeliveryExecution(ctx, work)
	return p.processAcknowledged(ctx, env)
}

func finishDeliveryExecution(ctx context.Context, work *idempotency.Work) {
	if work.Deferred() {
		bus.HoldDelivery(ctx)
	}
	bus.EndDeliveryExecution(ctx)
}

func (p *Pipeline) processAcknowledged(ctx context.Context, env *lane.Envelope) error {
	err := p.processEnvelope(ctx, env)
	p.recordAcknowledged(ctx, env, err)
	return err
}

func (p *Pipeline) processEnvelope(ctx context.Context, env *lane.Envelope) error {
	broadcasterID, ok := env.BroadcasterID()
	p.countDecodedEffect(ctx, env, broadcasterID)
	traceEnvelope(ctx, env)
	if !p.eligible(env) {
		traceResult(ctx, "filtered")
		return nil
	}

	if !ok {
		traceResult(ctx, "invalid")
		return nil
	}
	return p.processByOrigin(ctx, env, broadcasterID)
}

func (p *Pipeline) countDecoded(ctx context.Context, env *lane.Envelope, broadcasterID uint64) {
	p.countDecodedMetrics(ctx, decodedMetricsFor(env, broadcasterID))
}

type decodedMetrics struct {
	target  uint64
	change  delta
	trialID string
	trial   int64
}

func decodedMetricsFor(env *lane.Envelope, broadcasterID uint64) decodedMetrics {
	m := decodedMetrics{target: broadcasterID, change: decodedDelta(env)}
	if env.Origin == "trial" {
		m.target, m.trialID, m.trial = 0, env.BroadcasterUserID, env.MessageCount()
	}
	return m
}

func (p *Pipeline) countDecodedMetrics(ctx context.Context, m decodedMetrics) {
	if p.stats != nil {
		p.stats.record(ctx, func() { p.stats.count(m.target, m.change) })
	}
	if m.trial > 0 {
		p.addTrial(ctx, m.trialID, "decoded", m.trial)
	}
}

func decodedDelta(env *lane.Envelope) delta {
	n := counterDelta(env.MessageCount())
	if env.Type == chatType {
		return chatDelta(n)
	}
	return eventDelta(n)
}

func (p *Pipeline) processByOrigin(ctx context.Context, env *lane.Envelope, broadcasterID uint64) error {
	if env.Origin == "trial" {
		return p.processTrial(ctx, env, broadcasterID)
	}
	admission, allowed, err := p.runtimeAdmission(ctx, broadcasterID)
	if err != nil || !allowed {
		return err
	}

	p.feedChatGate(ctx, env, broadcasterID)

	p.roster.ObserveEnvelope(broadcasterID, env)

	if p.nuke != nil {
		p.nuke.recordChat(broadcasterID, env)
	}

	mctx := p.leaseContext(env, broadcasterID)
	defer PutContext(mctx)
	views, err := p.loadMessageProjections(ctx, mctx)
	if err != nil {
		traceResult(ctx, "error")
		return err
	}

	emission := emitState{
		admission: admission,
		subject:   p.laneSubject(mctx.Regress),
		env:       env,
		locale:    mctx.Locale,
		mctx:      mctx,
	}
	started := time.Now()
	p.runTracedStages(ctx, mctx, views, &emission)
	p.flushLegacyOutput(ctx, &emission)
	p.recordActivity(ctx, mctx, &emission, started)

	p.recordProcessedEffect(ctx, mctx, admission.AccountCreatedAt, emission.accepted)

	p.notifyObservers(ObservedEvent{
		BroadcasterID: broadcasterID,
		Type:          env.Type,
		At:            started,
		Locale:        p.observerLocale(ctx, mctx),
		Handled:       mctx.CommandAnswered,
		Command:       mctx.Command,
		Actor:         env.ChatterUserName,
		DurationMS:    int(time.Since(started).Milliseconds()),
	})

	tracePipelineResult(ctx, emission.err)
	return emission.err
}

func (p *Pipeline) feedChatGate(ctx context.Context, env *lane.Envelope, broadcasterID uint64) {
	if p.chatLineCounter != nil && env.Type == chatType {
		ctx, work := idempotency.NewWork(ctx)
		p.chatLineCounter.CountChatLine(ctx, broadcasterID)
		work.Seal(nil)
	}
}

func decodeEnvelope(ctx context.Context, payload []byte, env *lane.Envelope) error {
	segment := startStage(ctx, "sesame.decode")
	err := codec.FastUnmarshal(payload, env)
	endStageForError(segment, err, "invalid")
	return err
}

func (p *Pipeline) tracedModuleViews(ctx context.Context, eventType string, broadcasterID uint64) (map[string]projection.ModuleView, error) {
	segment := startStage(ctx, "sesame.dependency.projection")
	views, err := p.moduleViews(ctx, eventType, broadcasterID)
	endStageForError(segment, err, "error")
	return views, err
}

func (p *Pipeline) runTracedStages(ctx context.Context, mctx *module.Context, views map[string]projection.ModuleView, emission *emitState) {
	segment := startStage(ctx, "sesame.engine")
	emit := p.newEmit(ctx, mctx.Env.BroadcasterUserID, emission)
	consumed := p.runStages(ctx, mctx, views, emit)
	if emission.err == nil {
		emission.err = mctx.Failure
	}
	endStageForError(segment, emission.err, "error")
	traceDecision(ctx, decisionOf(mctx, consumed), emission.accepted)
}

func decisionOf(mctx *module.Context, consumed bool) string {
	switch {
	case consumed:
		return "moderated"
	case mctx.Command != "":
		return "command"
	case mctx.Emitted > 0:
		return "handled"
	default:
		return "ignored"
	}
}

func (p *Pipeline) flushLegacyOutput(ctx context.Context, emission *emitState) {
	defer emission.finishLegacy()
	if emission.err != nil || !emission.needsFlush {
		return
	}
	segment := startStage(ctx, "sesame.output.flush")
	emission.err = flushWithin(ctx, p.pub)
	endStageForError(segment, emission.err, "error")
}

func endStageForError(segment *newrelic.Segment, err error, failure string) {
	if err != nil {
		endStage(segment, failure)
		return
	}
	endStage(segment, "ok")
}

func tracePipelineResult(ctx context.Context, err error) {
	if err != nil {
		traceResult(ctx, "error")
		return
	}
	traceResult(ctx, "ok")
}

func (p *Pipeline) eligible(env *lane.Envelope) bool {
	isChat := env.Type == chatType
	if p.isOwnChat(env, isChat) {
		return false
	}
	return isChat || len(p.registry.For(env.Type)) > 0
}

func (p *Pipeline) leaseContext(env *lane.Envelope, broadcasterID uint64) *module.Context {
	mctx := GetContext()
	mctx.Env = *env
	mctx.Regress = module.RegressFromLane(env.Lane)
	mctx.BroadcasterID = broadcasterID
	mctx.Log = p.log
	mctx.LocaleLookup = p.localeLookup
	return mctx
}

func (p *Pipeline) runStages(ctx context.Context, mctx *module.Context, views map[string]projection.ModuleView, emit module.Emit) bool {
	env := &mctx.Env
	soloChat := env.Type == chatType && len(env.Senders) == 0

	consumed := false
	if env.Origin != "trial" {
		consumed = p.moderate(ctx, mctx, views, emit)
	}
	if soloChat && !consumed {
		stage := startStage(ctx, "sesame.dispatch")
		p.dispatch(ctx, mctx, views, emit)
		endStage(stage, "ok")
	}
	if !consumed && len(p.registry.For(env.Type)) > 0 {
		stage := startStage(ctx, "sesame.handlers")
		p.ensureHandlerLocale(ctx, mctx)
		p.runHandlers(ctx, views, mctx, emit)
		endStage(stage, "ok")
	}
	stage := startStage(ctx, "sesame.automations")
	mctx.EffectScope, mctx.EffectOrdinal, mctx.EffectContext = "automation", 0, ctx
	p.runAutomationStage(ctx, mctx, views, consumed, emit)
	endStage(stage, "ok")
	return consumed
}

func (p *Pipeline) moderate(ctx context.Context, mctx *module.Context, views map[string]projection.ModuleView, emit module.Emit) bool {
	stage := startStage(ctx, "sesame.moderation")
	consumed := p.moderateChat(ctx, mctx, views, emit)
	consumed = p.refundSpecialRedemption(mctx, emit) || consumed
	endStage(stage, "ok")
	return consumed
}

func (p *Pipeline) dropPoison(ctx context.Context, msgID string, err error) error {
	p.log.Warn("dropping malformed envelope", zap.String("message_id", msgID), zap.Error(err))
	notice(ctx, err)
	return bus.Poison(err)
}

func (p *Pipeline) isOwnChat(env *lane.Envelope, isChat bool) bool {
	return p.botID != "" && isChat && env.ChatterUserID == p.botID
}

func (p *Pipeline) moduleViews(ctx context.Context, eventType string, broadcasterID uint64) (map[string]projection.ModuleView, error) {
	if !p.registry.NeedsModuleViews(eventType) {
		return nil, nil
	}
	// The projection cache's shared map: stages must never write or recycle it.
	return p.proj.Modules(ctx, broadcasterID)
}
