// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package linkcheck

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"

	"ItsBagelBot/internal/moderation"

	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

// Worker-pool shape, recorded so it can be re-argued:
//
//   - queueCap 256: even a fleet-wide chat flood where EVERY line carries a
//     fresh host fills this in seconds; overflow drops (the next mention of
//     the same host re-enqueues) rather than blocking the gate's hot path.
//     A bigger buffer would only delay work that is stale by then anyway.
//   - defaultWorkers 2: one oracle query measures ~20-80ms, so two workers
//     sustain ~50 lookups/s — far above observed chat link rates — while more
//     workers would just multiply load against free public endpoints.
//   - dohRate burst 10 / 5qps: stays well inside what 1.1.1.2 expects from a
//     single polite client; bursts cover a raid-shaped spike of fresh hosts
//     without turning sesame into a resolver stress tool.
const (
	queueCap       = 256
	defaultWorkers = 2
	dohBurst       = 10
	dohQPS         = 5
)

// Source names which oracle convicted a host, carried on Hit for the audit log.
type Source string

const (
	SourceFeed  Source = "feed"
	SourceFloor Source = "floor"
	SourceDoH   Source = "doh"
)

// Hit records one conviction, delivered through OnBad. Shadow-mode operators
// read these to judge rule quality before arming enforcement; nothing else
// consumes them.
type Hit struct {
	Host    string // offending host as classified (destination for expansions)
	Token   string // chat token that carried it ("bit.ly/xyz")
	Via     string // shortener host walked to reach Host, "" when direct
	Source  Source
	Channel uint64
	Sender  string
}

// Options configures a Checker. Nil members select production defaults;
// tests inject httptest-backed ones.
type Options struct {
	ExpandShorteners bool
	Workers          int
	Feeds            *Feeds
	DoH              *DoH
	Expander         *Expander
	Log              *zap.Logger
}

// Checker resolves unknown link hosts off-thread and remembers the verdicts.
// The gate calls Evaluate synchronously on every line containing a dot: reads
// only (cache, feed snapshot), so the hot path never waits on network I/O and
// an unarmed (nil) checker costs one atomic load.
type Checker struct {
	opts    Options
	feeds   *Feeds
	doh     *DoH
	exp     *Expander
	cache   *cache
	queue   chan task
	log     *zap.Logger
	limiter *rate.Limiter

	mu       sync.Mutex
	inflight map[string]struct{}
	cooldown map[string]int64 // key -> unix nanos when retries resume

	// OnBad receives each conviction as it lands. It runs on a worker
	// goroutine: implementations must not block. nil is valid (drop).
	OnBad func(Hit)

	dropped atomic.Int64
}

// task is one pending resolution. Token keeps its original case and path (the
// expansion input); Host is the lowercased host component; Key is the dedup /
// cache slot this result will land in.
type task struct {
	token  string
	host   string
	key    string
	ch     uint64
	sender string
}

// NewChecker builds a checker around opts. It does NOT start goroutines —
// call Start with the service context.
func NewChecker(opts Options) *Checker {
	if opts.Workers <= 0 {
		opts.Workers = defaultWorkers
	}
	if opts.Log == nil {
		opts.Log = zap.NewNop()
	}
	if opts.Feeds == nil {
		opts.Feeds = NewFeeds(nil, nil)
	}
	if opts.DoH == nil {
		opts.DoH = NewDoH("", nil)
	}
	if opts.Expander == nil {
		opts.Expander = NewExpander(nil, nil)
	}
	return &Checker{
		opts:     opts,
		feeds:    opts.Feeds,
		doh:      opts.DoH,
		exp:      opts.Expander,
		cache:    newCache(),
		queue:    make(chan task, queueCap),
		log:      opts.Log,
		limiter:  rate.NewLimiter(dohQPS, dohBurst),
		inflight: make(map[string]struct{}),
		cooldown: make(map[string]int64),
	}
}

