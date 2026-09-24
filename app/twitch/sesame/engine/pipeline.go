// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"ItsBagelBot/internal/domain/event/data"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"time"

	"ItsBagelBot/app/twitch/sesame/automod"
	"ItsBagelBot/app/twitch/sesame/confcache"
	"ItsBagelBot/app/twitch/sesame/engine/scope"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/moderation"
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
	trialCounts CounterBumper
	log         *zap.Logger
	pub         bus.Publisher
	proj        projection.Reader
	registry    *Registry

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
	emotes        scope.EmoteSource
	roster        *chatterRoster

	botID            string
	outgressPremium  string
	outgressStandard string

	automod        *automod.Gate
	automodEnforce bool
	automodBeta    bool
	reputation     Reputation
	campaign       Campaign

	shieldEnabled   bool
	adaptiveEnabled bool
	raidGate        *raidCooldown

	nuke *Nuke

	special           *SpecialSet
	autoRefundChannel string

	observers []*observerLane

	chatLineCounter ChatLineCounter
}

func NewPipeline(d Deps, registry *Registry, cfg Config) *Pipeline {
	p := &Pipeline{
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
		botID:             cfg.BotID,
		outgressPremium:   cfg.OutgressPremium,
		outgressStandard:  cfg.OutgressStandard,
		automod:           d.Automod,
		automodEnforce:    cfg.AutomodEnforce,
		automodBeta:       automodBeta(registry),
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
	}
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
	ctx := msg.Context()

	env := GetEnvelope()
	defer PutEnvelope(env)
	if err := decodeEnvelope(ctx, msg.Payload, env); err != nil {
		traceResult(ctx, "invalid")
		return p.dropPoison(ctx, msg.UUID, err)
	}
	broadcasterID, ok := env.BroadcasterID()
	if err := p.countDecoded(ctx, msg.UUID, env, broadcasterID); err != nil {
		return err
	}
	if !p.eligible(env) {
		traceResult(ctx, "filtered")
		return nil
	}

	if !ok {
		traceResult(ctx, "invalid")
		return nil
	}
	traceEvent(ctx, env.Type, env.Lane, broadcasterID)
	return p.processByOrigin(ctx, env, broadcasterID)
}

// Decoded totals are published before the source handler returns successfully.
// A retry reuses the source identity, including when a publish acknowledgement
// was lost after the broker stored the counter event.
func (p *Pipeline) countDecoded(ctx context.Context, sourceID string, env *lane.Envelope, broadcasterID uint64) error {
	n := env.MessageCount()
	if p.stats != nil {
		if p.pub == nil || sourceID == "" {
			return errors.New("decoded counters require publisher and source identity")
		}
		if n <= 0 || n > data.MaxCounter {
			return errors.New("decoded count outside signed integer range")
		}
		targets := []uint64{0}
		if env.Origin != "trial" && broadcasterID != 0 {
			targets = append(targets, broadcasterID)
		}
		for _, target := range targets {
			scope := data.CounterScopeChannel
			if target == 0 {
				scope = data.CounterScopeBot
			}
			entries := []data.CounterBumpEntry{{Name: data.CounterEventsProcessed, Scope: scope, Delta: n}}
			if env.Type == chatType {
				entries = append(entries, data.CounterBumpEntry{Name: data.CounterMessagesProcessed, Scope: scope, Delta: n})
			}
			sum := sha256.Sum256([]byte("decoded:" + sourceID + ":" + strconv.FormatUint(target, 10)))
			id := "decoded:" + hex.EncodeToString(sum[:])
			body, err := codec.FastMarshal(data.CounterBumpedDTO{BatchID: id, UserID: target, Bumps: entries})
			if err != nil {
				return err
			}
			if err = bus.PublishConfirmed(ctx, p.pub, bus.Publication{Subject: data.SubjectLoyaltyCounters, ID: id, Payload: body}); err != nil {
				return err
			}
		}
	}
	if env.Origin == "trial" {
		p.addTrial(ctx, env.BroadcasterUserID, "decoded", n)
	}
	return nil
}

