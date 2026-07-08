package engine

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/moderation"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"

	"github.com/ThreeDotsLabs/watermill/message"
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
// each EX'd to its own interval — the broadcaster's stream just went online,
// so every timer starts its countdown fresh.
func (s *ValkeyTimerStore) ArmAll(ctx context.Context, broadcasterID uint64) {
	cfg, ok := s.config(ctx, broadcasterID)
	if !ok {
		return
	}
	for _, td := range cfg.Timers {
		s.arm(ctx, broadcasterID, td)
	}
}

// arm SETs one timer's schedule key. NX leaves an already-counting-down key
// alone: ArmAll must not reset the clock on a redelivered stream.online, and
// onExpired's re-arm must not clobber a fresh key a concurrent ArmAll just set
// (the narrow race of a stream ending and restarting within the same instant).
func (s *ValkeyTimerStore) arm(ctx context.Context, broadcasterID uint64, td timerDef) {
	if !td.Enabled || td.ID == "" {
		return
	}
	interval := time.Duration(td.Interval) * time.Second
	if interval < minTimerInterval {
		interval = minTimerInterval
	}
	err := s.client.Do(ctx, s.client.B().Set().Key(timerKey(broadcasterID, td.ID)).Value("1").Nx().ExSeconds(int64(interval.Seconds())).Build()).Error()
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
