// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"strconv"
	"time"

	"ItsBagelBot/pkg/codec"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

// Sentinel failures the songqueue module maps onto friendly chat lines. The
// contended error means the compare-and-set retry budget ran out against a
// hot channel — rare by construction (chat-paced writes), and safe to surface
// as a generic retry hint rather than a specific one.
var (
	ErrSongAlreadyQueued = errors.New("song already queued by this requester")
	ErrSongQueueFull     = errors.New("song queue is at its depth cap")
	errSongQueueStale    = errors.New("song queue changed under us")
)

// SongEntry is one requested track in a channel's song queue. RequesterID is
// the Twitch user id captured at request time — retract authorization keys on
// it, never on the display name (names collide and change).
type SongEntry struct {
	TrackID       string   `json:"tid"`
	Title         string   `json:"title"`
	Artists       []string `json:"artists,omitempty"`
	DurationMS    int64    `json:"dur"`
	ArtworkURL    string   `json:"art,omitempty"`
	URL           string   `json:"url,omitempty"`
	RequesterID   string   `json:"req_id"`
	RequesterName string   `json:"req_name"`
	EnqueuedAt    int64    `json:"at"` // unix millis
	// Position is filled by reads only (1-based spot in the up-next list);
	// it is never persisted.
	Position int `json:"-"`
}

// SongQueueSnapshot is a point-in-time read of one channel's queue.
type SongQueueSnapshot struct {
	Current *SongEntry
	UpNext  []SongEntry
}

// SongQueueStore holds the per-broadcaster song-request state: the track
// being played now plus the ordered line behind it. The songqueue module
// drives it from chat (!sr …); nothing else writes it.
type SongQueueStore interface {
	// Add appends the resolved track unless its requester already has one
	// pending (one live request per viewer keeps retract unambiguous) or the
	// line is at maxDepth. It returns the requester-facing 1-based position.
	Add(ctx context.Context, broadcasterID uint64, entry SongEntry, maxDepth int) (pos int, err error)
	// RetractOwn removes the requester's most recent pending entry — the one
	// thing a viewer may do to anyone's requests. The currently-playing track
	// is intentionally out of reach: it is already playing.
	RetractOwn(ctx context.Context, broadcasterID uint64, requesterID string) (SongEntry, bool, error)
	// RemoveAt takes the 1-based up-next entry out of the line (a moderator
	// action; viewers have no positional reach).
	RemoveAt(ctx context.Context, broadcasterID uint64, position int) (SongEntry, bool, error)
	// Advance marks the head as now-playing and promotes the next entry,
	// returning what just finished and what started. On an empty line it
	// clears a stale current instead of inventing one.
	Advance(ctx context.Context, broadcasterID uint64) (finished, nowPlaying *SongEntry, err error)
	// Clear empties everything including the now-playing pointer.
	Clear(ctx context.Context, broadcasterID uint64) error
	// Snapshot reads the current state; upNext caps how many waiting entries
	// come back (negative asks for all).
	Snapshot(ctx context.Context, broadcasterID uint64, upNext int) (SongQueueSnapshot, error)
}

// songQueueDoc is the whole per-channel state in one document. One key keeps
// every mutation a single compare-and-set: the alternatives (zset + hash +
// dedupe index) would need multi-key scripts to enforce the one-request-per-
// viewer and depth-cap invariants atomically, which is strictly more moving
// parts around the same chat-paced traffic.
type songQueueDoc struct {
	Current *SongEntry  `json:"current,omitempty"`
	Up      []SongEntry `json:"up,omitempty"`
}

const (
	songQueueDocPrefix = "songqueue:doc:"

	// casRetries bounds the optimistic-concurrency loop. Contention requires
	// two replicas mutating one channel's queue within the sub-millisecond
	// read-to-cas window; five rounds is far past anything chat cadence can
	// sustain, and exhausting it fails loudly instead of writing blind.
	casRetries = 5
)

// casScript writes newDoc only while the stored document still equals oldDoc
// ("" denotes absent), re-arming the safety TTL on success. Returning 0 sends
// the caller around the loop with fresh reads.
const casScript = `
local cur = redis.call('GET', KEYS[1])
if (cur == false and ARGV[1] == '') or (cur ~= false and cur == ARGV[1]) then
	redis.call('SET', KEYS[1], ARGV[2], 'EX', ARGV[3])
	return 1
end
return 0`

// ValkeySongQueueStore backs SongQueueStore with one JSON document per
// broadcaster, mutated through a get → apply → CAS loop.
type ValkeySongQueueStore struct {
	client valkey.Client
	ttl    time.Duration
	log    *zap.Logger
}

// NewValkeySongQueueStore builds the store on a primary-consistent view, for
// the same reason as NewValkeyQueueStore: every read follows a write chat
// just made, and a node-local replica answering makes the bot contradict
// itself ("you're #2!" then "!sr" shows four people ahead of them).
func NewValkeySongQueueStore(client valkey.Client, ttl time.Duration, log *zap.Logger) *ValkeySongQueueStore {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	if log == nil {
		log = zap.NewNop()
	}
	return &ValkeySongQueueStore{client: pkg_valkey.Primary(client), ttl: ttl, log: log}
}

func songQueueDocKey(id uint64) string {
	return songQueueDocPrefix + strconv.FormatUint(id, 10)
}

func (s *ValkeySongQueueStore) readDoc(ctx context.Context, key string) (string, error) {
	resp := s.client.Do(ctx, s.client.B().Get().Key(key).Build())
	b, err := resp.AsBytes()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return "", nil
		}
		return "", err
	}
	return string(b), nil
}

