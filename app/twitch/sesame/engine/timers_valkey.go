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

	gatedCache *cache.Keyed[uint64, bool]

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
		gatedCache:       cache.NewKeyed[uint64, bool](gatedCacheCapacity, gatedCacheTTL, gatedCacheKeyFn),
		now:              time.Now,
	}
}

const gatedCacheCapacity int64 = 4096

const gatedCacheTTL = 10 * time.Minute

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

func (r timerRef) markKey() string {
	return cache.PairKey(timerAuxPrefix+"mark:", r.broadcasterID, r.id)
}

func (r timerRef) firesKey() string {
	return cache.PairKey(timerAuxPrefix+"fires:", r.broadcasterID, r.id)
}

type armedTimer struct {
	ref timerRef
	def timerDef
}

func linesKey(broadcasterID uint64) string {
	return cache.UserKey(timerAuxPrefix+"lines:", broadcasterID)
}

func (s *ValkeyTimerStore) ArmAll(ctx context.Context, broadcasterID uint64) {
	cfg, ok := s.config(ctx, broadcasterID)
	if !ok {
		return
	}
	for _, td := range cfg.Timers {
		s.armOne(ctx, armedTimer{ref: timerRef{broadcasterID: broadcasterID, id: td.ID}, def: td})
	}
}

func (s *ValkeyTimerStore) armOne(ctx context.Context, at armedTimer) {
	if !at.def.Enabled || at.ref.id == "" {
		return
	}
	var fires int64
	if clampFireCap(at.def.MaxFires) > 0 {
		fires = s.fireCount(ctx, at.ref)
	}
	if stopped(at.def, fires, s.now()) {
		return
	}
	s.armJittered(ctx, at)
	if isGated(at.def) {
		s.seedWatermark(ctx, at)
	}
}

