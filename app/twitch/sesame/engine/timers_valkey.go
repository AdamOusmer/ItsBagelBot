// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"math/rand/v2"
	"strconv"
	"strings"
	"sync"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/invalidate"
	livekey "ItsBagelBot/internal/domain/live"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/moderation"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/cache"
	"ItsBagelBot/pkg/codec"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/nats-io/nats.go"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

const timersModuleName = "timers"

const timerKeyPrefix = "timer:"

const timerClaimPrefix = "timer:claim:"

const timerClaimTTL = 5 * time.Second

const minTimerInterval = 30 * time.Second

const timerFirstFireJitter = 30 * time.Second

const rearmTimeout = 5 * time.Second

const timerFireTimeout = 10 * time.Second

const reconcileInterval = time.Minute

const reconcileClaimKey = timerClaimPrefix + "reconcile"

const reconcileClaimTTL = 30 * time.Second

const timerAuxPrefix = "timerx:"

const timerAuxTTL = 48 * time.Hour

type timerDef struct {
	ID           string `json:"id"`
	Message      string `json:"message"`
	Interval     int    `json:"intervalSeconds"`
	Enabled      bool   `json:"enabled"`
	MinChatLines int    `json:"minChatLines"`
	MaxFires     int    `json:"maxFiresPerStream"`
	EndsAt       string `json:"endsAt"`

	ChatWindowMinutes int  `json:"chatWindowMinutes"`
	AllowOffline      bool `json:"allowOffline"`
}

type timersConfig struct {
	Timers []timerDef `json:"timers"`
}

type ValkeyTimerStore struct {
	client valkey.Client
	pub    bus.Publisher
	proj   projection.Reader
	live   IsLiveChecker

	nc           *nats.Conn
	rearmSubject string

	outgressPremium  string
	outgressStandard string

	keyspaceDB int
	log        *zap.Logger

	flagsCache *cache.Keyed[uint64, chatFlags]

	rearmThrottle *cache.Keyed[uint64, bool]

	now func() time.Time

	badEndsAtWarned sync.Map

	blankFireWarned sync.Map

	pipeline *Pipeline
}

// Call before starting the watchers: fire reads the field unsynchronized.
func (s *ValkeyTimerStore) WirePipeline(p *Pipeline) {
	s.pipeline = p
}

type TimersConfig struct {
	OutgressPremiumSubject   string
	OutgressStandardSubject  string
	KeyspaceDB               int
	NC                       *nats.Conn
	ModulesInvalidateSubject string
	Log                      *zap.Logger
}

func NewValkeyTimerStore(client valkey.Client, pub bus.Publisher, proj projection.Reader, live IsLiveChecker, cfg TimersConfig) *ValkeyTimerStore {
	log := cfg.Log
	if log == nil {
		log = zap.NewNop()
	}
	return &ValkeyTimerStore{
		client:           client,
		pub:              pub,
		proj:             proj,
		live:             live,
		nc:               cfg.NC,
		rearmSubject:     cfg.ModulesInvalidateSubject,
		outgressPremium:  cfg.OutgressPremiumSubject,
		outgressStandard: cfg.OutgressStandardSubject,
		keyspaceDB:       cfg.KeyspaceDB,
		log:              log,
		flagsCache:       cache.NewKeyed[uint64, chatFlags](gatedCacheCapacity, gatedCacheTTL, gatedCacheKeyFn),
		rearmThrottle:    cache.NewKeyed[uint64, bool](gatedCacheCapacity, chatRearmInterval, gatedCacheKeyFn),
		now:              time.Now,
	}
}

const gatedCacheCapacity int64 = 4096

const gatedCacheTTL = 10 * time.Minute

const chatRearmInterval = time.Minute

func gatedCacheKeyFn(broadcasterID uint64) string {
	return strconv.FormatUint(broadcasterID, 10)
}

type timerRef struct {
	broadcasterID uint64
	id            string
}

