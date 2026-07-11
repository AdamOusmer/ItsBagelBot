package engine

import (
	"context"
	"encoding/json"
	"math/rand/v2"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/internal/domain/invalidate"
	livekey "ItsBagelBot/internal/domain/live"
	"ItsBagelBot/internal/domain/rpc/manage"
	"ItsBagelBot/internal/projection"

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

	botID      string // the bot's own chatter id, excluded from accrual
	keyspaceDB int
	log        *zap.Logger
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
	Log        *zap.Logger
}

// NewValkeyLoyaltyClock builds the watch tick clock. proj resolves the
// broadcaster's "loyalty" ModuleView; live gates every fire and re-arm.
func NewValkeyLoyaltyClock(client valkey.Client, nc *nats.Conn, proj projection.Reader, live IsLiveChecker, reporter *LoyaltyReporter, cfg LoyaltyClockConfig) *ValkeyLoyaltyClock {
	log := cfg.Log
	if log == nil {
		log = zap.NewNop()
	}
	return &ValkeyLoyaltyClock{
		client:          client,
		proj:            proj,
		live:            live,
		reporter:        reporter,
		nc:              nc,
		chattersSubject: cfg.OutgressRPCPrefix + ".chatters.get",
		rearmSubject:    cfg.ModulesInvalidateSubject,
		botID:           cfg.BotUserID,
		keyspaceDB:      cfg.KeyspaceDB,
		log:             log,
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
		if err := json.Unmarshal(msg.Data, &dto); err != nil {
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
	idStr := strings.TrimPrefix(key, loyaltyTickKeyPrefix)
	broadcasterID, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil || broadcasterID == 0 {
		return
	}
	go s.fire(ctx, idStr, broadcasterID)
}

// fire claims one tick fleet-wide, re-validates live state and module config,
// accrues one tick over the current chatter list, then re-arms at the exact
// interval. A failed chatters fetch still re-arms — it must not stop the
// clock for the rest of the stream.
func (s *ValkeyLoyaltyClock) fire(ctx context.Context, idStr string, broadcasterID uint64) {
	claimKey := loyaltyTickClaimPrefix + idStr
	got, err := s.client.Do(ctx, s.client.B().Set().Key(claimKey).Value("1").Nx().
		ExSeconds(int64(loyaltyTickClaimTTL.Seconds())).Build()).ToString()
	if err != nil || got != "OK" {
		return
	}

	live, err := s.live.IsLive(ctx, broadcasterID)
	if err != nil || !live {
		return // stream ended: stay stopped until the next stream.online
	}
	cfg, enabled := loyaltyModuleConfig(ctx, s.proj, broadcasterID)
	if !enabled {
		return // module disabled since arming: drop, don't re-arm
	}

	s.accrue(ctx, broadcasterID, cfg)

	// Exact interval on re-arm: the first fire's jitter set the phase.
	err = s.client.Do(ctx, s.client.B().Set().Key(loyaltyTickKey(broadcasterID)).Value("1").Nx().
		ExSeconds(int64(watchTickInterval.Seconds())).Build()).Error()
	if err != nil && !valkey.IsValkeyNil(err) {
		s.log.Warn("loyalty: failed to re-arm watch tick", zap.Uint64("broadcaster_id", broadcasterID), zap.Error(err))
	}
}

// accrue lists the channel's chatters and hands each one the tick's watch
// seconds and points. Failures skip this tick's accrual (loss-tolerant).
func (s *ValkeyLoyaltyClock) accrue(ctx context.Context, broadcasterID uint64, cfg LoyaltyModuleConfig) {
	chatters, err := s.fetchChatters(ctx, broadcasterID)
	if err != nil {
		s.log.Warn("loyalty: chatters fetch failed, skipping tick",
			zap.Uint64("broadcaster_id", broadcasterID), zap.Error(err))
		return
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

	body, err := json.Marshal(manage.ChattersRequest{BroadcasterID: strconv.FormatUint(broadcasterID, 10)})
	if err != nil {
		return nil, err
	}
	msg, err := s.nc.RequestWithContext(ctx, s.chattersSubject, body)
	if err != nil {
		return nil, err
	}
	var reply manage.ChattersReply
	if err := json.Unmarshal(msg.Data, &reply); err != nil {
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