func (s *ValkeyTimerStore) RearmIfLive(ctx context.Context, broadcasterID uint64) {
	if broadcasterID == 0 {
		return
	}
	live, err := s.live.IsLive(ctx, broadcasterID)
	if err != nil || !live {
		return
	}
	s.ArmAll(ctx, broadcasterID)
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
		s.RearmIfLive(ctx, id)
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
		if td.ID == "" {
			continue
		}
		ref := timerRef{broadcasterID: broadcasterID, id: td.ID}
		s.delAuxKey(ctx, ref, ref.scheduleKey())
		s.delAuxKey(ctx, ref, ref.markKey())
		s.delAuxKey(ctx, ref, ref.firesKey())
		s.badEndsAtWarned.Delete(ref)
		s.blankFireWarned.Delete(ref)
	}
	s.delAuxKey(ctx, timerRef{broadcasterID: broadcasterID}, linesKey(broadcasterID))
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
		s.gatedCache.Invalidate(id)
		go func() {
			rctx, cancel := context.WithTimeout(context.Background(), rearmTimeout)
			defer cancel()
			s.RearmIfLive(rctx, id)
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

	live, err := s.live.IsLive(ctx, ref.broadcasterID)
	if err != nil || !live {
		return
	}

	at, ok := s.resolveArmed(ctx, ref)
	if !ok {
		return
	}
	s.tick(ctx, at)
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
	if clampFireCap(at.def.MaxFires) > 0 {
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
	lines := s.linesCount(ctx, at.ref.broadcasterID)
	mark := s.watermark(ctx, at.ref)
	return gatePasses(at.def, lines, mark)
}

func (s *ValkeyTimerStore) recordFire(ctx context.Context, at armedTimer) {
	if _, err := pkg_valkey.Incr(ctx, s.client, at.ref.firesKey(), timerAuxTTL); err != nil {
		s.log.Warn("timers: failed to record fire", module.BIDField(at.ref.broadcasterID), zap.String("timer_id", at.ref.id), zap.Error(err))
	}
	if isGated(at.def) {
		s.setWatermark(ctx, at)
	}
}

func (s *ValkeyTimerStore) fireCount(ctx context.Context, ref timerRef) int64 {
	n, err := pkg_valkey.GetInt(ctx, s.client, ref.firesKey())
	if err != nil {
		s.log.Warn("timers: failed to read fire count", module.BIDField(ref.broadcasterID), zap.String("timer_id", ref.id), zap.Error(err))
	}
	return n
}

func (s *ValkeyTimerStore) linesCount(ctx context.Context, broadcasterID uint64) int64 {
	n, err := pkg_valkey.GetInt(ctx, s.client, linesKey(broadcasterID))
	if err != nil {
		s.log.Warn("timers: failed to read chat line count", module.BIDField(broadcasterID), zap.Error(err))
	}
	return n
}

func (s *ValkeyTimerStore) watermark(ctx context.Context, ref timerRef) int64 {
	n, err := pkg_valkey.GetInt(ctx, s.client, ref.markKey())
	if err != nil {
		s.log.Warn("timers: failed to read gate watermark", module.BIDField(ref.broadcasterID), zap.String("timer_id", ref.id), zap.Error(err))
	}
	return n
}

func (s *ValkeyTimerStore) setWatermark(ctx context.Context, at armedTimer) {
	lines := s.linesCount(ctx, at.ref.broadcasterID)
	err := s.client.Do(ctx, s.client.B().Set().Key(at.ref.markKey()).
		Value(strconv.FormatInt(lines, 10)).Ex(timerAuxTTL).Build()).Error()
	if err != nil {
		s.log.Warn("timers: failed to set gate watermark", module.BIDField(at.ref.broadcasterID), zap.String("timer_id", at.ref.id), zap.Error(err))
	}
}

func (s *ValkeyTimerStore) seedWatermark(ctx context.Context, at armedTimer) {
	lines := s.linesCount(ctx, at.ref.broadcasterID)
	err := s.client.Do(ctx, s.client.B().Set().Key(at.ref.markKey()).
		Value(strconv.FormatInt(lines, 10)).Nx().Ex(timerAuxTTL).Build()).Error()
	if err != nil && !valkey.IsValkeyNil(err) {
		s.log.Warn("timers: failed to seed gate watermark", module.BIDField(at.ref.broadcasterID), zap.String("timer_id", at.ref.id), zap.Error(err))
	}
}

func (s *ValkeyTimerStore) CountChatLine(ctx context.Context, broadcasterID uint64) {
	if broadcasterID == 0 || !s.hasGatedTimer(ctx, broadcasterID) {
		return
	}
	go func() {
		actx, cancel := context.WithTimeout(context.Background(), countChatLineTimeout)
		defer cancel()
		if _, err := pkg_valkey.Incr(actx, s.client, linesKey(broadcasterID), timerAuxTTL); err != nil {
			s.log.Debug("timers: chat line count failed", module.BIDField(broadcasterID), zap.Error(err))
		}
	}()
}

func (s *ValkeyTimerStore) hasGatedTimer(ctx context.Context, broadcasterID uint64) bool {
	gated, err := s.gatedCache.GetOrLoad(ctx, broadcasterID, func(ctx context.Context) (bool, error) {
		cfg, ok := s.config(ctx, broadcasterID)
		if !ok {
			return false, nil
		}
		for _, td := range cfg.Timers {
			if td.Enabled && isGated(td) {
				return true, nil
			}
		}
		return false, nil
	})
	return err == nil && gated
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
	locale := ""
	if u, err := s.proj.User(ctx, at.ref.broadcasterID); err == nil {
		if u.Premium() {
			subject = s.outgressPremium
		}
		locale = u.Locale
	}

	text := s.timerText(ctx, at, locale)
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

func (s *ValkeyTimerStore) timerText(ctx context.Context, at armedTimer, locale string) string {
	if s.pipeline == nil {
		return at.def.Message
	}
	message := defangTimerSlashLines(at.def.Message)
	run := timerRun{ref: at.ref, locale: locale, firedAt: s.now()}
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
