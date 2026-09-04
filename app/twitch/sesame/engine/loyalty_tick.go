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

	"ItsBagelBot/internal/domain/invalidate"
	livekey "ItsBagelBot/internal/domain/live"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/domain/rpc/manage"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats.go"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

// The watch tick is the loyalty module's viewtime clock: one Valkey key per
// live broadcaster with an enabled loyalty module, EX'd to the tick interval.
// Its expiry (the timers_valkey idiom) claims one replica, lists the channel's
// connected chatters through the outgress chatters RPC, hands every one of
// them the tick's watch seconds + points via the loyalty reporter, and
// re-arms. Stream.offline deletes the key; a reconciler sweep recovers a
// silently stalled tick mid-stream.
//
// Failure handling is deliberate, because a failed attempt credits nobody:
// a transient chatters/live error re-arms short so a blip costs viewers a
// minute, not a whole window, and a persistent failure (a dead grant, a lost
// moderator seat) escalates to an Error-level log once per streak instead of
// spinning invisibly forever. Every twelfth successful tick additionally asks
// outgress for a positive Twitch re-check of the live state, bounding how long
// a lost stream.offline can keep paying phantom watch time from the warm
// live:<id> key to roughly an hour instead of that key's full 12h TTL.
const (
	loyaltyTickKeyPrefix   = "loyaltick:"
	loyaltyTickClaimPrefix = "loyaltick:claim:"

	// watchTickInterval is one watch accrual period. Five minutes matches the
	// going rate of chat loyalty systems and keeps the Helix chatters spend at
	// one listing per live channel per five minutes.
	watchTickInterval = 5 * time.Minute

	// watchTickJitter spreads first fires so channels armed in the same
	// instant (a mass stream.online after an ingress restart) don't line their
	// chatters fetches up.
	watchTickJitter = time.Minute
	// loyaltyTickClaimTTL covers one tick's work: a paginated chatters fetch
	// (10s handler budget) plus the reporter hand-off.
	loyaltyTickClaimTTL = 30 * time.Second

	// chattersRPCTimeout sits above the outgress handler's own 10s budget.
	chattersRPCTimeout = 12 * time.Second

	loyaltyRearmTimeout = 5 * time.Second

	// watchTickQuickRetry is the re-arm delay after a failed attempt whose
	// cause may already be gone (a timed-out chatters fetch, an errored live
	// read). Retrying inside a minute bounds what one blip costs the channel's
	// viewers; watchTickQuickRetries caps that spend so a hard-down
	// dependency falls back to the normal cadence after two fast attempts.
	watchTickQuickRetry   = time.Minute
	watchTickQuickRetries = 2

	// watchTickReconfirmEvery is how many successful fires sit between
	// positive live-state re-confirms (~1h at the normal interval). The warm
	// live:<id> key is trusted for its whole 12h TTL otherwise, so without
	// this a lost stream.offline keeps paying phantom watch time until the
	// key expires. Cost: one system-lane Helix call per live channel per hour.
	watchTickReconfirmEvery = 12

	loyaltyReconfirmTimeout = 5 * time.Second

	// loyaltyEscalationLevel is where a failure streak stops being Warn noise:
	// at this many consecutive failures each log line carries an actionable
	// Error (fix the bot grant's chatters scope or restore the mod seat).
	loyaltyEscalationLevel = 3

	loyaltyReconcileInterval = time.Minute
	loyaltyReconcileClaimKey = loyaltyTickClaimPrefix + "reconcile"
	loyaltyReconcileClaimTTL = 30 * time.Second
)

