package engine

import (
	"context"
	"strconv"
	"time"

	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

// greetKeyPrefix is the per-broadcaster set of chatter ids already greeted in the
// current stream session: bagel:greeted:<broadcaster_id>.
const greetKeyPrefix = "bagel:greeted:"

// ValkeyGreetStore backs GreetStore with a Valkey set. SADD's return count makes
// the first-message check atomic: a new member returns 1, an existing one 0.
type ValkeyGreetStore struct {
	client valkey.Client
	ttlArg string // safety expiry so an abandoned set is reclaimed
}

// SADD and its safety expiry execute atomically in the same server call. This
// preserves the one-round-trip response path while avoiding one goroutine per
// newly seen chatter and the race where a process dies before EXPIRE lands.
var firstGreetScript = valkey.NewLuaScript(`
local added = redis.call('SADD', KEYS[1], ARGV[1])
if added == 1 then redis.call('EXPIRE', KEYS[1], ARGV[2]) end
return added`)

func NewValkeyGreetStore(client valkey.Client, ttl time.Duration, log *zap.Logger) *ValkeyGreetStore {
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}
	_ = log // retained in the constructor contract for stable dependency wiring
	return &ValkeyGreetStore{client: client, ttlArg: strconv.FormatInt(int64(ttl.Seconds()), 10)}
}

func greetKey(id uint64) string { return greetKeyPrefix + strconv.FormatUint(id, 10) }

func (s *ValkeyGreetStore) FirstGreet(ctx context.Context, broadcasterID uint64, chatterID string) (bool, error) {
	key := greetKey(broadcasterID)
	added, err := firstGreetScript.Exec(ctx, s.client, []string{key}, []string{
		chatterID, s.ttlArg,
	}).AsInt64()
	if err != nil {
		return false, err
	}
	return added > 0, nil
}

func (s *ValkeyGreetStore) ResetGreets(ctx context.Context, broadcasterID uint64) error {
	return s.client.Do(ctx, s.client.B().Del().Key(greetKey(broadcasterID)).Build()).Error()
}
