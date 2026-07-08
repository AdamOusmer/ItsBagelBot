package engine

import (
	"context"
	"encoding/json"
	"math/rand/v2"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/invalidate"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/moderation"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/nats-io/nats.go"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

// timersModuleName is the ModuleView key the dashboard's Timers tab writes:
// its enable toggle is the master switch, and its Configs blob carries the
// list of repeating messages this store reads.
const timersModuleName = "timers"

// timerKeyPrefix is the Valkey schedule key for one timer: EX'd to its own
// interval. Its expiry, not a running goroutine, is the clock — the same
// idiom ValkeyLiveStore uses for its key-expiry re-check (live_valkey.go).
const timerKeyPrefix = "timer:"

// timerClaimPrefix guards one expiry so only one replica of the fleet fires
// it; nested under timerKeyPrefix the way live:recheck: nests under live:, so
// onExpired can tell its own claim keys apart from a schedule key.
const timerClaimPrefix = "timer:claim:"

const timerClaimTTL = 5 * time.Second

// minTimerInterval floors a configured interval. The dashboard already clamps
// to 60s; this only guards a hand-crafted RPC call from arming a tight
// expire/fire/re-arm loop.
const minTimerInterval = 30 * time.Second

// timerFirstFireJitter caps the random phase offset added to a timer's FIRST
// interval at arm time. It desynchronizes timers armed in the same instant
// (every timer at stream.online, or a freshly added same-interval one) so they
// don't all expire together and post as one wall of chat. Only the first fire
// is offset; re-arms use the exact interval, so the configured cadence holds.
// The offset is floored at the smaller of this cap and the interval itself, so
// a 30s timer never gets a 60s first wait.
const timerFirstFireJitter = 30 * time.Second

// rearmTimeout bounds one mid-stream rearm (IsLive read + ArmAll) triggered off
// a modules cache-invalidation, so a stalled Valkey/projector never pins the
// goroutine the rearm watcher spawns per message.
const rearmTimeout = 5 * time.Second

// timerDef is one broadcaster-authored repeating chat message.
type timerDef struct {
	ID       string `json:"id"`
	Message  string `json:"message"`
	Interval int    `json:"intervalSeconds"`
	Enabled  bool   `json:"enabled"`
}

// timersConfig is the "timers" module's Configs blob.
type timersConfig struct {
	Timers []timerDef `json:"timers"`
}

// ValkeyTimerStore arms a broadcaster's repeating messages for the length of
// one stream and fires them off Valkey key expiry: stream.online SETs one key
// per enabled timer (EX = its interval), stream.offline deletes them, and each
// expiry re-checks live state + config, posts the message, and re-arms.
//
// A missed expiry notification (the watcher's pub/sub connection drops and
// reconnects) silently stalls that one timer until the next stream.online —
// there is no reconciliation sweep. Given the stream-only requirement and the
// modest stakes (a scheduled chat line, not a payment), a rare stall until the
// next stream is an accepted trade for not running a second polling mechanism
// alongside this one.
type ValkeyTimerStore struct {
	client valkey.Client
	pub    message.Publisher
	proj   projection.Reader
	live   IsLiveChecker

	// nc + rearmSubject drive the mid-stream arm-on-save path: a subscription to
	// the modules cache-invalidation subject that re-arms a live broadcaster's
	// timers the moment their dashboard save lands (StartRearmWatcher). nil nc or
	// empty subject leaves the watcher disabled — timers still arm on
	// stream.online, just not mid-stream.
	nc           *nats.Conn
	rearmSubject string

	outgressPremium  string
	outgressStandard string

	keyspaceDB int
	log        *zap.Logger
}

// TimersConfig wires a ValkeyTimerStore.
type TimersConfig struct {
	OutgressPremiumSubject  string
	OutgressStandardSubject string
	// KeyspaceDB is the Valkey db the expiry watcher listens on (default 0).
	KeyspaceDB int
	// NC is the core NATS connection the rearm watcher subscribes on. nil leaves
	// the watcher disabled.
	NC *nats.Conn
	// ModulesInvalidateSubject is the modules-scope cache-invalidation subject
	// (e.g. "bagel.cache.invalidate.modules") the rearm watcher listens on. Empty
	// leaves the watcher disabled.
	ModulesInvalidateSubject string
	// Log is the store's logger; a nil Log defaults to a no-op.
	Log *zap.Logger
}

// NewValkeyTimerStore builds a timer store. proj resolves a broadcaster's
// "timers" ModuleView and tier (for the outgress lane); live gates every fire
// and re-arm to the broadcaster's current live state.
func NewValkeyTimerStore(client valkey.Client, pub message.Publisher, proj projection.Reader, live IsLiveChecker, cfg TimersConfig) *ValkeyTimerStore {
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
	}
}

func timerKey(broadcasterID uint64, timerID string) string {
	return timerKeyPrefix + strconv.FormatUint(broadcasterID, 10) + ":" + timerID
}