func (s *ValkeySongQueueStore) cas(ctx context.Context, key, oldDoc, newDoc string) (bool, error) {
	n, err := s.client.Do(ctx, s.client.B().Eval().
		Script(casScript).
		Numkeys(1).
		Key(key).
		Arg(oldDoc).
		Arg(newDoc).
		Arg(strconv.FormatInt(int64(s.ttl.Seconds()), 10)).
		Build()).AsInt64()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// mutate runs fn against the freshest document until its write lands
// compare-and-set clean. fn reports domain failures (already queued, full)
// as errors, which abort the loop untouched. An unchanged document skips the
// write entirely.
func (s *ValkeySongQueueStore) mutate(ctx context.Context, broadcasterID uint64, fn func(*songQueueDoc) error) error {
	key := songQueueDocKey(broadcasterID)
	for range casRetries {
		oldDoc, err := s.readDoc(ctx, key)
		if err != nil {
			return err
		}
		var doc songQueueDoc
		if oldDoc != "" {
			if err := codec.Unmarshal([]byte(oldDoc), &doc); err != nil {
				// A corrupt or foreign-format document resets rather than
				// bricking the channel's queue forever.
				s.log.Warn("songqueue: undecodable document, resetting",
					zap.Uint64("broadcaster_id", broadcasterID), zap.Error(err))
				doc = songQueueDoc{}
			}
		}
		if err := fn(&doc); err != nil {
			return err
		}
		newB, err := codec.Marshal(doc)
		if err != nil {
			return err
		}
		newDoc := string(newB)
		if newDoc == oldDoc {
			return nil
		}
		won, err := s.cas(ctx, key, oldDoc, newDoc)
		if err != nil {
			return err
		}
		if won {
			return nil
		}
	}
	return errSongQueueStale
}

func (s *ValkeySongQueueStore) Add(ctx context.Context, broadcasterID uint64, entry SongEntry, maxDepth int) (int, error) {
	var pos int
	err := s.mutate(ctx, broadcasterID, func(d *songQueueDoc) error {
		for i := range d.Up {
			if d.Up[i].RequesterID == entry.RequesterID {
				return ErrSongAlreadyQueued
			}
		}
		if maxDepth > 0 && len(d.Up) >= maxDepth {
			return ErrSongQueueFull
		}
		entry.EnqueuedAt = time.Now().UnixMilli()
		d.Up = append(d.Up, entry)
		pos = len(d.Up)
		return nil
	})
	return pos, err
}

func (s *ValkeySongQueueStore) RetractOwn(ctx context.Context, broadcasterID uint64, requesterID string) (SongEntry, bool, error) {
	var (
		out SongEntry
		ok  bool
	)
	err := s.mutate(ctx, broadcasterID, func(d *songQueueDoc) error {
		// Latest-first: a viewer fixing a typo wants their newest ask gone.
		for i := len(d.Up) - 1; i >= 0; i-- {
			if d.Up[i].RequesterID == requesterID {
				out = d.Up[i]
				out.Position = i + 1
				d.Up = append(d.Up[:i], d.Up[i+1:]...)
				ok = true
				return nil
			}
		}
		return nil
	})
	return out, ok, err
}

func (s *ValkeySongQueueStore) RemoveAt(ctx context.Context, broadcasterID uint64, position int) (SongEntry, bool, error) {
	var (
		out SongEntry
		ok  bool
	)
	err := s.mutate(ctx, broadcasterID, func(d *songQueueDoc) error {
		if position < 1 || position > len(d.Up) {
			return nil
		}
		i := position - 1
		out = d.Up[i]
		out.Position = i + 1
		d.Up = append(d.Up[:i], d.Up[i+1:]...)
		ok = true
		return nil
	})
	return out, ok, err
}

func (s *ValkeySongQueueStore) Advance(ctx context.Context, broadcasterID uint64) (*SongEntry, *SongEntry, error) {
	var (
		finished   *SongEntry
		nowPlaying *SongEntry
	)
	err := s.mutate(ctx, broadcasterID, func(d *songQueueDoc) error {
		if len(d.Up) == 0 {
			// Nothing behind it: an advance on an exhausted queue retires the
			// stale pointer instead of replaying the last track forever.
			finished = d.Current
			d.Current = nil
			return nil
		}
		finished = d.Current
		head := d.Up[0]
		d.Current = &head
		nowPlaying = &head
		d.Up = d.Up[1:]
		return nil
	})
	return finished, nowPlaying, err
}

func (s *ValkeySongQueueStore) Clear(ctx context.Context, broadcasterID uint64) error {
	return s.mutate(ctx, broadcasterID, func(d *songQueueDoc) error {
		d.Current = nil
		d.Up = nil
		return nil
	})
}

func (s *ValkeySongQueueStore) Snapshot(ctx context.Context, broadcasterID uint64, upNext int) (SongQueueSnapshot, error) {
	raw, err := s.readDoc(ctx, songQueueDocKey(broadcasterID))
	if err != nil || raw == "" {
		return SongQueueSnapshot{}, err
	}
	var doc songQueueDoc
	if err := codec.Unmarshal([]byte(raw), &doc); err != nil {
		return SongQueueSnapshot{}, err
	}
	snap := SongQueueSnapshot{Current: doc.Current}
	if upNext < 0 || upNext > len(doc.Up) {
		upNext = len(doc.Up)
	}
	snap.UpNext = make([]SongEntry, upNext)
	copy(snap.UpNext, doc.Up[:upNext])
	for i := range snap.UpNext {
		snap.UpNext[i].Position = i + 1
	}
	return snap, nil
}
