// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package custom

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"ItsBagelBot/app/gossip/internal/core"
	"ItsBagelBot/app/gossip/internal/provider"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/monitor"
	"ItsBagelBot/pkg/ratelimit"

	"go.uber.org/zap"
)

const (
	providerName  = "custom"
	fetchEndpoint = "fetch"

	// Must stay below sesame's customFetchRPCTimeout so a timeout still arrives as a reply.
	fetchTimeout   = 3 * time.Second
	upstreamBudget = 2500 * time.Millisecond

	negativeTTL = 15 * time.Second

	positiveTTLDflt = 30 * time.Second

	breakerThreshold = 5
	breakerTTL       = 60 * time.Second

	maxValues     = 5
	maxValueRunes = 256

	// Must equal SAMPLE_MAX_BYTES in JsonTree.svelte: the client rejects anything larger.
	maxSampleBytes = 128 * 1024

	authHeaderName = "Authorization"
)

type Config struct {
	ChannelRateLimit float64
	DefRateLimit     float64
	HostRateLimit    float64
	PositiveTTL      time.Duration
}

type api struct {
	http    *core.HTTPClient
	cache   *core.Cache
	log     *zap.Logger
	defs    provider.DefSource
	keys    provider.FetchKeyResolver
	limiter *ratelimit.Limiter

	channelBuckets core.Buckets
	defBuckets     core.Buckets
	hostBuckets    core.Buckets

	positiveTTL time.Duration

	admit func(ctx context.Context, fl *flight, isPremium bool) error

	fails syncMapHosts
}

type syncMapHosts struct {
	m sync.Map
}

func (h *syncMapHosts) counter(host string) *atomic.Int32 {
	if c, ok := h.m.Load(host); ok {
		return c.(*atomic.Int32)
	}
	c := &atomic.Int32{}
	actual, _ := h.m.LoadOrStore(host, c)
	return actual.(*atomic.Int32)
}

// Must not declare .Trusted(): user-defined fetches egress via WARP.
func New(cfg Config, d provider.Deps) provider.Provider {
	b := provider.NewProvider(providerName, d)
	p := newAPI(cfg, d, b)
	b.Endpoint(fetchEndpoint).Timeout(fetchTimeout).Handle(p.fetch)
	return b.Build()
}

func newAPI(cfg Config, d provider.Deps, b *provider.Builder) *api {
	p := &api{
		http:           b.Client("", nil, fetchTimeout),
		cache:          d.Cache,
		log:            d.Logger(),
		defs:           d.FetchDefs,
		keys:           d.FetchKeys,
		limiter:        d.Limiter,
		channelBuckets: core.NewBuckets("ratelimit:gossip:custom:ch", cfg.ChannelRateLimit, 60),
		defBuckets:     core.NewBuckets("ratelimit:gossip:custom:def", cfg.DefRateLimit, 60),
		hostBuckets:    core.NewBuckets("ratelimit:gossip:custom:host", cfg.HostRateLimit, 60),
		positiveTTL:    cfg.PositiveTTL,
	}
	if p.positiveTTL <= 0 {
		p.positiveTTL = positiveTTLDflt
	}
	p.admit = p.spendBudget
	return p
}

var errKeyMissing = errors.New("key missing")

// Must stay a bespoke handler: the byte-flow caches by identity alone and leaks across channels.
func (p *api) fetch(ctx context.Context, req gossiprpc.Request) any {
	start := time.Now()
	fl, status := p.planFlight(ctx, req)
	if fl == nil {
		return fetchReply(status, nil, start)
	}
	b, err := p.dispatch(ctx, req, fl)
	if err != nil {
		return fetchReply(p.classify(err), nil, start)
	}
	var out gossiprpc.CustomFetchReply
	if uerr := codec.Unmarshal(b, &out); uerr != nil {
		monitor.TxnLogger(ctx, p.log).Error("custom fetch reply decode failed", zap.String("def", fl.def.Name), zap.Error(uerr))
		return fetchReply(gossiprpc.FetchUpstreamError, nil, start)
	}
	out.MS = int(time.Since(start).Milliseconds())
	out.Values = capValues(out.Values)
	return out
}