// ArmAll SETs one Valkey key per enabled timer of an enabled "timers" module,
// each EX'd to its interval plus a small random phase offset (armJittered) so
// timers armed together here don't all fire in the same instant. NX means a
// timer already counting down is left alone, so this only starts the ones not
// yet armed — the freshly added timer on a mid-stream rearm, or every timer on
// a fresh stream.online.
func (s *ValkeyTimerStore) ArmAll(ctx context.Context, broadcasterID uint64) {
	cfg, ok := s.config(ctx, broadcasterID)
	if !ok {
		return
	}
	for _, td := range cfg.Timers {
		s.armJittered(ctx, broadcasterID, td)
	}
}

// RearmIfLive arms a broadcaster's enabled timers mid-stream, but only while the
// broadcaster is actually live. It backs the arm-on-save path: adding, enabling
// or editing a timer from the dashboard changes the broadcaster's modules blob,
// which is what StartRearmWatcher turns into this call — so a timer created
// after stream.online still starts counting down this session instead of
// sitting idle until the next stream.
//
// ArmAll's NX SET makes repeated calls safe: a timer already counting down keeps
// its clock, only a not-yet-armed one (the freshly added timer) gets a key. When
// the broadcaster is offline this is a no-op — the next stream.online arms
// everything fresh, and arming an offline broadcaster's keys would only set keys
// onExpired drops unfired on their first expiry.
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

// clampInterval turns a configured interval (seconds) into a Duration floored
// at minTimerInterval, the single place the floor is applied.
func clampInterval(seconds int) time.Duration {
	d := time.Duration(seconds) * time.Second
	if d < minTimerInterval {
		d = minTimerInterval
	}
	return d
}

// arm re-arms a timer at exactly its interval — the onExpired path, which must
// preserve the cadence set by the first (jittered) fire.
func (s *ValkeyTimerStore) arm(ctx context.Context, broadcasterID uint64, td timerDef) {
	s.armAfter(ctx, broadcasterID, td, clampInterval(td.Interval))
}

// armJittered arms a timer's first schedule key at its interval plus a random
// phase offset (see timerFirstFireJitter), so timers armed in the same instant
// don't all expire together. It is the ArmAll (stream.online + mid-stream
// rearm) path; onExpired re-arms via arm() at the exact interval, so the offset
// shifts only when the timer first fires this session, not its cadence after.
func (s *ValkeyTimerStore) armJittered(ctx context.Context, broadcasterID uint64, td timerDef) {
	interval := clampInterval(td.Interval)
	spread := interval
	if spread > timerFirstFireJitter {
		spread = timerFirstFireJitter
	}
	offset := time.Duration(rand.Int64N(int64(spread.Seconds())+1)) * time.Second
	s.armAfter(ctx, broadcasterID, td, interval+offset)
}

// armAfter SETs one timer's schedule key EX'd to ex. NX leaves an
// already-counting-down key alone: ArmAll must not reset the clock on a
// redelivered stream.online (or a mid-stream rearm), and onExpired's re-arm
// must not clobber a fresh key a concurrent ArmAll just set (the narrow race of
// a stream ending and restarting within the same instant).
func (s *ValkeyTimerStore) armAfter(ctx context.Context, broadcasterID uint64, td timerDef, ex time.Duration) {
	if !td.Enabled || td.ID == "" {
		return
	}
	err := s.client.Do(ctx, s.client.B().Set().Key(timerKey(broadcasterID, td.ID)).Value("1").Nx().ExSeconds(int64(ex.Seconds())).Build()).Error()
	if err != nil && !valkey.IsValkeyNil(err) {
		s.log.Warn("timers: failed to arm", zap.Uint64("broadcaster_id", broadcasterID), zap.String("timer_id", td.ID), zap.Error(err))
	}
}

// DisarmAll deletes every configured timer's key so a stream that just ended
// stops immediately rather than waiting out its longest-running timer's
// interval. Best-effort: a config read failure leaves the keys to expire and
// self-stop on their own (onExpired's live check fails the same way).
func (s *ValkeyTimerStore) DisarmAll(ctx context.Context, broadcasterID uint64) {
	cfg, ok := s.config(ctx, broadcasterID)
	if !ok {
		return
	}
	for _, td := range cfg.Timers {
		if td.ID == "" {
			continue
		}
		if err := s.client.Do(ctx, s.client.B().Del().Key(timerKey(broadcasterID, td.ID)).Build()).Error(); err != nil {
			s.log.Warn("timers: failed to disarm", zap.Uint64("broadcaster_id", broadcasterID), zap.String("timer_id", td.ID), zap.Error(err))
		}
	}
}

// config resolves the broadcaster's "timers" ModuleView, reporting false when
// the module is missing, disabled, unconfigured, or the read failed.
func (s *ValkeyTimerStore) config(ctx context.Context, broadcasterID uint64) (timersConfig, bool) {
	views, err := s.proj.Modules(ctx, broadcasterID)
	if err != nil {
		s.log.Warn("timers: failed to read module views", zap.Uint64("broadcaster_id", broadcasterID), zap.Error(err))
		return timersConfig{}, false
	}
	for _, v := range views {
		if v.Name != timersModuleName {
			continue
		}
		if !v.IsEnabled || len(v.Configs) == 0 {
			return timersConfig{}, false
		}
		var cfg timersConfig
		if err := json.Unmarshal(v.Configs, &cfg); err != nil {
			s.log.Warn("timers: bad config", zap.Uint64("broadcaster_id", broadcasterID), zap.Error(err))
			return timersConfig{}, false
		}
		return cfg, true
	}
	return timersConfig{}, false
}