func (r timerRef) scheduleKey() string {
	return cache.PairKey(timerKeyPrefix, r.broadcasterID, r.id)
}

func (r timerRef) firesKey() string {
	return cache.PairKey(timerAuxPrefix+"fires:", r.broadcasterID, r.id)
}

type armedTimer struct {
	ref timerRef
	def timerDef
}

func chatLog(broadcasterID uint64) pkg_valkey.RecentLog {
	return pkg_valkey.RecentLog{
		Key:  cache.UserKey(timerAuxPrefix+"chat:", broadcasterID),
		Keep: maxGateLines,
		TTL:  maxChatWindowMinutes * time.Minute,
	}
}

func (s *ValkeyTimerStore) ArmAll(ctx context.Context, broadcasterID uint64) {
	s.eachTimer(ctx, broadcasterID, func(at armedTimer) { s.armOne(ctx, at) })
}

func (s *ValkeyTimerStore) ArmOnline(ctx context.Context, broadcasterID uint64) {
	s.eachTimer(ctx, broadcasterID, func(at armedTimer) {
		if at.def.AllowOffline && at.ref.id != "" {
			s.delAuxKey(ctx, at.ref, at.ref.firesKey())
		}
		s.armOne(ctx, at)
	})
}

func (s *ValkeyTimerStore) armOffline(ctx context.Context, broadcasterID uint64) {
	s.eachTimer(ctx, broadcasterID, func(at armedTimer) {
		if at.def.AllowOffline {
			s.armOne(ctx, at)
		}
	})
}

func (s *ValkeyTimerStore) eachTimer(ctx context.Context, broadcasterID uint64, step func(armedTimer)) {
	cfg, ok := s.config(ctx, broadcasterID)
	if !ok {
		return
	}
	for _, td := range cfg.Timers {
		step(armedTimer{ref: timerRef{broadcasterID: broadcasterID, id: td.ID}, def: td})
	}
}

func (s *ValkeyTimerStore) armOne(ctx context.Context, at armedTimer) {
	if !at.def.Enabled || at.ref.id == "" {
		return
	}
	var fires int64
	if clampCount(at.def.MaxFires, maxFireCap) > 0 {
		fires = s.fireCount(ctx, at.ref)
	}
	if stopped(at.def, fires, s.now()) {
		return
	}
	s.armJittered(ctx, at)
}

func (s *ValkeyTimerStore) Rearm(ctx context.Context, broadcasterID uint64) {
	if broadcasterID == 0 {
		return
	}
	live, err := s.live.IsLive(ctx, broadcasterID)
	switch {
	case err != nil:
	case live:
		s.ArmAll(ctx, broadcasterID)
	default:
		s.armOffline(ctx, broadcasterID)
	}
}

func (s *ValkeyTimerStore) StartReconciler(ctx context.Context) {
	ticker := time.NewTicker(reconcileInterval)
	defer ticker.Stop()
	s.log.Info("timers: reconciler starting", zap.Duration("interval", reconcileInterval))
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.reconcile(ctx)
		}
	}
}

func (s *ValkeyTimerStore) reconcile(ctx context.Context) {
	if won, err := pkg_valkey.ClaimOnce(ctx, s.client, reconcileClaimKey, reconcileClaimTTL); err != nil || !won {
		return
	}
	for _, id := range s.liveBroadcasters(ctx) {
		s.Rearm(ctx, id)
	}
}

func (s *ValkeyTimerStore) liveBroadcasters(ctx context.Context) []uint64 {
	var ids []uint64
	cursor := uint64(0)
	for {
		entry, err := s.client.Do(ctx, s.client.B().Scan().Cursor(cursor).Match(livekey.KeyPrefix+"*").Count(200).Build()).AsScanEntry()
		if err != nil {
			s.log.Warn("timers: reconcile scan failed", zap.Error(err))
			return ids
		}
		for _, k := range entry.Elements {
			if strings.HasPrefix(k, recheckKeyPrefix) {
				continue
			}
			if id, err := strconv.ParseUint(strings.TrimPrefix(k, livekey.KeyPrefix), 10, 64); err == nil && id != 0 {
				ids = append(ids, id)
			}
		}
		cursor = entry.Cursor
		if cursor == 0 {
			return ids
		}
	}
}

