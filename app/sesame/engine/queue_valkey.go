package engine

import (
	"context"
	"strconv"
	"time"

	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

// QueueStore holds the per-broadcaster play queue: an ordered line of chatter
// logins plus an open/closed flag gating joins. The queue module drives it from
// chat (!queue open/close/next/remove, !join, !list); nothing else writes it.
type QueueStore interface {
	// SetOpen opens (true) or closes (false) the queue to new joins. Closing
	// keeps the line intact so the streamer can play through the remainder.
	SetOpen(ctx context.Context, broadcasterID uint64, open bool) error
	// IsOpen reports whether the queue currently accepts joins.
	IsOpen(ctx context.Context, broadcasterID uint64) (bool, error)
	// Join appends login to the line if not already in it. It returns the
	// login's 1-based position, the line's size, and whether this call added it
	// (false: it was already queued, pos is the existing spot).
	Join(ctx context.Context, broadcasterID uint64, login string) (pos, size int64, joined bool, err error)
	// Remove takes login out of the line, reporting whether it was in it. It
	// serves both a viewer's leave and a moderator's remove.
	Remove(ctx context.Context, broadcasterID uint64, login string) (bool, error)
	// Pop dequeues and returns the front of the line plus how many remain
	// behind them. An empty queue returns login "".
	Pop(ctx context.Context, broadcasterID uint64) (login string, remaining int64, err error)
	// List returns the first n logins in order plus the line's total size.
	List(ctx context.Context, broadcasterID uint64, n int64) (entries []string, total int64, err error)
	// Clear empties the line without touching the open/closed flag.
	Clear(ctx context.Context, broadcasterID uint64) error
}

// Queue keyspace: queue:open:<broadcaster_id> is the joins-accepted flag,
// queue:line:<broadcaster_id> is the line itself — a sorted set of chatter
// logins scored by join time (unix millis), so ZADD NX is an atomic
// join-once-keep-spot and ZPOPMIN is "next player up".
const (
	queueOpenPrefix = "queue:open:"
	queueLinePrefix = "queue:line:"
)

// ValkeyQueueStore backs QueueStore with one sorted set and one flag key per
// broadcaster. Both carry a safety TTL (re-armed on open and on every join) so
// a queue abandoned after a stream reclaims itself.
type ValkeyQueueStore struct {
	client valkey.Client
	ttl    time.Duration
	log    *zap.Logger
}

// NewValkeyQueueStore builds the store on a primary-consistent view. One
// broadcaster's chat drives the whole queue in sequence, so every read follows
// a write chat just made: IsOpen gates joins against the flag SetOpen wrote,
// and List renders the line Join and Pop just changed. Join and Pop already
// reach the master because their batches mix in a write, but a node-local
// replica serving IsOpen or List makes chat contradict itself — a viewer told
// the queue is closed right after the streamer opened it, or a !list missing
// the person who just joined. This is one broadcaster's feature, not the
// firehose, and the view borrows the client's connections.
func NewValkeyQueueStore(client valkey.Client, ttl time.Duration, log *zap.Logger) *ValkeyQueueStore {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	if log == nil {
		log = zap.NewNop()
	}
	return &ValkeyQueueStore{client: pkg_valkey.Primary(client), ttl: ttl, log: log}
}

func queueOpenKey(id uint64) string { return queueOpenPrefix + strconv.FormatUint(id, 10) }
func queueLineKey(id uint64) string { return queueLinePrefix + strconv.FormatUint(id, 10) }

func (s *ValkeyQueueStore) SetOpen(ctx context.Context, broadcasterID uint64, open bool) error {
	if !open {
		return s.client.Do(ctx, s.client.B().Del().Key(queueOpenKey(broadcasterID)).Build()).Error()
	}
	seconds := int64(s.ttl.Seconds())
	for _, r := range s.client.DoMulti(ctx,
		s.client.B().Set().Key(queueOpenKey(broadcasterID)).Value("1").ExSeconds(seconds).Build(),
		// Re-arm the line's safety expiry too: an open queue is an active one.
		s.client.B().Expire().Key(queueLineKey(broadcasterID)).Seconds(seconds).Build(),
	) {
		if err := r.Error(); err != nil && !valkey.IsValkeyNil(err) {
			return err
		}
	}
	return nil
}

func (s *ValkeyQueueStore) IsOpen(ctx context.Context, broadcasterID uint64) (bool, error) {
	n, err := s.client.Do(ctx, s.client.B().Exists().Key(queueOpenKey(broadcasterID)).Build()).AsInt64()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *ValkeyQueueStore) Join(ctx context.Context, broadcasterID uint64, login string) (pos, size int64, joined bool, err error) {
	key := queueLineKey(broadcasterID)
	// One round trip: claim the spot (NX keeps an existing one and its score),
	// read the resulting rank and size, re-arm the safety expiries. The open
	// flag is re-armed alongside the line: joins prove the queue is in active
	// use, so a stream running past the TTL never has its open queue silently
	// close mid-session. (EXPIRE on an absent flag is a harmless no-op.)
	seconds := int64(s.ttl.Seconds())
	resps := s.client.DoMulti(ctx,
		s.client.B().Zadd().Key(key).Nx().ScoreMember().ScoreMember(float64(time.Now().UnixMilli()), login).Build(),
		s.client.B().Zrank().Key(key).Member(login).Build(),
		s.client.B().Zcard().Key(key).Build(),
		s.client.B().Expire().Key(key).Seconds(seconds).Build(),
		s.client.B().Expire().Key(queueOpenKey(broadcasterID)).Seconds(seconds).Build(),
	)
	added, err := resps[0].AsInt64()
	if err != nil {
		return 0, 0, false, err
	}
	rank, err := resps[1].AsInt64()
	if err != nil {
		return 0, 0, false, err
	}
	size, err = resps[2].AsInt64()
	if err != nil {
		return 0, 0, false, err
	}
	return rank + 1, size, added > 0, nil
}

func (s *ValkeyQueueStore) Remove(ctx context.Context, broadcasterID uint64, login string) (bool, error) {
	n, err := s.client.Do(ctx, s.client.B().Zrem().Key(queueLineKey(broadcasterID)).Member(login).Build()).AsInt64()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *ValkeyQueueStore) Pop(ctx context.Context, broadcasterID uint64) (login string, remaining int64, err error) {
	key := queueLineKey(broadcasterID)
	resps := s.client.DoMulti(ctx,
		s.client.B().Zpopmin().Key(key).Count(1).Build(),
		s.client.B().Zcard().Key(key).Build(),
	)
	popped, err := resps[0].AsZScores()
	if err != nil {
		return "", 0, err
	}
	remaining, err = resps[1].AsInt64()
	if err != nil {
		return "", 0, err
	}
	if len(popped) == 0 {
		return "", remaining, nil
	}
	return popped[0].Member, remaining, nil
}

func (s *ValkeyQueueStore) List(ctx context.Context, broadcasterID uint64, n int64) (entries []string, total int64, err error) {
	if n <= 0 {
		return nil, 0, nil
	}
	key := queueLineKey(broadcasterID)
	resps := s.client.DoMulti(ctx,
		s.client.B().Zrange().Key(key).Min("0").Max(strconv.FormatInt(n-1, 10)).Build(),
		s.client.B().Zcard().Key(key).Build(),
	)
	entries, err = resps[0].AsStrSlice()
	if err != nil {
		return nil, 0, err
	}
	total, err = resps[1].AsInt64()
	if err != nil {
		return nil, 0, err
	}
	return entries, total, nil
}

func (s *ValkeyQueueStore) Clear(ctx context.Context, broadcasterID uint64) error {
	return s.client.Do(ctx, s.client.B().Del().Key(queueLineKey(broadcasterID)).Build()).Error()
}
