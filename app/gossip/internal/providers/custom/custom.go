// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package custom is the $(urlfetch) provider: it executes broadcaster-
// authored fetch definitions — a stored https URL, an optional JSON dot-path,
// an optional stored API-key label — for the {urlfetch:name} token sesame
// expands and the dashboard rehearses.
//
// Every request is untrusted by construction, so the whole stack tightens:
// definitions resolve through the commands-service projection (DefSource),
// keys decrypt just-in-time over the internal RPC and ride exactly one fetch,
// the SSRF gate runs in the handler AND again inside core before any dial,
// egress rides the WARP sidecar (this provider never declares .Trusted()),
// three rate-limit layers bound channel/definition/host spend, a fleet-wide
// breaker opens on dead upstreams, and replies carry extracted strings only —
// callers never see raw bodies.
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

	// fetchTimeout is the endpoint's declared budget (the engine wraps every
	// handler in it). The 2.5s fetch budget below lives INSIDE this, leaving
	// ~500ms for def resolution, buckets, extraction and marshal — sized so
	// three permitted redirect hops at ~700ms connect+TTFB each across the
	// WARP edge still fit, which is the realistic worst case for an
	// allow-listed URL. Sesame wraps its own call at 3.5s so a timeout here
	// surfaces as this reply instead of a client-side abort.
	fetchTimeout = 3 * time.Second
	// upstreamBudget bounds one full HTTP exchange (all hops). Per-hop
	// deadlines are NOT fixed numbers: net/http propagates this context into
	// every dial/write/read of every hop, so each hop gets exactly whatever
	// wall clock remains — a first hop that burns 2s leaves 0.5s for the
	// second, never a fresh allowance per attempt.
	upstreamBudget = 2500 * time.Millisecond

	// negativeTTL pins friendly absences: upstream 400/404, an unresolvable
	// path, a dangling key label. Long enough to shield the upstream from an
	// author's retry storm; short enough that fixing the definition heals in
	// seconds. Infrastructure failures are never cached at all (they teach us
	// nothing about the key), nor are local denials (a bucket refills in
	// seconds and must retry).
	negativeTTL = 15 * time.Second

	// positiveTTLDflt is the fresh window for successful results. Chat bursts
	// collapse onto one entry while live-data tokens stay honest; 30s sits
	// between the failure modes on either side — 5 minutes serves weather or
	// prices that stopped being true mid-stream, and 5 seconds lets a busy
	// channel re-dial its upstream every time the token re-fires. Env-tunable
	// (CUSTOM_FETCH_POSITIVE_TTL) pending traffic modelling.
	positiveTTLDflt = 30 * time.Second

	// breakerThreshold arms the host circuit after this many CONSECUTIVE
	// transport failures (connect refused, DNS, timeout — anything where no
	// usable response arrived). Five, not three: a single blip — one dropped
	// SYN, one conntrack reap — is routine on a fleet doing thousands of dials
	// an hour, and arming on it would flap the circuit during ordinary chat
	// bursts. Five consecutive means the host (or the tunnel to it) is
	// actually gone. Any answered request resets the count, even a 500.
	breakerThreshold = 5
	// breakerTTL is how long an armed circuit stays open. Sixty seconds
	// matches the SWR claim flooring in core: short enough that recovery
	// never feels eternal in chat, long enough that a dead origin stops
	// eating one dial per request from every replica.
	breakerTTL = 60 * time.Second

	// maxValues / maxValueRunes cap the reply server-side (<4KB): sesame
	// renders at most a few short values per line anyway, and gossip capping
	// its own replies is defense in depth behind ExternalVar's sanitize+cap
	// at the variable boundary — never a substitute for it.
	maxValues     = 5
	maxValueRunes = 256

	// authHeaderName is HOW a stored key attaches to a user-defined fetch.
	// The definition schema carries only key_label (no auth-type field), so
	// the attachment convention must be one fixed shape: Bearer, the scheme
	// the overwhelming majority of public JSON APIs use. Endpoints wanting a
	// custom header family remain a follow-up, not a per-request guess.
	authHeaderName = "Authorization"
)

// Config carries the provider's tunables. All three rate limits are requests
// per minute window; defaults are the spec placeholders, env-tunable pending
// traffic modelling.
type Config struct {
	ChannelRateLimit float64       // per-channel budget (default 6/min)
	DefRateLimit     float64       // per-definition fleet budget (default 30/min)
	HostRateLimit    float64       // per-target-host fleet budget (default 120/min)
	PositiveTTL      time.Duration // success cache fresh window (default 30s)
}