// ValkeyLoyaltyClock arms and fires the per-broadcaster watch tick. It shares
// the deployment's keyspace-notification config with the live and timer
// watchers.
type ValkeyLoyaltyClock struct {
	client   valkey.Client
	proj     projection.Reader
	live     IsLiveChecker
	reporter *LoyaltyReporter

	nc              *nats.Conn
	chattersSubject string // e.g. "bagel.rpc.outgress.chatters.get"
	rearmSubject    string // modules cache-invalidation subject; empty disables

	// pub + outgressSystemSubject drive the periodic positive live re-confirm
	// (see watchTickReconfirmEvery); either empty disables it.
	pub                   bus.Publisher
	outgressSystemSubject string

	botID      string // the bot's own chatter id, excluded from accrual
	keyspaceDB int
	log        *zap.Logger

	// Per-broadcaster ledgers for the failure policy and the reconfirm
	// cadence. Only live broadcasters appear, so both stay bounded by the
	// live set; Disarm drops a channel's entries with its tick key. Pod-local
	// by design: they steer retries and log levels, never accrual amounts.
	tmu      sync.Mutex
	failures map[uint64]int // consecutive failed attempts
	fires    map[uint64]int // successful fires this session
}

// LoyaltyClockConfig wires a ValkeyLoyaltyClock.
type LoyaltyClockConfig struct {
	// OutgressRPCPrefix is the outgress management RPC prefix (default
	// "bagel.rpc.outgress"); the clock appends ".chatters.get".
	OutgressRPCPrefix string
	// ModulesInvalidateSubject is the modules-scope cache-invalidation subject
	// the rearm watcher listens on. Empty leaves mid-stream enabling to the
	// reconciler sweep.
	ModulesInvalidateSubject string
	// BotUserID is the bot's own Twitch user id, skipped in the chatter list.
	BotUserID string
	// KeyspaceDB is the Valkey db the expiry watcher listens on (default 0).
	KeyspaceDB int
	// Publisher + OutgressSystemSubject publish the periodic live re-check job
	// (outgress.Message{TypeStreamStatus}) onto the outgress system lane — the
	// same request the live key's own expiry watcher sends when it expires.
	// Either empty disables the re-confirm.
	Publisher             bus.Publisher
	OutgressSystemSubject string
	Log                   *zap.Logger
}

// NewValkeyLoyaltyClock builds the watch tick clock. proj resolves the
// broadcaster's "loyalty" ModuleView; live gates every fire and re-arm.
func NewValkeyLoyaltyClock(client valkey.Client, nc *nats.Conn, proj projection.Reader, live IsLiveChecker, reporter *LoyaltyReporter, cfg LoyaltyClockConfig) *ValkeyLoyaltyClock {
	log := cfg.Log
	if log == nil {
		log = zap.NewNop()
	}
	return &ValkeyLoyaltyClock{
		client:                client,
		proj:                  proj,
		live:                  live,
		reporter:              reporter,
		nc:                    nc,
		chattersSubject:       cfg.OutgressRPCPrefix + ".chatters.get",
		rearmSubject:          cfg.ModulesInvalidateSubject,
		pub:                   cfg.Publisher,
		outgressSystemSubject: cfg.OutgressSystemSubject,
		botID:                 cfg.BotUserID,
		keyspaceDB:            cfg.KeyspaceDB,
		log:                   log,
		failures:              map[uint64]int{},
		fires:                 map[uint64]int{},
	}
}

func loyaltyTickKey(broadcasterID uint64) string {
	return loyaltyTickKeyPrefix + strconv.FormatUint(broadcasterID, 10)
}

// Arm starts (or leaves counting) the broadcaster's watch tick, if their
// loyalty module is enabled. NX keeps a redelivered stream.online from
// resetting a running tick; the first fire carries a phase offset.
func (s *ValkeyLoyaltyClock) Arm(ctx context.Context, broadcasterID uint64) {
	if broadcasterID == 0 {
		return
	}
	if _, enabled := loyaltyModuleConfig(ctx, s.proj, broadcasterID); !enabled {
		return
	}
	offset := time.Duration(rand.Int64N(int64(watchTickJitter.Seconds())+1)) * time.Second
	err := s.client.Do(ctx, s.client.B().Set().Key(loyaltyTickKey(broadcasterID)).Value("1").Nx().
		ExSeconds(int64((watchTickInterval + offset).Seconds())).Build()).Error()
	if err != nil && !valkey.IsValkeyNil(err) {
		s.log.Warn("loyalty: failed to arm watch tick", zap.Uint64("broadcaster_id", broadcasterID), zap.Error(err))
	}
}