func fetchReply(status gossiprpc.FetchStatus, values []string, start time.Time) gossiprpc.CustomFetchReply {
	return gossiprpc.CustomFetchReply{Status: status, Values: values, MS: int(time.Since(start).Milliseconds())}
}

type flight struct {
	channelID string
	def       *gossiprpc.FetchDef
	inline    bool
	host      string
	path      codec.Path
	key       string
	dryRun    bool
}

func (p *api) planFlight(ctx context.Context, req gossiprpc.Request) (*flight, gossiprpc.FetchStatus) {
	fl, status := p.resolveFlight(ctx, req)
	if fl == nil {
		return nil, status
	}
	return p.admitFlight(ctx, fl)
}

func (p *api) resolveFlight(ctx context.Context, req gossiprpc.Request) (*flight, gossiprpc.FetchStatus) {
	channelID := strings.TrimSpace(req.ChannelID)
	if channelID == "" || missingDefIdentity(req) {
		return nil, gossiprpc.FetchBadDef
	}
	name, tail := splitTokenPath(req.DefID)
	def, inline := p.resolveDef(ctx, req, name)
	if def == nil || !def.IsActive {
		return nil, gossiprpc.FetchBadDef
	}
	path, ok := effectivePath(def, tail)
	if !ok {
		return nil, gossiprpc.FetchBadDef
	}
	return &flight{
		channelID: channelID,
		def:       def,
		inline:    inline,
		path:      path,
		key:       resultKey(strings.ToLower(strings.TrimSpace(req.DefID))),
	}, gossiprpc.FetchOK
}

func (p *api) admitFlight(ctx context.Context, fl *flight) (*flight, gossiprpc.FetchStatus) {
	host, status := p.gateURL(ctx, fl.channelID, fl.def)
	if status != gossiprpc.FetchOK {
		return nil, status
	}
	fl.host = host
	if armed, _ := p.cache.Exists(ctx, breakerKey(host)); armed {
		return nil, p.classify(&core.UpstreamError{Status: http.StatusTooManyRequests, LocalDeny: true})
	}
	return fl, gossiprpc.FetchOK
}

func effectivePath(def *gossiprpc.FetchDef, tail []string) (codec.Path, bool) {
	effective := tail
	if len(effective) == 0 {
		effective = def.JSONPath
	}
	path, err := buildPath(effective)
	if err != nil {
		return nil, false
	}
	return path, true
}

func (p *api) gateURL(ctx context.Context, channelID string, def *gossiprpc.FetchDef) (string, gossiprpc.FetchStatus) {
	log := monitor.TxnLogger(ctx, p.log)
	u, err := url.Parse(def.URL)
	if err != nil || u.Scheme == "" {
		log.Warn("custom fetch has an unparseable url", zap.String("broadcaster", channelID), zap.String("def", def.Name))
		return "", gossiprpc.FetchBadDef
	}
	if serr := core.SSRFCheck(u); serr != nil {
		log.Warn("custom fetch denied by ssrf gate",
			zap.String("broadcaster", channelID), zap.String("def", def.Name), zap.String("reason", ssrfReason(serr)))
		return "", gossiprpc.FetchDenied
	}
	return strings.ToLower(u.Hostname()), gossiprpc.FetchOK
}

func ssrfReason(serr error) string {
	var se *core.SSRFError
	if errors.As(serr, &se) {
		return se.Reason
	}
	return serr.Error()
}

func (p *api) dispatch(ctx context.Context, req gossiprpc.Request, fl *flight) ([]byte, error) {
	fl.dryRun = req.DryRun
	build := func(ctx context.Context) ([]byte, time.Duration, error) {
		return p.produce(ctx, fl)
	}
	switch {
	case fl.inline || req.DryRun:
		b, _, err := build(ctx)
		return b, err
	case req.Fresh:
		return core.CachedBytesFresh(ctx, p.cache, fl.key, p.admitFor(req, fl), build)
	default:
		return core.CachedBytes(ctx, p.cache, fl.key, p.admitFor(req, fl), build)
	}
}

