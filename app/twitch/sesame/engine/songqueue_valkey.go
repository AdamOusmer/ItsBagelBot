// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"strconv"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/pkg/cache"
	"ItsBagelBot/pkg/codec"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

var (
	ErrSongQuotaReached = errors.New("requester is at their song quota")
	ErrSongQueueFull    = errors.New("song queue is at its depth cap")
	errSongQueueStale   = errors.New("song queue changed under us")
)

type SongEntry struct {
	TrackID       string   `json:"tid"`
	Title         string   `json:"title"`
	Artists       []string `json:"artists,omitempty"`
	DurationMS    int64    `json:"dur"`
	ArtworkURL    string   `json:"art,omitempty"`
	URL           string   `json:"url,omitempty"`
	RequesterID   string   `json:"req_id"`
	RequesterName string   `json:"req_name"`
	EnqueuedAt    int64    `json:"at"`
	Position      int      `json:"-"`
}

type SongQueueSnapshot struct {
	Current *SongEntry
	UpNext  []SongEntry
}

type SongQueueLimits struct {
	MaxDepth     int
	PerRequester int
}

type SongQueueStore interface {
	Add(ctx context.Context, broadcasterID uint64, entry SongEntry, limits SongQueueLimits) (pos int, err error)
	RetractOwn(ctx context.Context, broadcasterID uint64, requesterID string) (SongEntry, bool, error)
	RemoveAt(ctx context.Context, broadcasterID uint64, position int) (SongEntry, bool, error)
	SyncPlaying(ctx context.Context, broadcasterID uint64, trackID string) (bool, error)
	Advance(ctx context.Context, broadcasterID uint64) (finished, nowPlaying *SongEntry, err error)
	Clear(ctx context.Context, broadcasterID uint64) error
	Snapshot(ctx context.Context, broadcasterID uint64, upNext int) (SongQueueSnapshot, error)
}

type songQueueDoc struct {
	Current *SongEntry  `json:"current,omitempty"`
	Up      []SongEntry `json:"up,omitempty"`
}

const (
	songQueueDocPrefix = "songqueue:doc:"

	casRetries = 5
)

const casScript = `
local cur = redis.call('GET', KEYS[1])
if (cur == false and ARGV[1] == '') or (cur ~= false and cur == ARGV[1]) then
	redis.call('SET', KEYS[1], ARGV[2], 'EX', ARGV[3])
	return 1
end
return 0`

type ValkeySongQueueStore struct {
	client valkey.Client
	ttl    time.Duration
	log    *zap.Logger
}

func NewValkeySongQueueStore(client valkey.Client, ttl time.Duration, log *zap.Logger) *ValkeySongQueueStore {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	if log == nil {
		log = zap.NewNop()
	}
	return &ValkeySongQueueStore{client: pkg_valkey.Primary(client), ttl: ttl, log: log}
}

type (
	docKey string
	rawDoc string
)

func songQueueDocKey(id uint64) docKey {
	return docKey(cache.UserKey(songQueueDocPrefix, id))
}

type docState struct {
	key docKey
	raw rawDoc
	doc songQueueDoc
}

func (s *ValkeySongQueueStore) readDoc(ctx context.Context, key docKey) (rawDoc, error) {
	resp := s.client.Do(ctx, s.client.B().Get().Key(string(key)).Build())
	b, err := resp.AsBytes()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return "", nil
		}
		return "", err
	}
	return rawDoc(b), nil
}