func (p *Pipeline) processByOrigin(ctx context.Context, env *lane.Envelope, broadcasterID uint64) error {
	if env.Origin == "trial" {
		return p.processTrial(ctx, env, broadcasterID)
	}

	p.feedChatGate(ctx, env, broadcasterID)

	p.roster.ObserveEnvelope(broadcasterID, env)

	if p.nuke != nil {
		p.nuke.recordChat(broadcasterID, env)
	}

	views, err := p.tracedModuleViews(ctx, env.Type, broadcasterID)
	if err != nil {
		traceResult(ctx, "error")
		return err
	}

	mctx := p.leaseContext(env, broadcasterID)
	defer PutContext(mctx)

	emission := emitState{
		subject: p.laneSubject(mctx.Regress),
		env:     env,
		locale:  mctx.Locale,
	}
	emit := p.newEmit(ctx, env.BroadcasterUserID, &emission)
	started := time.Now()
	p.runTracedStages(ctx, mctx, views, emit, &emission)
	p.flushLegacyOutput(ctx, &emission)

	if mctx.Command != "" {
		p.stats.countAnswered(broadcasterID)
	}

	p.notifyObservers(ObservedEvent{
		BroadcasterID: broadcasterID,
		Type:          env.Type,
		At:            started,
		Locale:        mctx.Locale,
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
	emission.err = p.pub.Flush(ctx)
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
	return mctx
}

type emitState struct {
	subject    string
	replayBase string
	locale     string
	env        *lane.Envelope
	baseDone   bool
	ordinal    int
	needsFlush bool
	err        error
}

func (s *emitState) replayID() string {
	if !s.baseDone {
		s.replayBase = outputReplayBase(s.env)
		s.baseDone = true
	}
	return replayOutputID(s.replayBase, s.ordinal)
}

func (p *Pipeline) newEmit(ctx context.Context, partition string, state *emitState) module.Emit {
	ctx = bus.WithPublishPartition(ctx, partition)
	return func(o *module.Output) {
		if state.err != nil {
			return
		}
		if o == nil {
			return
		}
		if o.Type == "" {
			return
		}
		Translate(o)
		applyOutputLocale(o, state.locale)
		if isEmptyAction(o) {
			return
		}
		capEmitText(o)
		if p.floorSuppressed(o) {
			return
		}
		state.ordinal++
		replayID := state.replayID()
		if err := p.publishOutput(ctx, state, replayID, o); err != nil {
			state.err = err
			return
		}
		if replayID == "" {
			state.needsFlush = true
		}
	}
}

func applyOutputLocale(o *module.Output, locale string) {
	o.Locale = locale
	for i := range o.Items {
		applyOutputLocale(&o.Items[i], locale)
	}
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
	if len(p.registry.For(env.Type)) > 0 && !consumed {
		p.ensureLocale(ctx, mctx)
		p.runHandlers(ctx, views, mctx, emit)
	}
}

func (p *Pipeline) laneSubject(regress module.Regress) string {
	if regress.IsPremium() {
		return p.outgressPremium
	}
	return p.outgressStandard
}

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

func (p *Pipeline) publishOutput(ctx context.Context, state *emitState, replayID string, o *module.Output) error {
	encodeSegment := startStage(ctx, "sesame.output.encode")
	output, err := buildOutgressMessage(o)
	if err != nil {
		endStage(encodeSegment, "error")
		return err
	}
	if state.env != nil && state.env.Origin == "trial" {
		p.addTrial(ctx, state.env.BroadcasterUserID, "blocked", int64(markTrialOutput(&output, state.env.TrialGeneration)))
	}
	body, err := codec.Marshal(&output)
	if err != nil {
		endStage(encodeSegment, "error")
		return err
	}
	endStage(encodeSegment, "ok")
	if replayID == "" {
		return bus.PublishRaw(ctx, p.pub, state.subject, body)
	}
	return bus.PublishConfirmed(ctx, p.pub, bus.Publication{Subject: state.subject, ID: replayID, Payload: body})
}

func outputReplayBase(env *lane.Envelope) string {
	if env == nil || env.EventID == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(env.BroadcasterUserID + "\x00" + env.EventID))
	return "sesame:" + hex.EncodeToString(sum[:16])
}

func replayOutputID(base string, ordinal int) string {
	if base == "" {
		return ""
	}
	return base + ":" + strconv.Itoa(ordinal)
}

func (p *Pipeline) ensureLocale(ctx context.Context, mctx *module.Context) {
	if mctx.Locale != "" {
		return
	}
	if u, err := p.proj.User(ctx, mctx.BroadcasterID); err == nil {
		mctx.Locale = u.Locale
	}
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

const automodModuleName = "automod"

func automodBeta(reg *Registry) bool {
	for _, m := range reg.For(chatType) {
		if m.Name == automodModuleName {
			return m.Beta
		}
	}
	return false
}

func automodConfigFrom(views map[string]projection.ModuleView, locked bool) *automod.Config {
	mv, ok := views[automodModuleName]
	if !ok && !locked {
		return nil
	}
	var cfg *automod.Config
	if ok {
		cfg = automodConfigs.Get(mv.Configs, parseAutomodConfig)
	}
	if locked || !mv.IsEnabled {
		return disabledConfig(cfg)
	}
	return cfg
}

var automodConfigs = confcache.New[*automod.Config]()

func parseAutomodConfig(raw []byte) *automod.Config { return automod.ParseConfig(raw) }

// Copy: cfg is shared through confcache by every channel with the same blob.
func disabledConfig(cfg *automod.Config) *automod.Config {
	if cfg == nil {
		return &automod.Config{Disabled: true}
	}
	c := *cfg
	c.Disabled = true
	return &c
}

func (p *Pipeline) dispatch(ctx context.Context, mctx *module.Context, views map[string]projection.ModuleView, emit module.Emit) {
	if err := p.dispatchCommand(ctx, mctx, views, emit); err != nil {
		p.log.Error("command dispatch failed", module.BIDField(mctx.BroadcasterID), zap.Error(err))
		notice(ctx, err)
	}
}

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

func (p *Pipeline) handlerFailed(ctx context.Context, mctx *module.Context, m module.Module, err error) {
	p.log.Error("module handler failed",
		zap.String("module", moduleLabel(m)),
		zap.String("type", mctx.Env.Type),
		module.BIDField(mctx.BroadcasterID),
		zap.Error(err))
	if txn := newrelic.FromContext(ctx); txn != nil {
		txn.AddAttribute("module.failed", moduleLabel(m))
		txn.NoticeError(err)
	}
}

func traceEvent(ctx context.Context, eventType, eventLane string, broadcasterID uint64) {
	if txn := newrelic.FromContext(ctx); txn != nil {
		txn.AddAttribute("event.type", eventType)
		txn.AddAttribute("event.lane", eventLane)
		txn.AddAttribute("event.broadcaster_id", broadcasterID)
	}
}

func notice(ctx context.Context, err error) {
	if txn := newrelic.FromContext(ctx); txn != nil {
		txn.NoticeError(err)
	}
}

func (p *Pipeline) enabled(m module.Module, views map[string]projection.ModuleView, mctx *module.Context) bool {
	if m.Trial {
		mctx.Config = nil
		return mctx.Env.Origin == "trial"
	}
	if m.Beta && !mctx.Regress.IsPremium() {
		return false
	}
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

func enabledOptIn(mv projection.ModuleView, ok bool, mctx *module.Context) bool {
	if mctx.Env.Origin == "trial" {
		return false
	}
	if !ok || !mv.IsEnabled {
		return false
	}
	mctx.Config = mv.Configs
	return true
}