// Disarm stops the tick immediately (stream.offline).
func (s *ValkeyLoyaltyClock) Disarm(ctx context.Context, broadcasterID uint64) {
	if broadcasterID == 0 {
		return
	}
	// The ledgers only describe the current live session, so they go with the
	// tick key: the next stream starts from a clean streak and cadence.
	s.tmu.Lock()
	delete(s.failures, broadcasterID)
	delete(s.fires, broadcasterID)
	s.tmu.Unlock()
	if err := s.client.Do(ctx, s.client.B().Del().Key(loyaltyTickKey(broadcasterID)).Build()).Error(); err != nil {
		s.log.Warn("loyalty: failed to disarm watch tick", zap.Uint64("broadcaster_id", broadcasterID), zap.Error(err))
	}
}

// rearmIfLive arms mid-stream (module enabled from the dashboard while live).
// Arm re-checks the module config; the NX SET leaves a running tick alone.
func (s *ValkeyLoyaltyClock) rearmIfLive(ctx context.Context, broadcasterID uint64) {
	if broadcasterID == 0 {
		return
	}
	live, err := s.live.IsLive(ctx, broadcasterID)
	if err != nil || !live {
		return
	}
	s.Arm(ctx, broadcasterID)
}

// StartExpiryWatcher subscribes to Valkey key-expiry notifications and fires
// each tick whose key expires, reconnecting on a dropped subscription. Same
// idiom (and notify-keyspace-events requirement) as the live and timer
// watchers.
func (s *ValkeyLoyaltyClock) StartExpiryWatcher(ctx context.Context) {
	channel := "__keyevent@" + strconv.Itoa(s.keyspaceDB) + "__:expired"
	s.log.Info("loyalty: watch tick expiry watcher starting", zap.String("channel", channel))

	for ctx.Err() == nil {
		err := s.client.Receive(ctx, s.client.B().Subscribe().Channel(channel).Build(), func(msg valkey.PubSubMessage) {
			s.onExpired(ctx, msg.Message)
		})
		if ctx.Err() != nil {
			return
		}
		s.log.Warn("loyalty: expiry watcher dropped, reconnecting", zap.Error(err))
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

// StartRearmWatcher mirrors the timer store's arm-on-save path: a modules
// cache invalidation (a dashboard save) re-arms a live broadcaster's tick, so
// enabling loyalty mid-stream starts accruing this session.
func (s *ValkeyLoyaltyClock) StartRearmWatcher(ctx context.Context) {
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
		go func() {
			rctx, cancel := context.WithTimeout(context.Background(), loyaltyRearmTimeout)
			defer cancel()
			s.rearmIfLive(rctx, id)
		}()
	})
	if err != nil {
		s.log.Error("loyalty: failed to start rearm watcher", zap.String("subject", s.rearmSubject), zap.Error(err))
		return
	}
	s.log.Info("loyalty: rearm watcher starting", zap.String("subject", s.rearmSubject))
	go func() {
		<-ctx.Done()
		_ = sub.Unsubscribe()
	}()
}

// StartReconciler periodically re-arms every live broadcaster's tick,
// recovering one that silently stalled (a lost expiry notification), exactly
// like the timer reconciler. NX arming keeps a running tick untouched.
func (s *ValkeyLoyaltyClock) StartReconciler(ctx context.Context) {
	ticker := time.NewTicker(loyaltyReconcileInterval)
	defer ticker.Stop()
	s.log.Info("loyalty: reconciler starting", zap.Duration("interval", loyaltyReconcileInterval))
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.reconcile(ctx)
		}
	}
}