// api holds the provider's runtime pieces; the declared endpoint captures it.
type api struct {
	http    *core.HTTPClient // WARP-lane client; base "" because the URL varies per request and rides Request.Path whole
	cache   *core.Cache
	log     *zap.Logger
	defs    provider.DefSource
	keys    provider.FetchKeyResolver
	limiter *ratelimit.Limiter

	channelBuckets core.Buckets
	defBuckets     core.Buckets
	hostBuckets    core.Buckets

	positiveTTL time.Duration

	// admit is the budget seam: the production implementation spends the
	// three bucket layers; tests stub it to stage denials without a Valkey.
	admit func(ctx context.Context, fl *flight, isPremium bool) error

	// fails tracks consecutive transport failures per target host,
	// pod-locally; arming itself goes through the store (Claim), so the open
	// circuit is authoritative fleet-wide like an SWR refresh claim.
	fails syncMapHosts
}

// syncMapHosts is host -> *atomic.Int32 with a mutex-guarded slow path.
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

// New builds the custom provider. It deliberately does NOT declare .Trusted():
// user-defined fetches egress via the WARP sidecar, and the inverted default
// is what makes a forgotten flag fail toward hidden egress. d.FetchDefs nil
// disables the whole provider (providers.All skips it) — with no definition
// source there is nothing to execute.
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

// errKeyMissing is a definition naming a label with no key on file (or no key
// resolver wired). Fail closed, never send unauthenticated.
var errKeyMissing = errors.New("key missing")

// fetch answers bagel.rpc.gossip.custom.fetch. It is a bespoke Handle
// endpoint, not a byte-flow: the base URL varies per request and the flow
// skeleton caches by identity alone, which would leak channel A's answer into
// channel B's entry.
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
	// Stamp total handler latency onto whatever the cache/build returned. One
	// small decode/remarshal even on hits — the reply is bounded well under
	// 4KB, and MS belongs to the whole handler, hit or miss.
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

// flight bundles everything one admitted fetch needs downstream: the resolved
// definition, its vetted host, the effective extraction path and the cache
// key. Built once by planFlight so the dispatch and produce layers stop
// threading the same five values as loose arguments.
type flight struct {
	channelID string
	def       *gossiprpc.FetchDef
	inline    bool
	host      string
	path      codec.Path
	key       string
}

// planFlight runs the whole admission preamble — request shape, definition
// resolve, path build, URL gate, breaker — and returns the admitted flight.
// nil means answer the returned status without dialing anything.
func (p *api) planFlight(ctx context.Context, req gossiprpc.Request) (*flight, gossiprpc.FetchStatus) {
	fl, status := p.resolveFlight(ctx, req)
	if fl == nil {
		return nil, status
	}
	return p.admitFlight(ctx, fl)
}

// resolveFlight owns the authoring half of admission: request shape, the
// definition itself and the effective extraction path.
func (p *api) resolveFlight(ctx context.Context, req gossiprpc.Request) (*flight, gossiprpc.FetchStatus) {
	channelID := strings.TrimSpace(req.ChannelID)
	if channelID == "" || missingDefIdentity(req) {
		// A caller bug, not an upstream condition; bad_def keeps the author's
		// token visible rather than chatting an error about our own wire.
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
		// Keyed by the FULL id as the caller addressed it — "<name>" bare or
		// "<name>.<path>": different paths are different answers and must
		// never share an entry.
		key: resultKey(strings.ToLower(strings.TrimSpace(req.DefID))),
	}, gossiprpc.FetchOK
}

// admitFlight owns the dialing half: the URL gate and the host breaker.
func (p *api) admitFlight(ctx context.Context, fl *flight) (*flight, gossiprpc.FetchStatus) {
	host, status := p.gateURL(ctx, fl.channelID, fl.def)
	if status != gossiprpc.FetchOK {
		return nil, status
	}
	fl.host = host
	// Breaker check BEFORE buckets: an armed host answers without spending
	// anyone's tokens on a dial we already know is dead. Rehearsal sees the
	// same gate — dry_run weakens nothing but billing. The answer is the same
	// typed local-denial a drained bucket mints (429 + LocalDeny), so the
	// status mapping has exactly one source of truth.
	if armed, _ := p.cache.Exists(ctx, breakerKey(host)); armed {
		return nil, p.classify(&core.UpstreamError{Status: http.StatusTooManyRequests, LocalDeny: true})
	}
	return fl, gossiprpc.FetchOK
}

// effectivePath builds the extraction path for this ask. The console picker
// inserts the FULL dotted path into the token ({urlfetch:name.a.b}), so a
// non-empty token path is the whole path and overrides the def's own; an
// empty one falls back to it. false means malformed authoring — stable, so
// bad_def.
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

// gateURL parses and SSRF-vets the definition's URL, returning its lowered
// host. Gate before anything expensive; Do/FetchBounded re-runs it as defense
// in depth. A refusal is a POLICY denial (denied), distinct from upstream
// breakage — and it costs no bucket, since nothing was fetched.
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