// Start launches the worker pool; it returns immediately. Workers exit when
// ctx cancels.
func (c *Checker) Start(ctx context.Context) {
	for i := 0; i < c.opts.Workers; i++ {
		go c.work(ctx)
	}
}

// RefreshFeeds re-pulls the blocklist snapshot; the runner calls it on a slow
// ticker. A failure logs through the checker logger and keeps the old set.
func (c *Checker) RefreshFeeds(ctx context.Context) (int, error) {
	n, err := c.feeds.Refresh(ctx)
	if n > 0 {
		c.log.Info("linkcheck feeds refreshed", zap.Int("hosts", n))
	}
	return n, err
}

// Evaluate is the gate's synchronous entry point. It scans text for link
// tokens; any host already known Bad (cache or live feed snapshot) reports
// true so the caller can act; unknown hosts are enqueued for background
// classification and report nothing. Returns false immediately for a nil
// checker, which is how an unarmed gate stays byte-identical to before.
func (c *Checker) Evaluate(text string, channel uint64, sender string) bool {
	if c == nil {
		return false
	}
	bad := false
	var seen map[string]struct{} // lazily built; most lines carry no links

	iterLinkTokens(text, func(token string) {
		host := strings.ToLower(hostOf(token))
		if !validHost(host) {
			return
		}
		if seen == nil {
			seen = make(map[string]struct{}, 4)
		}
		if _, dup := seen[token]; dup {
			return
		}
		seen[token] = struct{}{}

		if c.knownBad(token, host) {
			bad = true
			return
		}
		c.resolveAsync(c.taskFor(token, host, channel, sender))
	})
	return bad
}

// knownBad reports whether token/host already sits convicted: the token slot
// covers expanded shorteners (bit.ly/abc is its own cache entry, since the
// destination is per-path), the host and folded-host slots cover everything
// else. Feed membership counts as known-bad without waiting for a worker —
// the snapshot is already in memory.
func (c *Checker) knownBad(token, host string) bool {
	folded := foldHost(host)
	for _, k := range [3]string{host, folded, strings.ToLower(token)} {
		if v, ok := c.cache.get(k); ok && v == Bad {
			return true
		}
		if c.feeds.Has(k) {
			return true
		}
	}
	return false
}

// taskFor picks a lookup's identity: shortener tokens key on their full path
// form (the destination is per-path), plain hosts on the registrable fold so
// subdomain churn collapses into one cache slot.
func (c *Checker) taskFor(token, host string, channel uint64, sender string) task {
	if c.exp.IsShortener(host) {
		lt := strings.ToLower(token)
		return task{token: lt, host: host, key: lt, ch: channel, sender: sender}
	}
	return task{token: token, host: host, key: foldHost(host), ch: channel, sender: sender}
}

// resolveAsync schedules one background classification unless its key is
// cached, still in flight, cooling down from an error, or the queue is full
// (a counted drop — the next mention re-enqueues).
func (c *Checker) resolveAsync(t task) {
	if _, done := c.cache.get(t.key); done {
		return
	}
	if !c.claim(t.key) {
		return
	}

	select {
	case c.queue <- t:
	default:
		c.release(t.key)
		n := c.dropped.Add(1)
		if n%128 == 1 {
			c.log.Warn("linkcheck queue full, dropping lookups", zap.Int64("dropped", n))
		}
	}
}

// claim reserves key for a worker, refusing keys that are already in flight
// or inside their post-error cooldown window.
func (c *Checker) claim(key string) bool {
	now := nowNanos()
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, busy := c.inflight[key]; busy {
		return false
	}
	if c.cooldown[key] > now {
		return false
	}
	c.inflight[key] = struct{}{}
	return true
}

func (c *Checker) release(key string) {
	c.mu.Lock()
	delete(c.inflight, key)
	c.mu.Unlock()
}

// work drains the queue until ctx cancels. Every path through handle releases
// the inflight mark exactly once — the defer lives here, not in handle, so a
// panic in one classification cannot leak the mark and wedge that key forever.
func (c *Checker) work(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case t := <-c.queue:
			c.handle(ctx, t)
			c.release(t.key)
		}
	}
}