// StartRearmWatcher subscribes to the modules cache-invalidation subject and
// arms a broadcaster's timers the moment a dashboard save changes their modules
// blob (RearmIfLive gates it to a live broadcaster). This is the arm-on-save
// path: without it, arming happens only on stream.online, so a timer added
// after the broadcaster went live would sit idle until their next stream.
//
// It rides the same "modules" invalidation event sesame's projection cache
// already consumes, so it needs no new NATS subject or account grant. A missed
// message (subscription drop) is self-correcting: the timer still arms on the
// next stream.online, matching the store's best-effort posture. The
// subscription is async — each message spawns a bounded goroutine so a slow
// IsLive/ArmAll never stalls delivery of the next invalidation. It runs until
// ctx is cancelled.
func (s *ValkeyTimerStore) StartRearmWatcher(ctx context.Context) {
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

// StartExpiryWatcher subscribes to Valkey key-expiry notifications and fires
// (or drops) each timer whose key expires. It runs until ctx is cancelled and
// reconnects on a dropped subscription, mirroring ValkeyLiveStore's watcher.
// Requires notify-keyspace-events to include expired-key events (Ex), already
// on for the live-recheck watcher this shares the deployment with.
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

// onExpired handles one expired key. It ignores anything that is not a
// schedule key, claims the expiry so only one replica acts on it, then
// re-validates live state + config before firing and re-arming: a timer
// paused, deleted, or whose stream ended between arming and this expiry is
// dropped instead of fired.
func (s *ValkeyTimerStore) onExpired(ctx context.Context, key string) {
	if !strings.HasPrefix(key, timerKeyPrefix) || strings.HasPrefix(key, timerClaimPrefix) {
		return
	}
	rest := strings.TrimPrefix(key, timerKeyPrefix)
	parts := strings.SplitN(rest, ":", 2)
	if len(parts) != 2 {
		return
	}
	broadcasterID, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil || broadcasterID == 0 {
		return
	}
	timerID := parts[1]

	// One replica per expiry fires the timer.
	claimKey := timerClaimPrefix + parts[0] + ":" + timerID
	got, err := s.client.Do(ctx, s.client.B().Set().Key(claimKey).Value("1").Nx().ExSeconds(int64(timerClaimTTL.Seconds())).Build()).ToString()
	if err != nil || got != "OK" {
		return
	}

	live, err := s.live.IsLive(ctx, broadcasterID)
	if err != nil || !live {
		return // stream ended: stay stopped until the next stream.online arms fresh
	}

	cfg, ok := s.config(ctx, broadcasterID)
	if !ok {
		return // module disabled or unreadable since arming: drop, don't re-arm
	}
	td, ok := findTimer(cfg.Timers, timerID)
	if !ok || !td.Enabled {
		return // this timer was disabled or deleted since arming: drop, don't re-arm
	}

	s.fire(ctx, broadcasterID, td)
	s.arm(ctx, broadcasterID, td)
}

func findTimer(timers []timerDef, id string) (timerDef, bool) {
	for _, td := range timers {
		if td.ID == id {
			return td, true
		}
	}
	return timerDef{}, false
}

// fire posts one timer's message the same way the pipeline posts any module
// Output: the send-time floor guard first (the config was already floor-
// checked at save time; this only covers drift), then whichever premium/
// standard lane the broadcaster's own tier resolves to.
func (s *ValkeyTimerStore) fire(ctx context.Context, broadcasterID uint64, td timerDef) {
	if term, hit := moderation.CheckFloor(td.Message); hit {
		s.log.Warn("timers: suppressed message carrying floor content",
			zap.Uint64("broadcaster_id", broadcasterID), zap.String("timer_id", td.ID), zap.String("term", term))
		return
	}

	idStr := strconv.FormatUint(broadcasterID, 10)
	subject := s.outgressStandard
	if u, err := s.proj.User(ctx, broadcasterID); err == nil && u.Premium() {
		subject = s.outgressPremium
	}

	body, err := buildOutgress(&module.Output{Type: outgress.TypeChat, BroadcasterID: idStr, Text: td.Message})
	if err != nil {
		s.log.Warn("timers: failed to build outgress message", zap.Uint64("broadcaster_id", broadcasterID), zap.String("timer_id", td.ID), zap.Error(err))
		return
	}
	if err := bus.PublishRaw(ctx, s.pub, subject, body); err != nil {
		s.log.Warn("timers: failed to publish", zap.Uint64("broadcaster_id", broadcasterID), zap.String("timer_id", td.ID), zap.Error(err))
	}
}
