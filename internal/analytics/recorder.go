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
	pkg_valkey "ItsBagelBot/pkg/valkey"

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

type distinctSet struct {
	window catalog.Window
	at     time.Time
	key    string
}

type saltRef struct {
	window catalog.Window
	period string
}

// Recorder buffers aggregate analytics in process and flushes them to Valkey.
// Channel ids reach Valkey only as salted HMACs inside sets, so counts are exact.
// A nil Recorder records nothing.
type Recorder struct {
	client valkey.Client
	log    *zap.Logger
	reg    catalog.Registry
	now    func() time.Time

	mu        sync.Mutex
	counts    map[slot]int64
	distincts map[distinctSet]map[uint64]struct{}
	levels    map[string]int64
	ids       int

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
		client:    client,
		log:       log,
		reg:       catalog.NewRegistry(registered),
		now:       time.Now,
		counts:    map[slot]int64{},
		distincts: map[distinctSet]map[uint64]struct{}{},
		levels:    map[string]int64{},
		salts:     map[saltRef][]byte{},
		done:      make(chan struct{}),
		kick:      make(chan struct{}, 1),
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

func (r *Recorder) accepts(m catalog.Metric, t catalog.Type, dims []string) bool {
	return r != nil && catalog.Valid(m, t, dims, r.reg)
}

func cohortInput(channelID uint64, signup time.Time) bool {
	return channelID != 0 && !signup.IsZero()
}

func (r *Recorder) Add(m catalog.Metric, n int64, dims ...string) {
	if n <= 0 || !r.accepts(m, catalog.Counter, dims) {
		return
	}
	at := r.now()
	s := slot{day: catalog.PeriodStart(catalog.Day, at), key: catalog.CounterKey(at), field: catalog.Field(m, dims...)}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, tracked := r.counts[s]; !tracked && len(r.counts) >= maxSlots {
		r.nudge()
		return
	}
	r.counts[s] += n
}

func (r *Recorder) Distinct(m catalog.Metric, channelID uint64, dims ...string) {
	if channelID == 0 || !r.accepts(m, catalog.Distinct, dims) {
		return
	}
	spec, _ := catalog.Lookup(m)
	at := r.now()
	field := catalog.Field(m, dims...)
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, w := range []catalog.Window{catalog.Day, catalog.Week, catalog.Month} {
		if spec.Windows&w != 0 {
			r.addID(distinctSet{window: w, at: catalog.PeriodStart(w, at), key: catalog.DistinctKey(w, at, field)}, channelID)
		}
	}
}

// Cohort marks the channel active this week in the retention cell of its signup week.
func (r *Recorder) Cohort(channelID uint64, signup time.Time) {
	if r == nil || !cohortInput(channelID, signup) {
		return
	}
	at := r.now()
	if signup.After(at) {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.addID(distinctSet{window: catalog.Week, at: catalog.PeriodStart(catalog.Week, at), key: catalog.CohortKey(signup, at)}, channelID)
}

func (r *Recorder) addID(s distinctSet, id uint64) {
	set := r.distincts[s]
	if set == nil {
		if len(r.distincts) >= maxSlots {
			r.nudge()
			return
		}
		set = map[uint64]struct{}{}
		r.distincts[s] = set
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

type pending struct {
	counts    map[slot]int64
	distincts map[distinctSet]map[uint64]struct{}
	levels    map[string]int64
}

func (p pending) empty() bool {
	return len(p.counts)+len(p.distincts)+len(p.levels) == 0
}

func (r *Recorder) drain() pending {
	r.mu.Lock()
	defer r.mu.Unlock()
	p := pending{counts: r.counts, distincts: r.distincts, levels: r.levels}
	r.counts, r.distincts, r.levels, r.ids = map[slot]int64{}, map[distinctSet]map[uint64]struct{}{}, map[string]int64{}, 0
	return p
}

// Flush writes everything pending. Analytics are loss-tolerant: a failed
// write is logged and dropped, never retried into the hot path.
func (r *Recorder) Flush(ctx context.Context) {
	if r == nil {
		return
	}
	p := r.drain()
	if p.empty() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, flushTimeout)
	defer cancel()

	cmds := r.counterCmds(p.counts)
	cmds = append(cmds, r.distinctCmds(ctx, p.distincts)...)
	cmds = append(cmds, r.levelCmds(p.levels)...)
	r.reportErrors(pkg_valkey.ExecMulti(ctx, r.client, cmds...))
}

func (r *Recorder) counterCmds(counts map[slot]int64) valkey.Commands {
	cmds := make(valkey.Commands, 0, len(counts)+1)
	expiries := map[string]time.Time{}
	for s, n := range counts {
		cmds = append(cmds, pkg_valkey.HIncrByCmd(r.client, s.key, s.field, n))
		expiries[s.key] = catalog.DataExpiry(catalog.Day, s.day)
	}
	return append(cmds, r.expireCmds(expiries)...)
}

func (r *Recorder) distinctCmds(ctx context.Context, distincts map[distinctSet]map[uint64]struct{}) valkey.Commands {
	cmds := make(valkey.Commands, 0, 2*len(distincts))
	expiries := map[string]time.Time{}
	for s, ids := range distincts {
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
			pkg_valkey.SaddCmd(r.client, s.key, elements...),
			pkg_valkey.SaddCmd(r.client, index, s.key),
		)
		expiry := catalog.DataExpiry(s.window, s.at)
		expiries[s.key] = expiry
		expiries[index] = expiry
	}
	return append(cmds, r.expireCmds(expiries)...)
}

func (r *Recorder) levelCmds(levels map[string]int64) valkey.Commands {
	cmds := make(valkey.Commands, 0, len(levels))
	for field, delta := range levels {
		if delta != 0 {
			cmds = append(cmds, pkg_valkey.HIncrByCmd(r.client, catalog.LevelKey, field, delta))
		}
	}
	return cmds
}

func (r *Recorder) expireCmds(expiries map[string]time.Time) valkey.Commands {
	cmds := make(valkey.Commands, 0, len(expiries))
	for key, at := range expiries {
		cmds = append(cmds, pkg_valkey.ExpireAtCmd(r.client, key, at.Unix()))
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
	cmd := pkg_valkey.SetIfAbsentGetExpireAtCmd(r.client, key, string(fresh), expiry)
	existing, err := pkg_valkey.Exec1(ctx, r.client, cmd).AsBytes()
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