func clampInterval(seconds int) time.Duration {
	d := time.Duration(seconds) * time.Second
	if d < minTimerInterval {
		d = minTimerInterval
	}
	return d
}

func (s *ValkeyTimerStore) arm(ctx context.Context, at armedTimer) {
	s.armAfter(ctx, at, clampInterval(at.def.Interval))
}

func (s *ValkeyTimerStore) armJittered(ctx context.Context, at armedTimer) {
	interval := clampInterval(at.def.Interval)
	spread := interval
	if spread > timerFirstFireJitter {
		spread = timerFirstFireJitter
	}
	offset := time.Duration(rand.Int64N(int64(spread.Seconds())+1)) * time.Second
	s.armAfter(ctx, at, interval+offset)
}

func (s *ValkeyTimerStore) armAfter(ctx context.Context, at armedTimer, ex time.Duration) {
	if !at.def.Enabled || at.ref.id == "" {
		return
	}
	err := s.client.Do(ctx, s.client.B().Set().Key(at.ref.scheduleKey()).Value("1").Nx().ExSeconds(int64(ex.Seconds())).Build()).Error()
	if err != nil && !valkey.IsValkeyNil(err) {
		s.log.Warn("timers: failed to arm", module.BIDField(at.ref.broadcasterID), zap.String("timer_id", at.ref.id), zap.Error(err))
	}
}

func (s *ValkeyTimerStore) DisarmAll(ctx context.Context, broadcasterID uint64) {
	cfg, ok := s.config(ctx, broadcasterID)
	if !ok {
		return
	}
	for _, td := range cfg.Timers {
		if td.ID != "" {
			s.disarmOne(ctx, armedTimer{ref: timerRef{broadcasterID: broadcasterID, id: td.ID}, def: td})
		}
	}
}

func (s *ValkeyTimerStore) disarmOne(ctx context.Context, at armedTimer) {
	s.delAuxKey(ctx, at.ref, at.ref.firesKey())
	s.badEndsAtWarned.Delete(at.ref)
	s.blankFireWarned.Delete(at.ref)
	if at.def.AllowOffline {
		s.armOne(ctx, at)
		return
	}
	s.delAuxKey(ctx, at.ref, at.ref.scheduleKey())
}

func (s *ValkeyTimerStore) delAuxKey(ctx context.Context, ref timerRef, key string) {
	if err := s.client.Do(ctx, s.client.B().Del().Key(key).Build()).Error(); err != nil {
		s.log.Warn("timers: failed to disarm", module.BIDField(ref.broadcasterID), zap.String("timer_id", ref.id), zap.String("key", key), zap.Error(err))
	}
}

func (s *ValkeyTimerStore) config(ctx context.Context, broadcasterID uint64) (timersConfig, bool) {
	view, state, err := ModuleLookup{Proj: s.proj, BroadcasterID: broadcasterID, Name: timersModuleName, Absent: ModuleOff}.Resolve(ctx)
	if err != nil {
		s.log.Warn("timers: failed to read module views", module.BIDField(broadcasterID), zap.Error(err))
	}
	if state != ModuleOn {
		return timersConfig{}, false
	}
	if len(view.Configs) == 0 {
		return timersConfig{}, false
	}
	var cfg timersConfig
	if err := codec.Unmarshal(view.Configs, &cfg); err != nil {
		s.log.Warn("timers: bad config", module.BIDField(broadcasterID), zap.Error(err))
		return timersConfig{}, false
	}
	return cfg, true
}