func (s *ValkeySongQueueStore) cas(ctx context.Context, st docState, newDoc rawDoc) (bool, error) {
	n, err := s.client.Do(ctx, s.client.B().Eval().
		Script(casScript).
		Numkeys(1).
		Key(string(st.key)).
		Arg(string(st.raw)).
		Arg(string(newDoc)).
		Arg(strconv.FormatInt(int64(s.ttl.Seconds()), 10)).
		Build()).AsInt64()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

func (s *ValkeySongQueueStore) mutate(ctx context.Context, broadcasterID uint64, fn func(*songQueueDoc) error) error {
	for range casRetries {
		st, err := s.loadDoc(ctx, broadcasterID)
		if err != nil {
			return err
		}
		if err := fn(&st.doc); err != nil {
			return err
		}
		done, err := s.commit(ctx, st)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
	}
	return errSongQueueStale
}

func (s *ValkeySongQueueStore) loadDoc(ctx context.Context, broadcasterID uint64) (docState, error) {
	st := docState{key: songQueueDocKey(broadcasterID)}
	raw, err := s.readDoc(ctx, st.key)
	if err != nil {
		return st, err
	}
	st.raw = raw
	if raw == "" {
		return st, nil
	}
	if err := codec.Unmarshal([]byte(raw), &st.doc); err != nil {
		s.log.Warn("songqueue: undecodable document, resetting",
			module.BIDField(broadcasterID), zap.Error(err))
		st.doc = songQueueDoc{}
	}
	return st, nil
}

func (s *ValkeySongQueueStore) commit(ctx context.Context, st docState) (bool, error) {
	newB, err := codec.Marshal(st.doc)
	if err != nil {
		return false, err
	}
	newDoc := rawDoc(newB)
	if newDoc == st.raw {
		return true, nil
	}
	return s.cas(ctx, st, newDoc)
}

func (s *ValkeySongQueueStore) Add(ctx context.Context, broadcasterID uint64, entry SongEntry, limits SongQueueLimits) (int, error) {
	var pos int
	err := s.mutate(ctx, broadcasterID, func(d *songQueueDoc) error {
		if limits.requesterAtQuota(d.Up, entry.RequesterID) {
			return ErrSongQuotaReached
		}
		if limits.queueFull(d.Up) {
			return ErrSongQueueFull
		}
		entry.EnqueuedAt = time.Now().UnixMilli()
		d.Up = append(d.Up, entry)
		pos = len(d.Up)
		return nil
	})
	return pos, err
}

func (l SongQueueLimits) requesterAtQuota(up []SongEntry, requesterID string) bool {
	if l.PerRequester <= 0 {
		return false
	}
	mine := 0
	for i := range up {
		if up[i].RequesterID != requesterID {
			continue
		}
		mine++
		if mine >= l.PerRequester {
			return true
		}
	}
	return false
}

func (l SongQueueLimits) queueFull(up []SongEntry) bool {
	return l.MaxDepth > 0 && len(up) >= l.MaxDepth
}

func (s *ValkeySongQueueStore) RetractOwn(ctx context.Context, broadcasterID uint64, requesterID string) (SongEntry, bool, error) {
	return s.removeWhere(ctx, broadcasterID, func(up []SongEntry) int {
		for i := len(up) - 1; i >= 0; i-- {
			if up[i].RequesterID == requesterID {
				return i
			}
		}
		return noSongIndex
	})
}

func (s *ValkeySongQueueStore) RemoveAt(ctx context.Context, broadcasterID uint64, position int) (SongEntry, bool, error) {
	return s.removeWhere(ctx, broadcasterID, func(up []SongEntry) int {
		if position < 1 || position > len(up) {
			return noSongIndex
		}
		return position - 1
	})
}

const noSongIndex = -1

func (s *ValkeySongQueueStore) removeWhere(ctx context.Context, broadcasterID uint64, pick func(up []SongEntry) int) (SongEntry, bool, error) {
	var (
		out SongEntry
		ok  bool
	)
	err := s.mutate(ctx, broadcasterID, func(d *songQueueDoc) error {
		i := pick(d.Up)
		if i == noSongIndex {
			return nil
		}
		out = d.Up[i]
		out.Position = i + 1
		d.Up = append(d.Up[:i], d.Up[i+1:]...)
		ok = true
		return nil
	})
	return out, ok, err
}

func (s *ValkeySongQueueStore) SyncPlaying(ctx context.Context, broadcasterID uint64, trackID string) (bool, error) {
	changed := false
	err := s.mutate(ctx, broadcasterID, func(d *songQueueDoc) error {
		changed = false
		if trackID == "" {
			return nil
		}
		if d.Current != nil && d.Current.TrackID == trackID {
			return nil
		}
		for i := range d.Up {
			if d.Up[i].TrackID != trackID {
				continue
			}
			entry := d.Up[i]
			d.Current = &entry
			d.Up = d.Up[i+1:]
			changed = true
			return nil
		}
		return nil
	})
	return changed, err
}

func (s *ValkeySongQueueStore) Advance(ctx context.Context, broadcasterID uint64) (*SongEntry, *SongEntry, error) {
	var (
		finished   *SongEntry
		nowPlaying *SongEntry
	)
	err := s.mutate(ctx, broadcasterID, func(d *songQueueDoc) error {
		if len(d.Up) == 0 {
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