func (s *ValkeyLoyaltyClock) reconcile(ctx context.Context) {
	got, err := s.client.Do(ctx, s.client.B().Set().Key(loyaltyReconcileClaimKey).Value("1").Nx().
		ExSeconds(int64(loyaltyReconcileClaimTTL.Seconds())).Build()).ToString()
	if err != nil || got != "OK" {
		return // another replica owns this tick, or the claim write failed
	}
	for _, id := range s.liveBroadcasters(ctx) {
		s.rearmIfLive(ctx, id)
	}
}

// liveBroadcasters SCANs the live-key set, skipping the recheck guard keys
// that share the live: prefix (the timer reconciler's scan, duplicated rather
// than shared so neither store grows a dependency on the other).
func (s *ValkeyLoyaltyClock) liveBroadcasters(ctx context.Context) []uint64 {
	var ids []uint64
	cursor := uint64(0)
	for {
		entry, err := s.client.Do(ctx, s.client.B().Scan().Cursor(cursor).Match(livekey.KeyPrefix+"*").Count(200).Build()).AsScanEntry()
		if err != nil {
			s.log.Warn("loyalty: reconcile scan failed", zap.Error(err))
			return ids
		}
		for _, k := range entry.Elements {
			if id, ok := parseLiveKey(k); ok {
				ids = append(ids, id)
			}
		}
		cursor = entry.Cursor
		if cursor == 0 {
			return ids
		}
	}
}

// parseLiveKey extracts the broadcaster id from one live:<id> key, rejecting
// the live:recheck: guard keys that share the prefix.
func parseLiveKey(key string) (uint64, bool) {
	if strings.HasPrefix(key, recheckKeyPrefix) {
		return 0, false
	}
	id, err := strconv.ParseUint(strings.TrimPrefix(key, livekey.KeyPrefix), 10, 64)
	return id, err == nil && id != 0
}

// onExpired handles one expired key. The real work runs on its own goroutine:
// a tick's chatters fetch can take seconds, and the expiry watcher's pub/sub
// callback must never block other keys' notifications behind it. The claim
// inside fire keeps the fan-out cheap — every replica spawns, one proceeds.
func (s *ValkeyLoyaltyClock) onExpired(ctx context.Context, key string) {
	if !strings.HasPrefix(key, loyaltyTickKeyPrefix) || strings.HasPrefix(key, loyaltyTickClaimPrefix) {
		return
	}
	broadcasterID, err := strconv.ParseUint(strings.TrimPrefix(key, loyaltyTickKeyPrefix), 10, 64)
	if err != nil || broadcasterID == 0 {
		return
	}
	go s.fire(ctx, broadcasterID)
}

// fire claims one tick fleet-wide, re-validates live state and module config,
// accrues one tick over the current chatter list, then re-arms. A failed
// attempt still re-arms — it must not stop the clock for the rest of the
// stream — but re-arms short when the cause may be gone (see settle).
func (s *ValkeyLoyaltyClock) fire(ctx context.Context, broadcasterID uint64) {
	claimKey := loyaltyTickClaimPrefix + strconv.FormatUint(broadcasterID, 10)
	got, err := s.client.Do(ctx, s.client.B().Set().Key(claimKey).Value("1").Nx().
		ExSeconds(int64(loyaltyTickClaimTTL.Seconds())).Build()).ToString()
	if err != nil || got != "OK" {
		return
	}

	live, err := s.live.IsLive(ctx, broadcasterID)
	if err != nil {
		// The read failed rather than answering offline; retry soon instead of
		// sitting out until the reconciler's next sweep.
		s.rearmAfterFailure(ctx, broadcasterID, err)
		return
	}
	if !live {
		return // stream ended: stay stopped until the next stream.online
	}
	cfg, enabled := loyaltyModuleConfig(ctx, s.proj, broadcasterID)
	if !enabled {
		return // module disabled since arming: drop, don't re-arm
	}

	if err := s.accrue(ctx, broadcasterID, cfg); err != nil {
		s.rearmAfterFailure(ctx, broadcasterID, err)
		return
	}
	s.rearm(ctx, broadcasterID, s.settleSuccess(ctx, broadcasterID))
}