// dispatch runs the flight through the right cache lane. Inline defs and
// rehearsals execute for real but spend no bucket, read no cache and write no
// cache — an unsaved draft can never poison the stored definition's entry
// under the same name.
func (p *api) dispatch(ctx context.Context, req gossiprpc.Request, fl *flight) ([]byte, error) {
	build := func(ctx context.Context) ([]byte, time.Duration, error) {
		return p.produce(ctx, fl)
	}
	switch {
	case fl.inline || req.DryRun:
		b, _, err := build(ctx) // TTL discarded: uncached by design
		return b, err
	case req.Fresh:
		return core.CachedBytesFresh(ctx, p.cache, fl.key, p.admitFor(req, fl), build)
	default:
		return core.CachedBytes(ctx, p.cache, fl.key, p.admitFor(req, fl), build)
	}
}

// missingDefIdentity: the request names nothing to run — no stored id and no
// inline draft.
func missingDefIdentity(req gossiprpc.Request) bool {
	return req.DefID == "" && req.Def == nil
}

// resolveDef returns the definition to run: the inline draft when the request
// carries one (rehearsal — never persisted, never looked up), otherwise the
// projection row for (channelID, name). nil means bad_def.
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

// produce runs the real work for one flight: gated fetch, then extraction. It
// returns ready-to-send reply bytes plus the TTL they may be stored for
// (zero = do not store) and swallows every error it could shape into a reply —
// infrastructure failures come back as errors so the cache layer stores
// nothing.
func (p *api) produce(ctx context.Context, fl *flight) ([]byte, time.Duration, error) {
	body, err := p.fetchUpstream(ctx, fl)
	if err != nil {
		return p.failureReply(err)
	}
	values, xerr := extractValues(body, fl.path)
	if xerr != nil {
		// A path that names nothing (or a non-scalar leaf) is broken
		// authoring: stable, so negative-cache it briefly.
		return marshalReply(gossiprpc.FetchBadDef, nil), negativeTTL, nil
	}
	return marshalReply(gossiprpc.FetchOK, values), p.positiveTTL, nil
}

// failureReply shapes a fetch failure into cacheable reply bytes where the
// condition is stable enough to store, and propagates everything else as an
// error so the cache layer stores nothing.
func (p *api) failureReply(err error) ([]byte, time.Duration, error) {
	var ue *core.UpstreamError
	switch {
	case errors.As(err, &ue):
		return upstreamStatusReply(ue, err)
	case errors.Is(err, errKeyMissing):
		// Dangling key_label: fail closed, and remember briefly — it heals
		// only when the broadcaster relinks a key.
		return marshalReply(gossiprpc.FetchBadDef, nil), negativeTTL, nil
	case errors.Is(err, core.ErrWARPDown):
		// The untrusted lane failed closed. "limited" tells sesame to say
		// [source unavailable] — honest, transient, nobody's fault in chat.
		return marshalReply(gossiprpc.FetchLimited, nil), 0, nil
	}
	return nil, 0, err // timeouts and everything else: propagate uncached
}

// upstreamStatusReply maps an ANSWERED upstream's status onto reply bytes and
// their storage TTL.
func upstreamStatusReply(ue *core.UpstreamError, err error) ([]byte, time.Duration, error) {
	switch {
	case ue.Status == http.StatusBadRequest || ue.Status == http.StatusNotFound:
		return marshalReply(gossiprpc.FetchUpstreamError, nil), negativeTTL, nil
	case ue.Status == http.StatusTooManyRequests && ue.LocalDeny:
		return marshalReply(gossiprpc.FetchLimited, nil), 0, nil // refills in seconds; retry raw
	case ue.Status == http.StatusTooManyRequests:
		return marshalReply(gossiprpc.FetchLimited, nil), core.ThrottleTTL, nil
	}
	return nil, 0, err // 401/403/5xx: real conditions, uncached
}

// classify maps a propagated infrastructure error onto a status. Everything
// reaching here was already deemed uncachable. A transport-class failure —
// anything where no usable response arrived (refused, DNS, TLS, deadline) —
// lands in the timeout family, which is exactly the bucket sesame renders as
// "[source timed out]" for infra trouble; only an ANSWERED-but-bad upstream
// says upstream_error.
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

