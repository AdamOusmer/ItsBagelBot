// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package analytics

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"strconv"
	"sync"
	"time"

	catalog "ItsBagelBot/internal/domain/analytics"

	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

const (
	flushInterval = 10 * time.Second
	flushTimeout  = 3 * time.Second
	maxSlots      = 4096
	maxPendingIDs = 1 << 16
	saltBytes     = 32
	elementBytes  = 8
)

type slot struct {
	day   time.Time
	key   string
	field string
}

type sketch struct {
	window catalog.Window
	at     time.Time
	key    string
}

type saltRef struct {
	window catalog.Window
	period string
}

// Recorder buffers aggregate analytics in process and flushes them to Valkey.
// Channel ids reach Valkey only as salted HMACs inside HyperLogLog sketches.
// A nil Recorder records nothing.
type Recorder struct {
	client valkey.Client
	log    *zap.Logger
	reg    catalog.Registry
	now    func() time.Time

	mu       sync.Mutex
	counts   map[slot]int64
	maxes    map[slot]int64
	sketches map[sketch]map[uint64]struct{}
	ids      int

	saltMu sync.Mutex
	salts  map[saltRef][]byte

	done chan struct{}
	kick chan struct{}
	wg   sync.WaitGroup
}

type Option func(*Recorder)

func WithClock(now func() time.Time) Option { return func(r *Recorder) { r.now = now } }

// WithoutTicker leaves flushing to explicit Flush calls.
func WithoutTicker() Option { return func(r *Recorder) { r.done = nil } }

func New(client valkey.Client, log *zap.Logger, registered map[catalog.Kind][]string, opts ...Option) *Recorder {
	r := &Recorder{
		client:   client,
		log:      log,
		reg:      catalog.NewRegistry(registered),
		now:      time.Now,
		counts:   map[slot]int64{},
		maxes:    map[slot]int64{},
		sketches: map[sketch]map[uint64]struct{}{},
		salts:    map[saltRef][]byte{},
		done:     make(chan struct{}),
		kick:     make(chan struct{}, 1),
	}
	for _, opt := range opts {
		opt(r)
	}
	if r.done != nil {
		r.wg.Add(1)
		go r.loop()
	}
	return r
}

func (r *Recorder) loop() {
	defer r.wg.Done()
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			r.Flush(context.Background())
		case <-r.kick:
			r.Flush(context.Background())
		case <-r.done:
			return
		}
	}
}

func (r *Recorder) Count(m catalog.Metric, dims ...string) { r.Add(m, 1, dims...) }

func (r *Recorder) Add(m catalog.Metric, n int64, dims ...string) {
	if r == nil || n <= 0 || !catalog.Valid(m, catalog.Counter, dims, r.reg) {
		return
	}
	at := r.now()
	s := slot{day: catalog.PeriodStart(catalog.Day, at), key: catalog.CounterKey(at), field: catalog.Field(m, dims...)}
	r.bump(r.counts, s, func(old int64) int64 { return old + n })
}

func (r *Recorder) Max(m catalog.Metric, v int64, dims ...string) {
	if r == nil || v < 0 || !catalog.Valid(m, catalog.Gauge, dims, r.reg) {
		return
	}
	at := r.now()
	s := slot{day: catalog.PeriodStart(catalog.Day, at), key: catalog.GaugeKey(at), field: catalog.Field(m, dims...)}
	r.bump(r.maxes, s, func(old int64) int64 { return max(old, v) })
}

func (r *Recorder) bump(into map[slot]int64, s slot, next func(int64) int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	old, tracked := into[s]
	if !tracked && len(r.counts)+len(r.maxes) >= maxSlots {
		r.nudge()
		return
	}
	into[s] = next(old)
}

func (r *Recorder) Distinct(m catalog.Metric, channelID uint64, dims ...string) {
	if r == nil || channelID == 0 || !catalog.Valid(m, catalog.Distinct, dims, r.reg) {
		return
	}
	spec, _ := catalog.Lookup(m)
	at := r.now()
	field := catalog.Field(m, dims...)
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, w := range []catalog.Window{catalog.Day, catalog.Week, catalog.Month} {
		if spec.Windows&w != 0 {
			r.addID(sketch{window: w, at: catalog.PeriodStart(w, at), key: catalog.DistinctKey(w, at, field)}, channelID)
		}
	}
}