// rearmAfterFailure records a failed attempt and re-arms at its policy delay.
func (s *ValkeyLoyaltyClock) rearmAfterFailure(ctx context.Context, broadcasterID uint64, cause error) {
	s.rearm(ctx, broadcasterID, s.settleFailure(broadcasterID, cause))
}

// rearm sets the tick key's next expiry. NX keeps a reconciler arm that raced
// ahead from being reset; a lost race just means the key lives a little
// longer. A failed re-arm is survivable either way — the reconciler recovers
// the tick within its sweep interval.
func (s *ValkeyLoyaltyClock) rearm(ctx context.Context, broadcasterID uint64, ttl time.Duration) {
	err := s.client.Do(ctx, s.client.B().Set().Key(loyaltyTickKey(broadcasterID)).Value("1").Nx().
		ExSeconds(int64(ttl.Seconds())).Build()).Error()
	if err != nil && !valkey.IsValkeyNil(err) {
		s.log.Warn("loyalty: failed to re-arm watch tick", zap.Uint64("broadcaster_id", broadcasterID), zap.Error(err))
	}
}

// accrue lists the channel's chatters and hands each one the tick's watch
// seconds and points. A returned error skips this tick's accrual
// (loss-tolerant) and feeds the caller's failure policy.
func (s *ValkeyLoyaltyClock) accrue(ctx context.Context, broadcasterID uint64, cfg LoyaltyModuleConfig) error {
	chatters, err := s.fetchChatters(ctx, broadcasterID)
	if err != nil {
		return err
	}
	points := cfg.EffectiveWatchPointsPerTick()
	seconds := uint64(watchTickInterval.Seconds())
	for _, ch := range chatters {
		if viewerID, ok := s.chatterViewerID(ch.ID); ok {
			s.reporter.Earn(broadcasterID, viewerID, ch.Login, "", points, seconds)
		}
	}
	s.log.Debug("loyalty: watch tick accrued",
		zap.Uint64("broadcaster_id", broadcasterID), zap.Int("chatters", len(chatters)))
	return nil
}

// chatterViewerID parses one chatter's id, dropping the bot's own account (it
// sits in every chat it serves and must not farm points).
func (s *ValkeyLoyaltyClock) chatterViewerID(id string) (uint64, bool) {
	if id == s.botID {
		return 0, false
	}
	viewerID, err := strconv.ParseUint(id, 10, 64)
	return viewerID, err == nil && viewerID != 0
}

// fetchChatters asks outgress for the current chatter list. A missing-scope
// reply (bot demodded / stale grant) is surfaced as an error and skipped
// upstream.
func (s *ValkeyLoyaltyClock) fetchChatters(ctx context.Context, broadcasterID uint64) ([]manage.Chatter, error) {
	ctx, cancel := context.WithTimeout(ctx, chattersRPCTimeout)
	defer cancel()

	body, err := codec.Marshal(manage.ChattersRequest{BroadcasterID: strconv.FormatUint(broadcasterID, 10)})
	if err != nil {
		return nil, err
	}
	msg, err := bus.RequestWithContext(ctx, s.nc, s.chattersSubject, body)
	if err != nil {
		return nil, err
	}
	var reply manage.ChattersReply
	if err := codec.Unmarshal(msg.Data, &reply); err != nil {
		return nil, err
	}
	if reply.Error != "" {
		return nil, &chattersError{message: reply.Error, missingScope: reply.MissingScope}
	}
	return reply.Chatters, nil
}

// chattersError carries the reply's failure detail for the skip log line.
type chattersError struct {
	message      string
	missingScope bool
}

func (e *chattersError) Error() string {
	if e.missingScope {
		return "chatters unavailable (missing scope or not a moderator): " + e.message
	}
	return e.message
}