func missingDefIdentity(req gossiprpc.Request) bool {
	return req.DefID == "" && req.Def == nil
}

func (p *api) resolveDef(ctx context.Context, req gossiprpc.Request, name string) (*gossiprpc.FetchDef, bool) {
	if req.Def != nil {
		d := *req.Def
		if d.Name == "" {
			d.Name = name
		}
		return &d, true
	}
	if p.defs == nil || name == "" {
		return nil, false
	}
	def, found, err := p.defs.FetchDef(ctx, req.ChannelID, name)
	if err != nil {
		monitor.TxnLogger(ctx, p.log).Warn("custom fetch def resolve failed", zap.String("broadcaster", req.ChannelID), zap.String("def", name), zap.Error(err))
		return nil, false
	}
	if !found {
		return nil, false
	}
	def.Name = name
	return &def, false
}

func (p *api) produce(ctx context.Context, fl *flight) ([]byte, time.Duration, error) {
	body, err := p.fetchUpstream(ctx, fl)
	if err != nil {
		return p.failureReply(err)
	}
	return p.shapeFetched(fl, body)
}

func (p *api) shapeFetched(fl *flight, body []byte) ([]byte, time.Duration, error) {
	sample := sampleFor(fl, body)
	values, err := extractValues(body, fl.path)
	if err != nil {
		return marshalSampled(gossiprpc.FetchBadDef, nil, sample), negativeTTL, nil
	}
	return marshalSampled(gossiprpc.FetchOK, values, sample), p.positiveTTL, nil
}

func sampleFor(fl *flight, body []byte) string {
	if !fl.dryRun {
		return ""
	}
	if len(body) > maxSampleBytes {
		return ""
	}
	if !utf8.Valid(body) {
		return ""
	}
	return string(body)
}

func (p *api) failureReply(err error) ([]byte, time.Duration, error) {
	var ue *core.UpstreamError
	switch {
	case errors.As(err, &ue):
		return upstreamStatusReply(ue, err)
	case errors.Is(err, errKeyMissing):
		return marshalReply(gossiprpc.FetchBadDef, nil), negativeTTL, nil
	case errors.Is(err, core.ErrWARPDown):
		return marshalReply(gossiprpc.FetchLimited, nil), 0, nil
	}
	return nil, 0, err
}

func upstreamStatusReply(ue *core.UpstreamError, err error) ([]byte, time.Duration, error) {
	switch {
	case ue.Status == http.StatusBadRequest || ue.Status == http.StatusNotFound:
		return marshalReply(gossiprpc.FetchUpstreamError, nil), negativeTTL, nil
	case ue.Status == http.StatusTooManyRequests && ue.LocalDeny:
		return marshalReply(gossiprpc.FetchLimited, nil), 0, nil
	case ue.Status == http.StatusTooManyRequests:
		return marshalReply(gossiprpc.FetchLimited, nil), core.ThrottleTTL, nil
	}
	return nil, 0, err
}

func (p *api) classify(err error) gossiprpc.FetchStatus {
	switch {
	case timeoutClass(err):
		return gossiprpc.FetchTimeout
	case policyDenied(err):
		return gossiprpc.FetchDenied
	case errors.Is(err, core.ErrWARPDown):
		return gossiprpc.FetchLimited
	}
	var ue *core.UpstreamError
	if errors.As(err, &ue) {
		return upstreamStatusClass(ue)
	}
	if breakerClass(err) {
		return gossiprpc.FetchTimeout
	}
	return gossiprpc.FetchUpstreamError
}

