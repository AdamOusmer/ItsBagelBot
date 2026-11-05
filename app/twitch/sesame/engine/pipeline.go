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
	autoIntN    func(int) int
	autoExec    autoExecState
	autoSegment time.Duration

	queueWaiters *queueWaiters
	queueTiming  queueTiming
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
	}
	p.localeLookup = p.readLocale
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
	return p
}

func (p *Pipeline) Close() {
	p.queueWaiters.close()
	p.closeObservers()
	if p.uses != nil {
		p.uses.Close()
	}
	if p.stats != nil {
		p.stats.Close()
	}
}

const chatType = "channel.chat.message"

func (p *Pipeline) Process(msg *bus.Message) error {
	ctx, cancel := messageContext(msg)
	defer cancel()

	env := GetEnvelope()
	defer PutEnvelope(env)
	if err := decodeEnvelope(ctx, msg.Payload, env); err != nil {
		traceResult(ctx, "invalid")
		return p.dropPoison(ctx, msg.UUID, err)
	}
	broadcasterID, ok := env.BroadcasterID()
	p.countDecoded(ctx, env, broadcasterID)
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
	target := broadcasterID
	if env.Origin == "trial" {
		target = 0
	}
	p.stats.count(target, decodedDelta(env))
	p.recordDecoded(env)
	if env.Origin == "trial" {
		p.addTrial(ctx, env.BroadcasterUserID, "decoded", env.MessageCount())
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
	emit := p.newEmit(ctx, env.BroadcasterUserID, &emission)
	started := time.Now()
	p.runTracedStages(ctx, mctx, views, emit, &emission)
	p.flushLegacyOutput(ctx, &emission)
	p.recordActivity(ctx, mctx, &emission, started)

	if mctx.Command != "" {
		p.stats.countAnswered(broadcasterID)
	}
	p.recordProcessed(broadcasterID, admission.AccountCreatedAt, mctx.Command != "", emission.ordinal)

	p.notifyObservers(ObservedEvent{
		BroadcasterID: broadcasterID,
		Type:          env.Type,
		At:            started,
		Locale:        p.observerLocale(ctx, mctx),
		Handled:       mctx.Command != "",
		Command:       mctx.Command,
		Actor:         env.ChatterUserName,
		DurationMS:    int(time.Since(started).Milliseconds()),
	})

	tracePipelineResult(ctx, emission.err)
	return emission.err
}

func (p *Pipeline) feedChatGate(ctx context.Context, env *lane.Envelope, broadcasterID uint64) {
	if p.chatLineCounter != nil && env.Type == chatType {
		p.chatLineCounter.CountChatLine(ctx, broadcasterID)
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

func (p *Pipeline) runTracedStages(ctx context.Context, mctx *module.Context, views map[string]projection.ModuleView, emit module.Emit, emission *emitState) {
	segment := startStage(ctx, "sesame.engine")
	p.runStages(ctx, mctx, views, emit)
	endStageForError(segment, emission.err, "error")
}

func (p *Pipeline) flushLegacyOutput(ctx context.Context, emission *emitState) {
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

func (p *Pipeline) runStages(ctx context.Context, mctx *module.Context, views map[string]projection.ModuleView, emit module.Emit) {
	env := &mctx.Env
	soloChat := env.Type == chatType && len(env.Senders) == 0

	consumed := false
	if env.Origin != "trial" {
		consumed = p.moderateChat(ctx, mctx, views, emit)
		consumed = p.refundSpecialRedemption(mctx, emit) || consumed
	}
	if soloChat && !consumed {
		p.dispatch(ctx, mctx, views, emit)
	}
	if !consumed && len(p.registry.For(env.Type)) > 0 {
		p.ensureHandlerLocale(ctx, mctx)
		p.runHandlers(ctx, views, mctx, emit, env.Type)
	}
	p.runAutomationStage(ctx, mctx, views, consumed, emit)
}

func (p *Pipeline) dropPoison(ctx context.Context, msgID string, err error) error {
	p.log.Warn("dropping malformed envelope", zap.String("message_id", msgID), zap.Error(err))
	notice(ctx, err)
	return nil
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
