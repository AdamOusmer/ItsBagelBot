package module

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/internal/domain/invalidate"
	livekey "ItsBagelBot/internal/domain/live"
	"ItsBagelBot/internal/domain/outgress"
	projectorrpc "ItsBagelBot/internal/domain/rpc/projector"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/cache"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/nats-io/nats.go"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

// recheckKeyPrefix guards the per-broadcaster Twitch re-check so that, when many
// worker replicas all see the same key expire, only one fires the outgress job.
// It deliberately sorts under the shared live: prefix; onExpired skips it.
const recheckKeyPrefix = "live:recheck:"

// LiveStore answers and maintains a broadcaster's live state. Reads are
// served from an in-process cache fronting the shared Valkey key, with a
// projector RPC fallback on a cold key; writes flow from the stream events the
// worker already consumes.
type LiveStore interface {
	IsLive(ctx context.Context, broadcasterID uint64) (bool, error)
	SetLive(ctx context.Context, broadcasterID uint64) error
	ClearLive(ctx context.Context, broadcasterID uint64) error
}

// LiveConfig wires the Valkey-backed live store.
type LiveConfig struct {
	// TTL bounds how long a live key survives without a refresh; on expiry the
	// key-event watcher re-checks Twitch (via outgress) rather than letting the
	// state silently drop, so a stream longer than TTL is re-confirmed.
	TTL time.Duration
	// CacheTTL is the in-process cache lifetime for the resolved live bool.
	CacheTTL time.Duration
	// ProjectorLiveSubject is the projector RPC asked on a cold Valkey key.
	ProjectorLiveSubject string
	// OutgressSystemSubject is the outgress system lane the re-check job rides.
	OutgressSystemSubject string
	// CacheInvalidatePrefix is the core-NATS prefix used to fan a live change to
	// every worker replica (subject = prefix + ".live").
	CacheInvalidatePrefix string
	// KeyspaceDB is the Valkey db the key-event watcher listens on (default 0).
	KeyspaceDB int
}

// ValkeyLiveStore is the default LiveStore. The same struct owns the read path
// (cache -> Valkey -> projector RPC), the write path (stream events), the
// fleet-wide invalidation, and the key-expiry re-check.
type ValkeyLiveStore struct {
	client valkey.Client
	nc     *nats.Conn
	pub    message.Publisher
	cfg    LiveConfig
	log    *zap.Logger

	cache      *cache.Cache[bool]
	rpcTimeout time.Duration

	invalidationSub *nats.Subscription
}

// NewValkeyLiveStore builds a live store. pub publishes the re-check job onto the
// outgress system lane; nc carries the projector RPC and the invalidation fan-out.
func NewValkeyLiveStore(client valkey.Client, nc *nats.Conn, pub message.Publisher, cfg LiveConfig, log *zap.Logger) *ValkeyLiveStore {
	if cfg.CacheTTL <= 0 {
		cfg.CacheTTL = 30 * time.Second
	}
	return &ValkeyLiveStore{
		client:     client,
		nc:         nc,
		pub:        pub,
		cfg:        cfg,
		log:        log,
		cache:      cache.New[bool](cache.DefaultCapacity, cfg.CacheTTL),
		rpcTimeout: 1500 * time.Millisecond,
	}
}

func liveKey(id uint64) string { return livekey.Key(id) }

// IsLive resolves the broadcaster's live state: in-process cache, then the
// shared Valkey key, then the projector on a cold key. A live answer learned
// from the projector is written back so the key-expiry re-check applies to it.
func (s *ValkeyLiveStore) IsLive(ctx context.Context, broadcasterID uint64) (bool, error) {
	return s.cache.GetOrLoad(ctx, liveKey(broadcasterID), func(ctx context.Context) (bool, error) {
		val, err := s.client.Do(ctx, s.client.B().Get().Key(liveKey(broadcasterID)).Build()).ToString()
		if err == nil {
			return val == "1", nil
		}
		if !valkey.IsValkeyNil(err) {
			return false, err
		}

		// Cold key: ask the projector. It returns its own projected state or, on
		// its miss, escalates to Twitch and replies offline (eventual). Treat an
		// RPC failure as offline so an outage never falsely greenlights.
		reply, err := bus.RequestJSONTimeout[projectorrpc.LiveReply](
			ctx, s.nc, s.cfg.ProjectorLiveSubject,
			projectorrpc.LiveRequest{BroadcasterID: strconv.FormatUint(broadcasterID, 10)},
			s.rpcTimeout,
		)
		if err != nil {
			return false, nil
		}
		if reply.Live {
			// Cache-aside: persist a confirmed-live key so the expiry re-check
			// keeps it honest. Never write an offline marker (it would expire and
			// trigger pointless re-checks for idle channels).
			_ = s.setLiveKey(ctx, broadcasterID)
		}
		return reply.Live, nil
	})
}

// SetLive marks the broadcaster live (on stream.online) and fans the change out.
func (s *ValkeyLiveStore) SetLive(ctx context.Context, broadcasterID uint64) error {
	if err := s.setLiveKey(ctx, broadcasterID); err != nil {
		return err
	}
	s.cache.Set(liveKey(broadcasterID), true)
	s.broadcast(broadcasterID)
	return nil
}

