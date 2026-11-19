// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package analytics

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"slices"
	"strconv"
	"sync"
	"time"

	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/idempotency"
	pkg_valkey "ItsBagelBot/pkg/valkey"
	"github.com/google/uuid"

	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

const (
	flushInterval = time.Second
	flushTimeout  = 3 * time.Second
	flushSlots    = 4096
	flushIDs      = 1 << 16
	distinctChunk = 1000
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

type saltResult struct {
	value []byte
	err   error
}

type Recorder struct {
	flushMu sync.Mutex
	next    *idempotency.Handoff
	flight  *pending
	client  valkey.Client
	log     *zap.Logger
	reg     catalog.Registry
	now     func() time.Time

	mu        sync.Mutex
	counts    map[slot]int64
	distincts map[distinctSet]map[uint64]struct{}
	ids       int

	done chan struct{}
	kick chan struct{}
	wg   sync.WaitGroup
}

type Option func(*Recorder)

func WithClock(now func() time.Time) Option { return func(r *Recorder) { r.now = now } }

// WithoutTicker leaves flushing to explicit Flush calls.
func WithoutTicker() Option { return func(r *Recorder) { r.done = nil } }

// Register replaces the accepted module and command names; call it before the recorder is shared.
func (r *Recorder) Register(registered map[catalog.Kind][]string) {
	if r != nil {
		r.reg = catalog.NewRegistry(registered)
	}
}

func New(client valkey.Client, log *zap.Logger, registered map[catalog.Kind][]string, opts ...Option) *Recorder {
	r := &Recorder{
		client:    client,
		log:       log,
		reg:       catalog.NewRegistry(registered),
		now:       time.Now,
		counts:    map[slot]int64{},
		distincts: map[distinctSet]map[uint64]struct{}{},
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
	r.AddContext(context.Background(), m, n, dims...)
}

func (r *Recorder) AddContext(ctx context.Context, m catalog.Metric, n int64, dims ...string) {
	if n <= 0 || !r.accepts(m, catalog.Counter, dims) {
		return
	}
	at := r.now()
	s := slot{day: catalog.PeriodStart(catalog.Day, at), key: catalog.CounterKey(at), field: catalog.Field(m, dims...)}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counts[s] += n
	r.join(ctx)
	if len(r.counts) >= flushSlots {
		r.nudge()
	}
}

func (r *Recorder) Distinct(m catalog.Metric, channelID uint64, dims ...string) {
	r.DistinctContext(context.Background(), m, channelID, dims...)
}

func (r *Recorder) DistinctContext(ctx context.Context, m catalog.Metric, channelID uint64, dims ...string) {
	if channelID == 0 || !r.accepts(m, catalog.Distinct, dims) {
		return
	}
	spec, _ := catalog.Lookup(m)
	at := r.now()
	field := catalog.Field(m, dims...)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.join(ctx)
	for _, w := range []catalog.Window{catalog.Day, catalog.Week, catalog.Month} {
		if spec.Windows&w != 0 {
			r.addID(distinctSet{window: w, at: catalog.PeriodStart(w, at), key: catalog.DistinctKey(w, at, field)}, channelID)
		}
	}
}

func (r *Recorder) Cohort(channelID uint64, signup time.Time) {
	r.CohortContext(context.Background(), channelID, signup)
}

func (r *Recorder) CohortContext(ctx context.Context, channelID uint64, signup time.Time) {
	if r == nil || !cohortInput(channelID, signup) {
		return
	}
	at := r.now()
	if signup.After(at) {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.join(ctx)
	incarnation := channelID ^ (uint64(signup.UnixMicro()) * 0x9E3779B97F4A7C15)
	r.addID(distinctSet{window: catalog.Week, at: catalog.PeriodStart(catalog.Week, at), key: catalog.CohortKey(signup, at)}, incarnation)
}

func (r *Recorder) addID(s distinctSet, id uint64) {
	set := r.distincts[s]
	if set == nil {
		set = map[uint64]struct{}{}
		r.distincts[s] = set
	}
	if _, seen := set[id]; seen {
		return
	}
	set[id] = struct{}{}
	r.ids++
	if r.ids >= flushIDs || len(r.distincts) >= flushSlots {
		r.nudge()
	}
}

func (r *Recorder) nudge() {
	select {
	case r.kick <- struct{}{}:
	default:
	}
}

type pending struct {
	handoff   *idempotency.Handoff
	batch     pkg_valkey.ConfirmedBatch
	counts    map[slot]int64
	distincts map[distinctSet]map[uint64]struct{}
}

func (p pending) empty() bool {
	return len(p.counts)+len(p.distincts) == 0
}

func (r *Recorder) drain() pending {
	r.mu.Lock()
	defer r.mu.Unlock()
	p := pending{counts: r.counts, distincts: r.distincts}
	p.handoff, r.next = r.next, nil
	p.batch.ID = "analytics:flush:" + uuid.NewString()
	r.counts, r.distincts, r.ids = map[slot]int64{}, map[distinctSet]map[uint64]struct{}{}, 0
	return p
}

func (r *Recorder) Flush(ctx context.Context) error {
	if r == nil {
		return nil
	}
	r.flushMu.Lock()
	defer r.flushMu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, flushTimeout)
	defer cancel()
	if r.flight != nil {
		if err := r.flushPending(ctx); err != nil {
			return err
		}
	}
	p := r.drain()
	if p.empty() {
		return nil
	}
	r.flight = &p
	return r.flushPending(ctx)
}

func (r *Recorder) join(ctx context.Context) {
	if r.next == nil {
		r.next = &idempotency.Handoff{}
	}
	idempotency.Join(ctx, r.next)
	bus.HoldDelivery(ctx)
}

func (r *Recorder) flushPending(ctx context.Context) error {
	p := r.flight
	err := r.prepare(ctx, p)
	if err == nil {
		err = pkg_valkey.ConfirmCommands(ctx, r.client, p.batch)
	}
	if err != nil {
		if p.handoff != nil {
			p.handoff.Notify(err)
		}
		r.log.Warn("analytics flush unconfirmed", zap.Error(err))
		return err
	}
	if p.handoff != nil {
		p.handoff.Resolve(nil)
	}
	r.flight = nil
	return nil
}

func (r *Recorder) prepare(ctx context.Context, p *pending) error {
	if p.batch.Commands != nil {
		return nil
	}
	distincts, err := r.distinctCmds(ctx, p.distincts)
	if err != nil {
		return err
	}
	cmds := r.counterCmds(p.counts)
	cmds = append(cmds, distincts...)
	p.batch.Commands = make([][]string, 0, len(cmds))
	for _, cmd := range cmds {
		p.batch.Commands = append(p.batch.Commands, append([]string(nil), cmd.Commands()...))
	}
	return nil
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

func (r *Recorder) distinctCmds(ctx context.Context, distincts map[distinctSet]map[uint64]struct{}) (valkey.Commands, error) {
	cmds := make(valkey.Commands, 0, 2*len(distincts))
	expiries := map[string]time.Time{}
	salts := map[saltRef]saltResult{}
	for s, ids := range distincts {
		salt, err := r.flushSalt(ctx, s, salts)
		if err != nil {
			return nil, err
		}
		elements := make([]string, 0, len(ids))
		for id := range ids {
			elements = append(elements, element(salt, id))
		}
		index := catalog.IndexKey(s.window, s.at)
		for chunk := range slices.Chunk(elements, distinctChunk) {
			cmds = append(cmds, pkg_valkey.SaddCmd(r.client, s.key, chunk...))
		}
		cmds = append(cmds, pkg_valkey.SaddCmd(r.client, index, s.key))
		expiry := catalog.DataExpiry(s.window, s.at)
		expiries[s.key] = expiry
		expiries[index] = expiry
	}
	return append(cmds, r.expireCmds(expiries)...), nil
}

func (r *Recorder) expireCmds(expiries map[string]time.Time) valkey.Commands {
	cmds := make(valkey.Commands, 0, len(expiries))
	for key, at := range expiries {
		cmds = append(cmds, pkg_valkey.ExpireAtCmd(r.client, key, at.Unix()))
	}
	return cmds
}

func (r *Recorder) flushSalt(ctx context.Context, s distinctSet, salts map[saltRef]saltResult) ([]byte, error) {
	ref := saltRef{window: s.window, period: catalog.Period(s.window, s.at)}
	result, ok := salts[ref]
	if !ok {
		result.value, result.err = r.salt(ctx, s.window, s.at)
		salts[ref] = result
	}
	return result.value, result.err
}

func (r *Recorder) salt(ctx context.Context, w catalog.Window, at time.Time) ([]byte, error) {
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
	return existing, nil
}

func element(salt []byte, id uint64) string {
	var raw [8]byte
	binary.BigEndian.PutUint64(raw[:], id)
	mac := hmac.New(sha256.New, salt)
	mac.Write(raw[:])
	return string(mac.Sum(nil)[:elementBytes])
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