// Cohort marks the channel active this week in the retention cell of its signup week.
func (r *Recorder) Cohort(channelID uint64, signup time.Time) {
	if r == nil || channelID == 0 || signup.IsZero() {
		return
	}
	at := r.now()
	if signup.After(at) {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.addID(sketch{window: catalog.Week, at: catalog.PeriodStart(catalog.Week, at), key: catalog.CohortKey(signup, at)}, channelID)
}

func (r *Recorder) addID(s sketch, id uint64) {
	set := r.sketches[s]
	if set == nil {
		if len(r.sketches) >= maxSlots {
			r.nudge()
			return
		}
		set = map[uint64]struct{}{}
		r.sketches[s] = set
	}
	if _, seen := set[id]; seen {
		return
	}
	if r.ids >= maxPendingIDs {
		r.nudge()
		return
	}
	set[id] = struct{}{}
	r.ids++
}

func (r *Recorder) nudge() {
	select {
	case r.kick <- struct{}{}:
	default:
	}
}

func (r *Recorder) drain() (map[slot]int64, map[slot]int64, map[sketch]map[uint64]struct{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	counts, maxes, sketches := r.counts, r.maxes, r.sketches
	r.counts, r.maxes, r.sketches, r.ids = map[slot]int64{}, map[slot]int64{}, map[sketch]map[uint64]struct{}{}, 0
	return counts, maxes, sketches
}

// Flush writes everything pending. Analytics are loss-tolerant: a failed
// write is logged and dropped, never retried into the hot path.
func (r *Recorder) Flush(ctx context.Context) {
	if r == nil {
		return
	}
	counts, maxes, sketches := r.drain()
	if len(counts)+len(maxes)+len(sketches) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, flushTimeout)
	defer cancel()

	cmds := r.counterCmds(counts)
	cmds = append(cmds, r.gaugeCmds(maxes)...)
	cmds = append(cmds, r.sketchCmds(ctx, sketches)...)
	r.reportErrors(r.client.DoMulti(ctx, cmds...))
}

func (r *Recorder) counterCmds(counts map[slot]int64) valkey.Commands {
	cmds := make(valkey.Commands, 0, len(counts)+1)
	expiries := map[string]time.Time{}
	for s, n := range counts {
		cmds = append(cmds, r.client.B().Hincrby().Key(s.key).Field(s.field).Increment(n).Build())
		expiries[s.key] = catalog.DataExpiry(catalog.Day, s.day)
	}
	return append(cmds, r.expireCmds(expiries)...)
}

func (r *Recorder) gaugeCmds(maxes map[slot]int64) valkey.Commands {
	cmds := make(valkey.Commands, 0, len(maxes)+1)
	expiries := map[string]time.Time{}
	for s, v := range maxes {
		cmds = append(cmds, r.client.B().Zadd().Key(s.key).Gt().ScoreMember().ScoreMember(float64(v), s.field).Build())
		expiries[s.key] = catalog.DataExpiry(catalog.Day, s.day)
	}
	return append(cmds, r.expireCmds(expiries)...)
}

func (r *Recorder) sketchCmds(ctx context.Context, sketches map[sketch]map[uint64]struct{}) valkey.Commands {
	cmds := make(valkey.Commands, 0, 2*len(sketches))
	expiries := map[string]time.Time{}
	for s, ids := range sketches {
		salt, err := r.salt(ctx, s.window, s.at)
		if err != nil {
			r.log.Debug("analytics salt unavailable", zap.Error(err))
			continue
		}
		elements := make([]string, 0, len(ids))
		for id := range ids {
			elements = append(elements, element(salt, id))
		}
		index := catalog.IndexKey(s.window, s.at)
		cmds = append(cmds,
			r.client.B().Pfadd().Key(s.key).Element(elements...).Build(),
			r.client.B().Sadd().Key(index).Member(s.key).Build(),
		)
		expiry := catalog.DataExpiry(s.window, s.at)
		expiries[s.key] = expiry
		expiries[index] = expiry
	}
	return append(cmds, r.expireCmds(expiries)...)
}

func (r *Recorder) expireCmds(expiries map[string]time.Time) valkey.Commands {
	cmds := make(valkey.Commands, 0, len(expiries))
	for key, at := range expiries {
		cmds = append(cmds, r.client.B().Expireat().Key(key).Timestamp(at.Unix()).Build())
	}
	return cmds
}

func (r *Recorder) salt(ctx context.Context, w catalog.Window, at time.Time) ([]byte, error) {
	ref := saltRef{window: w, period: catalog.Period(w, at)}
	r.saltMu.Lock()
	defer r.saltMu.Unlock()
	if salt, ok := r.salts[ref]; ok {
		return salt, nil
	}
	fresh := make([]byte, saltBytes)
	if _, err := rand.Read(fresh); err != nil {
		return nil, err
	}
	key := catalog.SaltKey(w, at)
	expiry := strconv.FormatInt(catalog.SaltExpiry(w, at).Unix(), 10)
	cmd := r.client.B().Arbitrary("SET").Keys(key).Args(string(fresh), "NX", "GET", "EXAT", expiry).Build()
	existing, err := r.client.Do(ctx, cmd).AsBytes()
	switch {
	case valkey.IsValkeyNil(err):
		existing = fresh
	case err != nil:
		return nil, err
	}
	for stale := range r.salts {
		if stale.window == w && stale.period != ref.period {
			delete(r.salts, stale)
		}
	}
	r.salts[ref] = existing
	return existing, nil
}

func element(salt []byte, id uint64) string {
	var raw [8]byte
	binary.BigEndian.PutUint64(raw[:], id)
	mac := hmac.New(sha256.New, salt)
	mac.Write(raw[:])
	return string(mac.Sum(nil)[:elementBytes])
}

func (r *Recorder) reportErrors(resps []valkey.ValkeyResult) {
	failed := 0
	var first error
	for _, resp := range resps {
		if err := resp.Error(); err != nil {
			if first == nil {
				first = err
			}
			failed++
		}
	}
	if failed > 0 {
		r.log.Warn("analytics flush partially failed", zap.Int("failed", failed), zap.Error(first))
	}
}

func (r *Recorder) Close() {
	if r == nil {
		return
	}
	if r.done != nil {
		close(r.done)
		r.wg.Wait()
	}
	r.Flush(context.Background())
}