// ClearLive drops the broadcaster's live state (on stream.offline) = invalidate.
func (s *ValkeyLiveStore) ClearLive(ctx context.Context, broadcasterID uint64) error {
	if err := s.client.Do(ctx, s.client.B().Del().Key(liveKey(broadcasterID)).Build()).Error(); err != nil {
		return err
	}
	s.cache.Invalidate(liveKey(broadcasterID))
	s.broadcast(broadcasterID)
	return nil
}

func (s *ValkeyLiveStore) setLiveKey(ctx context.Context, broadcasterID uint64) error {
	ttl := s.cfg.TTL
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}
	return s.client.Do(ctx, s.client.B().Set().Key(liveKey(broadcasterID)).Value("1").ExSeconds(int64(ttl.Seconds())).Build()).Error()
}

// broadcast fans a live change to every worker replica so their in-process
// caches drop the entry immediately, backing the short cache TTL.
func (s *ValkeyLiveStore) broadcast(broadcasterID uint64) {
	if s.nc == nil || s.cfg.CacheInvalidatePrefix == "" {
		return
	}
	if err := invalidate.Publish(s.nc, s.cfg.CacheInvalidatePrefix, livekey.InvalidateScope, strconv.FormatUint(broadcasterID, 10)); err != nil {
		s.log.Warn("live: failed to broadcast invalidation", zap.Uint64("broadcaster_id", broadcasterID), zap.Error(err))
	}
}

// StartInvalidationListener subscribes to prefix+".live" so a live change made by
// any replica (or by outgress after a re-check) drops this replica's cached bool.
func (s *ValkeyLiveStore) StartInvalidationListener() {
	if s.cfg.CacheInvalidatePrefix == "" {
		return
	}
	subject := s.cfg.CacheInvalidatePrefix + "." + livekey.InvalidateScope
	sub, err := s.nc.Subscribe(subject, func(msg *nats.Msg) {
		var dto invalidate.DTO
		if err := json.Unmarshal(msg.Data, &dto); err != nil {
			return
		}
		id, err := strconv.ParseUint(dto.BroadcasterID, 10, 64)
		if err != nil || id == 0 {
			return
		}
		s.cache.Invalidate(liveKey(id))
	})
	if err != nil {
		s.log.Error("live: failed to subscribe to invalidation", zap.String("subject", subject), zap.Error(err))
		return
	}
	s.invalidationSub = sub
	s.log.Info("live: invalidation listener started", zap.String("subject", subject))
}

// StartExpiryWatcher subscribes to Valkey key-expiry notifications and, when a
// live key expires, asks outgress (system lane) to re-check the stream against
// Twitch. It runs until ctx is cancelled. Requires the Valkey server to have
// notify-keyspace-events including expired-key events (Ex). Run in a goroutine.
func (s *ValkeyLiveStore) StartExpiryWatcher(ctx context.Context) {
	channel := "__keyevent@" + strconv.Itoa(s.cfg.KeyspaceDB) + "__:expired"
	s.log.Info("live: expiry watcher starting", zap.String("channel", channel))

	for ctx.Err() == nil {
		err := s.client.Receive(ctx, s.client.B().Subscribe().Channel(channel).Build(), func(msg valkey.PubSubMessage) {
			s.onExpired(ctx, msg.Message)
		})
		if ctx.Err() != nil {
			return
		}
		s.log.Warn("live: expiry watcher dropped, reconnecting", zap.Error(err))
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

// onExpired handles one expired-key notification. It ignores everything that is
// not a live key (and the recheck guard keys), dedups across replicas with an NX
// guard, then publishes the re-check job.
func (s *ValkeyLiveStore) onExpired(ctx context.Context, key string) {
	if !strings.HasPrefix(key, livekey.KeyPrefix) || strings.HasPrefix(key, recheckKeyPrefix) {
		return
	}
	idStr := strings.TrimPrefix(key, livekey.KeyPrefix)
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil || id == 0 {
		return
	}

	// One replica per expiry fires the re-check.
	got, err := s.client.Do(ctx, s.client.B().Set().Key(recheckKeyPrefix+idStr).Value("1").Nx().ExSeconds(10).Build()).ToString()
	if err != nil || got != "OK" {
		return
	}

	if err := s.requestRecheck(ctx, idStr); err != nil {
		s.log.Warn("live: failed to publish re-check", zap.String("broadcaster_id", idStr), zap.Error(err))
	}
}

// requestRecheck publishes a stream_status job onto the outgress system lane;
// outgress resolves Twitch and writes the live key back with a fresh TTL.
func (s *ValkeyLiveStore) requestRecheck(ctx context.Context, broadcasterID string) error {
	body, err := json.Marshal(outgress.StreamStatusJob{BroadcasterID: broadcasterID})
	if err != nil {
		return err
	}
	return bus.PublishJSON(ctx, s.pub, s.cfg.OutgressSystemSubject, outgress.Message{
		Type:          outgress.TypeStreamStatus,
		BroadcasterID: broadcasterID,
		Payload:       body,
	})
}

// Close releases the invalidation subscription. The expiry watcher stops with
// its context.
func (s *ValkeyLiveStore) Close() {
	if s.invalidationSub != nil {
		_ = s.invalidationSub.Unsubscribe()
	}
	s.cache.Close()
}
