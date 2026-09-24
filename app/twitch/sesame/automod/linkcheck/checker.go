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

const (
	queueCap       = 256
	defaultWorkers = 2
	dohBurst       = 10
	dohQPS         = 5
)

type Source string

const (
	SourceFeed  Source = "feed"
	SourceFloor Source = "floor"
	SourceDoH   Source = "doh"
)

type Hit struct {
	Host    string
	Token   string
	Via     string
	Source  Source
	Channel uint64
	Sender  string
}

type Options struct {
	ExpandShorteners bool
	Workers          int
	Feeds            *Feeds
	DoH              *DoH
	Expander         *Expander
	Log              *zap.Logger
}

type Checker struct {
	opts    Options
	feeds   *Feeds
	doh     *DoH
	exp     *Expander
	cache   *cache
	queue   chan task
	log     *zap.Logger
	limiter *rate.Limiter

	mu              sync.Mutex
	inflight        map[string]struct{}
	retryAfterNanos map[string]int64

	OnBad func(Hit)

	dropped atomic.Int64
}

type task struct {
	token  string
	host   string
	key    string
	ch     uint64
	sender string
}

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
		opts:            opts,
		feeds:           opts.Feeds,
		doh:             opts.DoH,
		exp:             opts.Expander,
		cache:           newCache(),
		queue:           make(chan task, queueCap),
		log:             opts.Log,
		limiter:         rate.NewLimiter(dohQPS, dohBurst),
		inflight:        make(map[string]struct{}),
		retryAfterNanos: make(map[string]int64),
	}
}

func (c *Checker) Start(ctx context.Context) {
	for i := 0; i < c.opts.Workers; i++ {
		go c.work(ctx)
	}
}

func (c *Checker) RefreshFeeds(ctx context.Context) (int, error) {
	n, err := c.feeds.Refresh(ctx)
	if n > 0 {
		c.log.Info("linkcheck feeds refreshed", zap.Int("hosts", n))
	}
	return n, err
}

func (c *Checker) Evaluate(text string, channel uint64, sender string) bool {
	if c == nil {
		return false
	}
	bad := false
	var seen map[string]struct{}

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
		c.resolveAsync(c.keyed(task{token: token, host: host, ch: channel, sender: sender}))
	})
	return bad
}

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

func (c *Checker) keyed(t task) task {
	if c.exp.IsShortener(t.host) {
		lt := strings.ToLower(t.token)
		t.token, t.key = lt, lt
		return t
	}
	t.key = foldHost(t.host)
	return t
}

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

func (c *Checker) claim(key string) bool {
	now := nowNanos()
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, busy := c.inflight[key]; busy {
		return false
	}
	if c.retryAfterNanos[key] > now {
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

func (c *Checker) handle(ctx context.Context, t task) {
	if c.exp.IsShortener(t.host) && c.opts.ExpandShorteners {
		c.handleExpansion(ctx, t)
		return
	}
	res := c.classifyHost(ctx, t.host)
	if res.err != nil {
		c.coolDown(t.key)
		return
	}
	c.remember(t.host, t.key, res.verdict)
	if res.verdict == Bad {
		c.fire(Hit{Host: t.host, Token: t.token, Source: res.source, Channel: t.ch, Sender: t.sender})
	}
}

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

func (c *Checker) remember(host, key string, v Verdict) {
	c.cache.put(key, v, false)
	if f := foldHost(host); f != key {
		c.cache.put(f, v, false)
	}
}

type classification struct {
	verdict Verdict
	source  Source
	err     error
}

func (c *Checker) classifyHost(ctx context.Context, host string) classification {
	if c.feeds.Has(host) {
		return classification{verdict: Bad, source: SourceFeed}
	}
	if kind, _ := moderation.MatchFloor(moderation.Normalize(nil, host)); kind != moderation.FloorNone {
		return classification{verdict: Bad, source: SourceFloor}
	}
	if err := c.limiter.Wait(ctx); err != nil {
		return classification{verdict: Clean, err: err}
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

func (c *Checker) coolDown(key string) {
	c.mu.Lock()
	c.retryAfterNanos[key] = nowNanos() + int64(errCooldown)
	c.mu.Unlock()
}

func (c *Checker) fire(h Hit) {
	if c.OnBad != nil {
		c.OnBad(h)
	}
}