// handle resolves one task into the cache and, when convicted, fires OnBad.
func (c *Checker) handle(ctx context.Context, t task) {
	if c.exp.IsShortener(t.host) && c.opts.ExpandShorteners {
		c.handleExpansion(ctx, t)
		return
	}
	res := c.classifyHost(ctx, t.host)
	if res.err != nil {
		// An unanswered question must not cache as either answer: cool the
		// key down so the next mention retries instead of whitelisting a
		// possibly-hostile host for a Clean TTL.
		c.coolDown(t.key)
		return
	}
	c.remember(t.host, t.key, res.verdict)
	if res.verdict == Bad {
		c.fire(Hit{Host: t.host, Token: t.token, Source: res.source, Channel: t.ch, Sender: t.sender})
	}
}

// handleExpansion walks a shortener chain and classifies where it lands,
// caching under BOTH the destination host and the shortener token (shorter
// TTL there: destinations behind a stable bit.ly/abc rotate server-side).
func (c *Checker) handleExpansion(ctx context.Context, t task) {
	destHost, err := c.exp.Destination(ctx, t.token)
	if err != nil {
		c.coolDown(t.key)
		c.log.Debug("linkcheck expansion failed", zap.String("token", t.token), zap.Error(err))
		return
	}
	res := c.classifyHost(ctx, destHost)
	if res.err != nil {
		c.coolDown(t.key)
		return
	}
	c.remember(destHost, destHost, res.verdict)
	c.cache.put(t.key, res.verdict, true)
	if res.verdict == Bad {
		c.fire(Hit{Host: destHost, Token: t.token, Via: t.host, Source: res.source, Channel: t.ch, Sender: t.sender})
	}
}

// remember stores a verdict under its host and its registrable fold (one
// entry when they coincide).
func (c *Checker) remember(host, key string, v Verdict) {
	c.cache.put(key, v, false)
	if f := foldHost(host); f != key {
		c.cache.put(f, v, false)
	}
}

// classification is one host's verdict plus which oracle produced it. A
// non-nil err means "no answer" — callers cool down rather than caching, so
// verdict/source are only meaningful when err is nil.
type classification struct {
	verdict Verdict
	source  Source
	err     error
}

// classifyHost runs the passive oracles in cost order: memory-resident feed
// snapshot, allocation-free floor terms, then the network resolver (rate-
// limited). A non-nil err means "no answer" — callers cool down rather than
// caching.
func (c *Checker) classifyHost(ctx context.Context, host string) classification {
	if c.feeds.Has(host) {
		return classification{verdict: Bad, source: SourceFeed}
	}
	if kind, _ := moderation.MatchFloor(moderation.Normalize(nil, host)); kind != moderation.FloorNone {
		// The floor list also convicts dynamically here so ops additions to
		// IPLoggerDomains reach hosts that only surface wrapped in a shortener.
		return classification{verdict: Bad, source: SourceFloor}
	}
	if err := c.limiter.Wait(ctx); err != nil {
		return classification{verdict: Clean, err: err} // ctx cancelled mid-shutdown
	}
	blocked, err := c.doh.Blocked(ctx, host)
	if err != nil {
		c.log.Debug("linkcheck oracle error", zap.String("host", host), zap.Error(err))
		return classification{verdict: Clean, err: err}
	}
	if blocked {
		return classification{verdict: Bad, source: SourceDoH}
	}
	return classification{verdict: Clean}
}

// coolDown blocks retries for key until errCooldown elapses.
func (c *Checker) coolDown(key string) {
	c.mu.Lock()
	c.cooldown[key] = nowNanos() + int64(errCooldown)
	c.mu.Unlock()
}

// fire delivers a conviction when anyone is listening.
func (c *Checker) fire(h Hit) {
	if c.OnBad != nil {
		c.OnBad(h)
	}
}
