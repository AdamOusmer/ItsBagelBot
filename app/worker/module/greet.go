package module

import (
	"context"
	"strconv"
	"time"

	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

// greetKeyPrefix is the per-broadcaster set of chatter ids already greeted in
// the current stream session: bagel:greeted:<broadcaster_id>.
const greetKeyPrefix = "bagel:greeted:"

// GreetStore tracks which special users have already been greeted in the current
// stream, so the bagel reply fires only on a user's first message per stream.
type GreetStore interface {
	// FirstGreet records chatterID as greeted for the broadcaster and reports
	// whether this was the first time (i.e. whether the caller should greet).
	FirstGreet(ctx context.Context, broadcasterID uint64, chatterID string) (bool, error)
	// ResetGreets clears the greeted set, called when a stream goes online so the
	// next session greets everyone again.
	ResetGreets(ctx context.Context, broadcasterID uint64) error
}

// ValkeyGreetStore backs GreetStore with a Valkey set. SADD's return count makes
// the first-message check atomic: a new member returns 1, an existing one 0.
type ValkeyGreetStore struct {
	client valkey.Client
	ttl    time.Duration // safety expiry so an abandoned set is reclaimed
	log    *zap.Logger
}

func NewValkeyGreetStore(client valkey.Client, ttl time.Duration, log *zap.Logger) *ValkeyGreetStore {
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}
	if log == nil {
		log = zap.NewNop()
	}
	return &ValkeyGreetStore{client: client, ttl: ttl, log: log}
}

func greetKey(id uint64) string { return greetKeyPrefix + strconv.FormatUint(id, 10) }

func (s *ValkeyGreetStore) FirstGreet(ctx context.Context, broadcasterID uint64, chatterID string) (bool, error) {
	key := greetKey(broadcasterID)
	added, err := s.client.Do(ctx, s.client.B().Sadd().Key(key).Member(chatterID).Build()).AsInt64()
	if err != nil {
		return false, err
	}
	if added > 0 {
		// First greet this session: (re)arm the safety expiry. Fire-and-forget so
		// the EXPIRE round-trip never lands on the response path of the chat
		// message that won the SADD (the single Valkey master is transatlantic, so
		// a blocking write would add ~90ms). The SADD above already decided the
		// greet; the TTL is only a janitorial backstop for an abandoned set, so it
		// is safe to re-arm asynchronously on its own short context derived from
		// Background (the request ctx may cancel the instant we ack). Best effort:
		// a failure only means the safety expiry was not refreshed this time.
		seconds := int64(s.ttl.Seconds())
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := s.client.Do(ctx, s.client.B().Expire().Key(key).Seconds(seconds).Build()).Error(); err != nil {
				s.log.Warn("greet: failed to re-arm greeted-set expiry", zap.String("key", key), zap.Error(err))
			}
		}()
	}
	return added > 0, nil
}

func (s *ValkeyGreetStore) ResetGreets(ctx context.Context, broadcasterID uint64) error {
	return s.client.Do(ctx, s.client.B().Del().Key(greetKey(broadcasterID)).Build()).Error()
}
