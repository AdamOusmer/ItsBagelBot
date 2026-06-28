package worker

import (
	"context"
	"sync"
	"time"

	"ItsBagelBot/app/outgress/internal/channels"
	"ItsBagelBot/app/outgress/internal/twitch"
	"ItsBagelBot/internal/domain/rpc/manage"

	"go.uber.org/zap"
)

const (
	modStatusTTL         = 24 * time.Hour
	modCheckTimeout      = 15 * time.Second
	modCheckErrorBackoff = 5 * time.Minute
	maxConcurrentChecks  = 4
)

// ModVerifier keeps moderator discovery off the latency-sensitive chat path.
// Checks are bounded per process, collapsed per broadcaster, and guarded by a
// Valkey lock so the four outgress replicas do not all perform the same
// paginated Twitch lookup.
type ModVerifier struct {
	registry modRegistry
	twitch   moderatorClient
	botID    string
	owner    string
	log      *zap.Logger

	mu      sync.Mutex
	pending map[string]struct{}
	backoff map[string]time.Time
	slots   chan struct{}
	wg      sync.WaitGroup
	closed  bool
}

type modRegistry interface {
	AcquireModCheckLock(context.Context, string, string, time.Duration) (bool, error)
	ReleaseModCheckLock(context.Context, string, string) error
	SetMod(context.Context, string, bool) error
}

type moderatorClient interface {
	HasUserToken() bool
	IsModerator(context.Context, string, string) (bool, error)
}

func NewModVerifier(registry *channels.Registry, tw *twitch.Client, botID, owner string, log *zap.Logger) *ModVerifier {
	return newModVerifier(registry, tw, botID, owner, log)
}

func newModVerifier(registry modRegistry, tw moderatorClient, botID, owner string, log *zap.Logger) *ModVerifier {
	return &ModVerifier{
		registry: registry,
		twitch:   tw,
		botID:    botID,
		owner:    owner,
		log:      log,
		pending:  make(map[string]struct{}),
		backoff:  make(map[string]time.Time),
		slots:    make(chan struct{}, maxConcurrentChecks),
	}
}

// Status returns immediately from the last known state. A missing or stale
// state schedules a refresh, but safely uses the non-mod allowance until that
// background check completes.
func (v *ModVerifier) Status(ch manage.Channel, found bool, broadcasterID, senderID string) bool {
	if found && !ch.ModCheckedAt.IsZero() && time.Since(ch.ModCheckedAt) < modStatusTTL {
		return ch.IsMod
	}
	v.Schedule(broadcasterID, senderID)
	return found && ch.IsMod
}

// Schedule requests a refresh regardless of the cached timestamp. This is
// used by go-live events, while Status handles the ordinary chat-path TTL.
func (v *ModVerifier) Schedule(broadcasterID, senderID string) {
	if v == nil || broadcasterID == "" || !v.twitch.HasUserToken() {
		return
	}
	botID := v.botID
	if botID == "" {
		botID = senderID
	}
	if botID == "" {
		return
	}

	now := time.Now()
	v.mu.Lock()
	if v.closed {
		v.mu.Unlock()
		return
	}
	if _, ok := v.pending[broadcasterID]; ok || now.Before(v.backoff[broadcasterID]) {
		v.mu.Unlock()
		return
	}
	select {
	case v.slots <- struct{}{}:
		v.pending[broadcasterID] = struct{}{}
	default:
		v.mu.Unlock()
		return
	}
	v.wg.Add(1)
	v.mu.Unlock()

	go v.verify(broadcasterID, botID)
}

// Close stops accepting refreshes and waits for the bounded in-flight checks.
func (v *ModVerifier) Close() {
	v.mu.Lock()
	v.closed = true
	v.mu.Unlock()
	v.wg.Wait()
}

func (v *ModVerifier) verify(broadcasterID, botID string) {
	failed := false
	defer func() {
		defer v.wg.Done()
		<-v.slots
		v.mu.Lock()
		delete(v.pending, broadcasterID)
		if failed {
			v.backoff[broadcasterID] = time.Now().Add(modCheckErrorBackoff)
		} else {
			delete(v.backoff, broadcasterID)
		}
		v.mu.Unlock()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), modCheckTimeout)
	defer cancel()

	got, err := v.registry.AcquireModCheckLock(ctx, broadcasterID, v.owner, modCheckErrorBackoff)
	if err != nil {
		failed = true
		v.log.Warn("mod status lock failed", zap.String("broadcaster_id", broadcasterID), zap.Error(err))
		return
	}
	if !got {
		// Another replica is checking or is holding the distributed error
		// backoff. Avoid turning every chat message into another lock probe.
		failed = true
		return
	}

	// On failure the lock is deliberately left until its TTL expires, providing
	// a fleet-wide backoff. A successful check releases it immediately.
	isMod, err := v.twitch.IsModerator(ctx, botID, broadcasterID)
	if err != nil {
		failed = true
		v.log.Warn("mod status verification failed; preserving last known state",
			zap.String("broadcaster_id", broadcasterID), zap.Error(err))
		return
	}
	if err := v.registry.SetMod(ctx, broadcasterID, isMod); err != nil {
		failed = true
		v.log.Warn("failed to persist mod status", zap.String("broadcaster_id", broadcasterID), zap.Error(err))
		return
	}

	releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer releaseCancel()
	if err := v.registry.ReleaseModCheckLock(releaseCtx, broadcasterID, v.owner); err != nil {
		v.log.Warn("failed to release mod status lock", zap.String("broadcaster_id", broadcasterID), zap.Error(err))
	}
	v.log.Debug("mod status refreshed",
		zap.String("broadcaster_id", broadcasterID), zap.Bool("is_mod", isMod))
}
