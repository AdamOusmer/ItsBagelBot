package module

import (
	"context"
	"strconv"
	"time"

	"github.com/valkey-io/valkey-go"
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
}

func NewValkeyGreetStore(client valkey.Client, ttl time.Duration) *ValkeyGreetStore {
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}
	return &ValkeyGreetStore{client: client, ttl: ttl}
}

func greetKey(id uint64) string { return greetKeyPrefix + strconv.FormatUint(id, 10) }

func (s *ValkeyGreetStore) FirstGreet(ctx context.Context, broadcasterID uint64, chatterID string) (bool, error) {
	key := greetKey(broadcasterID)
	added, err := s.client.Do(ctx, s.client.B().Sadd().Key(key).Member(chatterID).Build()).AsInt64()
	if err != nil {
		return false, err
	}
	if added > 0 {
		// First greet this session: (re)arm the safety expiry. Best effort.
		_ = s.client.Do(ctx, s.client.B().Expire().Key(key).Seconds(int64(s.ttl.Seconds())).Build()).Error()
	}
	return added > 0, nil
}

func (s *ValkeyGreetStore) ResetGreets(ctx context.Context, broadcasterID uint64) error {
	return s.client.Do(ctx, s.client.B().Del().Key(greetKey(broadcasterID)).Build()).Error()
}