// settleSuccess folds a good tick into the ledgers: it clears the failure
// streak, advances the reconfirm cadence (publishing the periodic live
// re-check when due) and answers the next re-arm delay — the exact interval,
// since the first fire's jitter set the phase.
func (s *ValkeyLoyaltyClock) settleSuccess(ctx context.Context, broadcasterID uint64) time.Duration {
	confirm := false
	s.tmu.Lock()
	delete(s.failures, broadcasterID)
	s.fires[broadcasterID]++
	confirm = s.fires[broadcasterID]%watchTickReconfirmEvery == 0
	s.tmu.Unlock()
	if confirm {
		s.requestLiveRecheck(ctx, broadcasterID)
	}
	return watchTickInterval
}

// settleFailure records one failed attempt and answers its re-arm delay.
// Persistent trouble escalates the log level once per streak — at
// loyaltyEscalationLevel every line becomes an actionable Error naming the
// two real-world causes (stale grant scope, lost moderator seat) instead of
// an identical Warn repeating forever.
func (s *ValkeyLoyaltyClock) settleFailure(broadcasterID uint64, cause error) time.Duration {
	s.tmu.Lock()
	n := s.failures[broadcasterID] + 1
	s.failures[broadcasterID] = n
	s.tmu.Unlock()

	fields := []zap.Field{
		zap.Uint64("broadcaster_id", broadcasterID),
		zap.Int("consecutive_failures", n),
		zap.Error(cause),
	}
	if n < loyaltyEscalationLevel {
		s.log.Warn("loyalty: watch tick attempt failed", fields...)
	} else {
		s.log.Error("loyalty: watch tick keeps failing; check the bot grant's chatters scope or the channel's moderator seat", fields...)
	}
	return rearmAfterFailure(n)
}

// rearmAfterFailure is the pure policy behind settleFailure: the first couple
// of failures retry inside a minute (a blip then costs viewers one minute,
// not one window), anything longer-standing waits out the normal interval so
// a hard-down dependency stops spending Helix calls. The reconciler still
// recovers a stopped tick either way.
func rearmAfterFailure(failures int) time.Duration {
	if failures <= watchTickQuickRetries {
		return watchTickQuickRetry
	}
	return watchTickInterval
}

// watchTickIdentity is the stable dedup identity for the accrual bucket that
// contains at on one channel: the channel id plus the interval index of the
// event's OWN timestamp. Both replicas firing the same key expiry derive the
// same string (the bucket comes from the payload's clock, never the local
// one), yet the next legitimate window indexes differently — so the dedup
// guard collapses a refired expiry without suppressing the following accrual.
func watchTickIdentity(broadcasterID uint64, at time.Time) string {
	buf := GetBuf()
	buf = append(buf, "wtick:"...)
	buf = strconv.AppendUint(buf, broadcasterID, 10)
	buf = append(buf, ':')
	buf = strconv.AppendInt(buf, at.Unix()/int64(watchTickInterval/time.Second), 10)
	key := string(buf)
	PutBuf(buf)
	return key
}

// requestLiveRecheck asks outgress to resolve the broadcaster's live state
// against Twitch and write it back — outgress.Message{TypeStreamStatus}, the
// same job the live key's own expiry watcher sends. Best-effort: a failed
// publish just means the next window's confirm lands instead.
func (s *ValkeyLoyaltyClock) requestLiveRecheck(ctx context.Context, broadcasterID uint64) {
	if s.pub == nil || s.outgressSystemSubject == "" {
		return
	}
	id := strconv.FormatUint(broadcasterID, 10)
	body, err := codec.Marshal(outgress.StreamStatusJob{BroadcasterID: id})
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, loyaltyReconfirmTimeout)
	defer cancel()
	if err := bus.PublishJSON(ctx, s.pub, s.outgressSystemSubject, outgress.Message{
		Type:          outgress.TypeStreamStatus,
		BroadcasterID: id,
		Payload:       body,
	}); err != nil {
		s.log.Debug("loyalty: live re-check publish failed",
			zap.Uint64("broadcaster_id", broadcasterID), zap.Error(err))
	}
}