func (s *ValkeyTimerStore) StartRearmWatcher(ctx context.Context) {
	if s.nc == nil || s.rearmSubject == "" {
		return
	}
	sub, err := s.nc.Subscribe(s.rearmSubject, func(msg *nats.Msg) {
		var dto invalidate.DTO
		if err := codec.Unmarshal(msg.Data, &dto); err != nil {
			return
		}
		id, err := strconv.ParseUint(dto.BroadcasterID, 10, 64)
		if err != nil || id == 0 {
			return
		}
		s.flagsCache.Invalidate(id)
		go func() {
			rctx, cancel := context.WithTimeout(context.Background(), rearmTimeout)
			defer cancel()
			s.Rearm(rctx, id)
		}()
	})
	if err != nil {
		s.log.Error("timers: failed to start rearm watcher", zap.String("subject", s.rearmSubject), zap.Error(err))
		return
	}
	s.log.Info("timers: rearm watcher starting", zap.String("subject", s.rearmSubject))
	go func() {
		<-ctx.Done()
		_ = sub.Unsubscribe()
	}()
}

func (s *ValkeyTimerStore) StartExpiryWatcher(ctx context.Context) {
	channel := "__keyevent@" + strconv.Itoa(s.keyspaceDB) + "__:expired"
	s.log.Info("timers: expiry watcher starting", zap.String("channel", channel))

	for ctx.Err() == nil {
		err := s.client.Receive(ctx, s.client.B().Subscribe().Channel(channel).Build(), func(msg valkey.PubSubMessage) {
			s.onExpired(ctx, msg.Message)
		})
		if ctx.Err() != nil {
			return
		}
		s.log.Warn("timers: expiry watcher dropped, reconnecting", zap.Error(err))
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

func (s *ValkeyTimerStore) onExpired(ctx context.Context, key string) {
	ref, ok := parseTimerKey(key)
	if !ok {
		return
	}

	claimKey := timerClaimPrefix + strconv.FormatUint(ref.broadcasterID, 10) + ":" + ref.id
	if won, err := pkg_valkey.ClaimOnce(ctx, s.client, claimKey, timerClaimTTL); err != nil || !won {
		return
	}

	at, ok := s.resolveArmed(ctx, ref)
	if !ok || !s.mayTick(ctx, at) {
		return
	}
	s.tick(ctx, at)
}

func (s *ValkeyTimerStore) mayTick(ctx context.Context, at armedTimer) bool {
	if at.def.AllowOffline {
		return true
	}
	live, err := s.live.IsLive(ctx, at.ref.broadcasterID)
	return err == nil && live
}

func parseTimerKey(key string) (timerRef, bool) {
	if !strings.HasPrefix(key, timerKeyPrefix) || strings.HasPrefix(key, timerClaimPrefix) {
		return timerRef{}, false
	}
	rest := strings.TrimPrefix(key, timerKeyPrefix)
	parts := strings.SplitN(rest, ":", 2)
	if len(parts) != 2 {
		return timerRef{}, false
	}
	broadcasterID, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil || broadcasterID == 0 {
		return timerRef{}, false
	}
	return timerRef{broadcasterID: broadcasterID, id: parts[1]}, true
}

func (s *ValkeyTimerStore) resolveArmed(ctx context.Context, ref timerRef) (armedTimer, bool) {
	cfg, ok := s.config(ctx, ref.broadcasterID)
	if !ok {
		return armedTimer{}, false
	}
	td, ok := findTimer(cfg.Timers, ref.id)
	if !ok || !td.Enabled {
		return armedTimer{}, false
	}
	return armedTimer{ref: ref, def: td}, true
}

func (s *ValkeyTimerStore) tick(ctx context.Context, at armedTimer) {
	if s.hasStopped(ctx, at) {
		return
	}
	if !s.gateOpen(ctx, at) {
		s.arm(ctx, at)
		return
	}
	s.fireBounded(at)
	s.recordFire(ctx, at)
	s.arm(ctx, at)
}

func (s *ValkeyTimerStore) fireBounded(at armedTimer) {
	go func() {
		fctx, cancel := context.WithTimeout(context.Background(), timerFireTimeout)
		defer cancel()
		s.fire(fctx, at)
	}()
}

func (s *ValkeyTimerStore) hasStopped(ctx context.Context, at armedTimer) bool {
	s.warnUnparsableEndsAt(at)
	var fires int64
	if clampCount(at.def.MaxFires, maxFireCap) > 0 {
		fires = s.fireCount(ctx, at.ref)
	}
	return stopped(at.def, fires, s.now())
}

func (s *ValkeyTimerStore) warnUnparsableEndsAt(at armedTimer) {
	if at.def.EndsAt == "" {
		return
	}
	if _, ok := parseEndsAt(at.def.EndsAt); ok {
		return
	}
	if _, alreadyWarned := s.badEndsAtWarned.LoadOrStore(at.ref, struct{}{}); alreadyWarned {
		return
	}
	s.log.Warn("timers: unparsable end date, treating as never-ends",
		module.BIDField(at.ref.broadcasterID), zap.String("timer_id", at.ref.id), zap.String("ends_at", at.def.EndsAt))
}

func (s *ValkeyTimerStore) gateOpen(ctx context.Context, at armedTimer) bool {
	if !isGated(at.def) {
		return true
	}
	return gatePasses(at.def, s.recentLines(ctx, at))
}

func (s *ValkeyTimerStore) recordFire(ctx context.Context, at armedTimer) {
	if _, err := pkg_valkey.Incr(ctx, s.client, at.ref.firesKey(), timerAuxTTL); err != nil {
		s.log.Warn("timers: failed to record fire", module.BIDField(at.ref.broadcasterID), zap.String("timer_id", at.ref.id), zap.Error(err))
	}
}

func (s *ValkeyTimerStore) fireCount(ctx context.Context, ref timerRef) int64 {
	n, err := pkg_valkey.GetInt(ctx, s.client, ref.firesKey())
	if err != nil {
		s.log.Warn("timers: failed to read fire count", module.BIDField(ref.broadcasterID), zap.String("timer_id", ref.id), zap.Error(err))
	}
	return n
}

func (s *ValkeyTimerStore) recentLines(ctx context.Context, at armedTimer) int64 {
	since := s.now().Add(-chatWindow(at.def))
	n, err := pkg_valkey.CountRecentSince(ctx, s.client, chatLog(at.ref.broadcasterID).Key, since)
	if err != nil {
		s.log.Warn("timers: failed to read chat line count", module.BIDField(at.ref.broadcasterID), zap.String("timer_id", at.ref.id), zap.Error(err))
	}
	return n
}

type chatFlags struct {
	gated   bool
	offline bool
}

func (s *ValkeyTimerStore) CountChatLine(ctx context.Context, broadcasterID uint64) {
	if broadcasterID == 0 {
		return
	}
	flags := s.chatFlags(ctx, broadcasterID)
	if !flags.gated && !flags.offline {
		return
	}
	go func() {
		actx, cancel := context.WithTimeout(context.Background(), countChatLineTimeout)
		defer cancel()
		if flags.gated {
			s.recordChatLine(actx, broadcasterID)
		}
		if flags.offline {
			s.throttledRearm(actx, broadcasterID)
		}
	}()
}

func (s *ValkeyTimerStore) recordChatLine(ctx context.Context, broadcasterID uint64) {
	if err := pkg_valkey.RecordRecent(ctx, s.client, chatLog(broadcasterID), s.now()); err != nil {
		s.log.Debug("timers: chat line count failed", module.BIDField(broadcasterID), zap.Error(err))
	}
}

func (s *ValkeyTimerStore) throttledRearm(ctx context.Context, broadcasterID uint64) {
	first := false
	_, _ = s.rearmThrottle.GetOrLoad(ctx, broadcasterID, func(context.Context) (bool, error) {
		first = true
		return true, nil
	})
	if first {
		s.Rearm(ctx, broadcasterID)
	}
}

func (s *ValkeyTimerStore) chatFlags(ctx context.Context, broadcasterID uint64) chatFlags {
	flags, err := s.flagsCache.GetOrLoad(ctx, broadcasterID, func(ctx context.Context) (chatFlags, error) {
		cfg, ok := s.config(ctx, broadcasterID)
		if !ok {
			return chatFlags{}, nil
		}
		return flagsOf(cfg.Timers), nil
	})
	if err != nil {
		return chatFlags{}
	}
	return flags
}

func flagsOf(timers []timerDef) chatFlags {
	var flags chatFlags
	for _, td := range timers {
		if !td.Enabled {
			continue
		}
		flags.gated = flags.gated || isGated(td)
		flags.offline = flags.offline || td.AllowOffline
	}
	return flags
}

const countChatLineTimeout = 5 * time.Second

func findTimer(timers []timerDef, id string) (timerDef, bool) {
	for _, td := range timers {
		if td.ID == id {
			return td, true
		}
	}
	return timerDef{}, false
}

func (s *ValkeyTimerStore) fire(ctx context.Context, at armedTimer) {
	if term, hit := moderation.CheckFloor(at.def.Message); hit {
		s.log.Warn("timers: suppressed message carrying floor content",
			module.BIDField(at.ref.broadcasterID), zap.String("timer_id", at.ref.id), zap.String("term", term))
		return
	}

	idStr := strconv.FormatUint(at.ref.broadcasterID, 10)
	subject := s.outgressStandard
	user := projection.User{}
	if u, err := s.proj.User(ctx, at.ref.broadcasterID); err == nil {
		if u.Premium() {
			subject = s.outgressPremium
		}
		user = u
	}

	text := s.timerText(ctx, at, user)
	outputs := s.timerOutputs(idStr, text)
	if len(outputs) == 0 {
		s.warnBlankFire(at)
		return
	}
	for _, out := range outputs {
		s.publishFired(ctx, at, subject, out)
	}
}

func (s *ValkeyTimerStore) warnBlankFire(at armedTimer) {
	if _, already := s.blankFireWarned.LoadOrStore(at.ref, struct{}{}); already {
		return
	}
	s.log.Warn("timers: expansion left nothing to post, fire slot spent",
		module.BIDField(at.ref.broadcasterID), zap.String("timer_id", at.ref.id))
}

func (s *ValkeyTimerStore) timerText(ctx context.Context, at armedTimer, user projection.User) string {
	if s.pipeline == nil {
		return at.def.Message
	}
	message := defangTimerSlashLines(at.def.Message)
	run := timerRun{ref: at.ref, locale: user.Locale, user: user, firedAt: s.now()}
	return s.pipeline.expandTimerText(ctx, run, message)
}

func (s *ValkeyTimerStore) timerOutputs(idStr, text string) []module.Output {
	out := &module.Output{Type: outgress.TypeChat, BroadcasterID: idStr, Text: text}
	if s.pipeline == nil {
		return []module.Output{*out}
	}
	return s.pipeline.chatLines(out)
}

func (s *ValkeyTimerStore) publishFired(ctx context.Context, at armedTimer, subject string, out module.Output) {
	body, err := buildOutgress(&out)
	if err != nil {
		s.log.Warn("timers: failed to build outgress message", module.BIDField(at.ref.broadcasterID), zap.String("timer_id", at.ref.id), zap.Error(err))
		return
	}
	if err := bus.PublishRaw(ctx, s.pub, subject, body); err != nil {
		s.log.Warn("timers: failed to publish", module.BIDField(at.ref.broadcasterID), zap.String("timer_id", at.ref.id), zap.Error(err))
	}
}