// timeoutClass: the deadline or a transport-level timeout — no usable
// response arrived.
func timeoutClass(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

// policyDenied: our own SSRF gate refused, at URL-shape or at address time.
func policyDenied(err error) bool {
	var se *core.SSRFError
	return errors.As(err, &se) || errors.Is(err, core.ErrBlockedAddressPolicy)
}

// upstreamStatusClass maps an ANSWERED upstream onto its status family.
func upstreamStatusClass(ue *core.UpstreamError) gossiprpc.FetchStatus {
	if ue.Status == http.StatusTooManyRequests {
		return gossiprpc.FetchLimited
	}
	return gossiprpc.FetchUpstreamError
}

// fetchUpstream resolves the key (if any) and performs one gate-checked GET.
// Transport failures count toward the host breaker; anything that proves the
// host reachable (any response, even 500, even a refused payload) resets it.
func (p *api) fetchUpstream(ctx context.Context, fl *flight) ([]byte, error) {
	headers, err := p.authHeaders(ctx, fl.channelID, fl.def.KeyLabel)
	if err != nil {
		return nil, err // not the target host's fault; no breaker movement
	}

	fctx, cancel := context.WithTimeout(ctx, upstreamBudget)
	defer cancel()
	body, err := p.http.FetchBounded(fctx, core.Request{
		Method:  http.MethodGet,
		Path:    fl.def.URL, // base "" — the whole absolute URL rides Path
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

// authHeaders builds the per-fetch credential header. A labeled key decrypts
// once here and rides exactly one upstream call — never cached, logged, or
// projected.
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

// breakerClass reports whether err is evidence the host/tunnel is unreachable
// (connect refused, DNS, TLS, timeout): the only class that moves the breaker.
// An ANSWERED request — any status, or a payload we refuse on policy — proves
// reachability and resets the circuit instead.
func breakerClass(err error) bool {
	var ue *core.UpstreamError
	if errors.As(err, &ue) {
		return false
	}
	if errors.Is(err, core.ErrContentTypeNotAllowed) || errors.Is(err, core.ErrBodyTooLarge) {
		return false
	}
	if errors.Is(err, core.ErrBlockedAddressPolicy) {
		return false // policy refusal before any dial: proves nothing about the host
	}
	return true
}

// recordFailure counts one transport failure for host and arms the fleet-wide
// circuit at the threshold. The pod-local counter coalesces attempts; the SET
// NX claim in the shared store is what makes the OPEN circuit authoritative
// across replicas.
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

// spendBudget is the production admit: three bucket layers re-keyed per
// caller, exactly as govee re-keys per broadcaster. Order is channel → def →
// host (chat cadence first, abuse ceilings last); each layer is atomic within
// itself, and premium spends only the general half of each, keeping the 25%
// reserve standard traffic never touches.
//
// Per-channel 6/min is bounded by chat cadence — cooldowns already keep a
// command from firing faster than roughly once per few seconds, and 6/min
// absorbs a viewer burst while starving a hot-loop. Lower starves legitimate
// shared-channel use; higher doubles the worst case one broadcaster can pull
// for no observed demand.
//
// Per-def 30/min caps any single definition's aggregate fleet pull (many
// channels referencing one popular API still stay polite).
//
// Per-host 120/min bounds abuse against ONE third-party origin regardless of
// how many defs aim at it — the ceiling that stops a hostile broadcaster from
// burning a public API's goodwill for everyone.
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

// admitFor binds one request's identity to the budget seam, or returns nil
// when there is nothing to spend (tests, nil limiter).
func (p *api) admitFor(req gossiprpc.Request, fl *flight) func(context.Context) error {
	if p.admit == nil {
		return nil
	}
	return func(ctx context.Context) error {
		return p.admit(ctx, fl, req.IsPremium)
	}
}

// breakerKey is the shared-store circuit key for one host.
func breakerKey(host string) string { return "gossip:custom:cb:" + host }

// resultKey derives the cache key for one definition's answer: the spec's
// "gossip:custom:fetch:<hash(def_id)>". The id is hashed, not stored raw, to
// bound key length against any future id grammar drift; 128 bits of SHA-256
// makes collisions impossible at this scale, and the id carries no data we
// would ever need to recover from the key.
func resultKey(defID string) string {
	sum := sha256.Sum256([]byte(defID))
	return "gossip:custom:fetch:" + hex.EncodeToString(sum[:8])
}

// marshalReply renders one CustomFetchReply to wire bytes.
func marshalReply(status gossiprpc.FetchStatus, values []string) []byte {
	b, err := codec.Marshal(gossiprpc.CustomFetchReply{Status: status, Values: values})
	if err != nil { // cannot happen for these types, but never return nil bytes
		return []byte(`{"status":"upstream_error","ms":0}`)
	}
	return b
}

// capValues enforces the server-side reply caps (≤5 values, ≤256 runes each)
// defensively at the boundary, rune-safe like ExternalVar's cap.
func capValues(values []string) []string {
	if len(values) > maxValues {
		values = values[:maxValues]
	}
	for i, v := range values {
		values[i] = truncateRunes(v, maxValueRunes)
	}
	return values
}

// truncateRunes cuts s to at most n runes without splitting one.
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