func timeoutClass(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

func policyDenied(err error) bool {
	var se *core.SSRFError
	return errors.As(err, &se) || errors.Is(err, core.ErrBlockedAddressPolicy)
}

func upstreamStatusClass(ue *core.UpstreamError) gossiprpc.FetchStatus {
	if ue.Status == http.StatusTooManyRequests {
		return gossiprpc.FetchLimited
	}
	return gossiprpc.FetchUpstreamError
}

func (p *api) fetchUpstream(ctx context.Context, fl *flight) ([]byte, error) {
	headers, err := p.authHeaders(ctx, fl.channelID, fl.def.KeyLabel)
	if err != nil {
		return nil, err
	}

	fctx, cancel := context.WithTimeout(ctx, upstreamBudget)
	defer cancel()
	body, err := p.http.FetchBounded(fctx, core.Request{
		Method:  http.MethodGet,
		Path:    fl.def.URL,
		Headers: headers,
	})
	if err != nil {
		if breakerClass(err) {
			p.recordFailure(ctx, fl.host)
		} else {
			p.resetFailure(fl.host)
		}
		return nil, err
	}
	p.resetFailure(fl.host)
	return body, nil
}

func (p *api) authHeaders(ctx context.Context, channelID, label string) (map[string]string, error) {
	if label == "" {
		return nil, nil
	}
	if p.keys == nil {
		return nil, errKeyMissing
	}
	key, err := p.keys.FetchKey(ctx, channelID, label)
	if err != nil {
		return nil, fmt.Errorf("resolve fetch key %q: %w", label, err)
	}
	if key == "" {
		return nil, errKeyMissing
	}
	return map[string]string{authHeaderName: "Bearer " + key}, nil
}

func breakerClass(err error) bool {
	var ue *core.UpstreamError
	if errors.As(err, &ue) {
		return false
	}
	if errors.Is(err, core.ErrContentTypeNotAllowed) || errors.Is(err, core.ErrBodyTooLarge) {
		return false
	}
	if errors.Is(err, core.ErrBlockedAddressPolicy) {
		return false
	}
	return true
}

func (p *api) recordFailure(ctx context.Context, host string) {
	n := p.fails.counter(host).Add(1)
	if n < breakerThreshold {
		return
	}
	p.fails.counter(host).Store(0)
	if won, err := p.cache.Claim(ctx, breakerKey(host), breakerTTL); err != nil {
		monitor.TxnLogger(ctx, p.log).Warn("custom fetch breaker claim failed", zap.String("host", host), zap.Error(err))
	} else if won {
		p.log.Warn("custom fetch breaker armed",
			zap.String("host", host), zap.Int("consecutive_failures", breakerThreshold), zap.Duration("ttl", breakerTTL))
	}
}

func (p *api) resetFailure(host string) {
	p.fails.counter(host).Store(0)
}

func (p *api) spendBudget(ctx context.Context, fl *flight, isPremium bool) error {
	if p.limiter == nil {
		return nil
	}
	layers := []core.Buckets{
		p.channelBuckets.WithKey("ratelimit:gossip:custom:ch:" + fl.channelID),
		p.defBuckets.WithKey("ratelimit:gossip:custom:def:" + fl.def.Name),
		p.hostBuckets.WithKey("ratelimit:gossip:custom:host:" + fl.host),
	}
	for _, b := range layers {
		if err := b.Enforce(ctx, p.limiter, isPremium); err != nil {
			return err
		}
	}
	return nil
}

func (p *api) admitFor(req gossiprpc.Request, fl *flight) func(context.Context) error {
	if p.admit == nil {
		return nil
	}
	return func(ctx context.Context) error {
		return p.admit(ctx, fl, req.IsPremium)
	}
}

func breakerKey(host string) string { return "gossip:custom:cb:" + host }

func resultKey(defID string) string {
	sum := sha256.Sum256([]byte(defID))
	return "gossip:custom:fetch:" + hex.EncodeToString(sum[:8])
}

func marshalReply(status gossiprpc.FetchStatus, values []string) []byte {
	return marshalSampled(status, values, "")
}

func marshalSampled(status gossiprpc.FetchStatus, values []string, sample string) []byte {
	b, err := codec.Marshal(gossiprpc.CustomFetchReply{Status: status, Values: values, Sample: sample})
	if err != nil {
		return []byte(`{"status":"upstream_error","ms":0}`)
	}
	return b
}

func capValues(values []string) []string {
	if len(values) > maxValues {
		values = values[:maxValues]
	}
	for i, v := range values {
		values[i] = truncateRunes(v, maxValueRunes)
	}
	return values
}

func truncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
