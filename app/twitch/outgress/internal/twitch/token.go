// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package twitch

import (
	"ItsBagelBot/pkg/codec"
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

const tokenEndpoint = "https://id.twitch.tv/oauth2/token"

// refreshMargin renews tokens this long before Twitch would reject them, so
// in-flight requests never race the expiry.
const refreshMargin = 5 * time.Minute

const (
	// tokenMaxIdleConnsPerHost: tokenHTTP only ever talks to id.twitch.tv, and
	// refreshes are a single request roughly every ~4h (refreshMargin before
	// Twitch's own ~4h token lifetime), never a burst. Two idle connections is
	// already generous for that; it is not sized like the Helix client's
	// 192-per-host pool (client.go, newHTTPClient) because that pool absorbs
	// concurrent request goroutines against api.twitch.tv, a completely
	// different traffic shape.
	tokenMaxIdleConnsPerHost = 2

	// tokenIdleConnTimeout must outlive the gap between refreshes, or the
	// pooled connection is dead before it can ever be reused. Measured
	// 2026-08-20 (20x TCP connect from a pod on node1): id.twitch.tv is 80.5ms
	// TCP RTT (api.twitch.tv, by contrast, is 11.5ms), and a cold TLS
	// handshake there costs ~320ms (TCP + TLS + request, ~3 round trips at
	// ~80ms). Before this fix, tokenHTTP ran on a bare http.DefaultTransport
	// clone whose IdleConnTimeout defaults to 90s -- far shorter than the ~4h
	// refresh cadence -- so the connection was ALWAYS closed by the time the
	// next refresh happened, and every single refresh paid the full cold-TLS
	// cost. refreshMargin (5m) trims the true cadence to ~3h55m; set this
	// comfortably past that so the background refresher (StartBackgroundRefresh
	// below) keeps reusing one warm connection indefinitely instead of paying
	// ~320ms every renewal.
	tokenIdleConnTimeout = 4*time.Hour + 30*time.Minute
)

// tokenHTTP is deliberately its own client, separate from the Helix client in
// client.go: different host, different traffic shape, different tuning.
var tokenHTTP = newTokenHTTPClient()

// tokenClientTimeout bounds one mint request end to end (TCP + TLS + the
// grant POST). Measured 2026-08-20, 15 consecutive cold requests against
// id.twitch.tv: min 300ms, p50 310ms, max 430ms (the 430ms sample was first,
// i.e. cold DNS). That run was a trivial GET returning 400 -- it exercises
// the same network path a grant exchange does, but NOT the server-side
// processing Twitch does for a real token grant, which was never measured
// and must not be assumed to be free.
//
// This used to be a bare 10s, picked with no stated reasoning -- about 23x
// the measured worst case. Two problems with that: it let a hung mint run
// far longer than any real request should need before failing, and (the
// unsafe part) it let one mint legally outlive mintLeaseTTL
// (app/twitch/outgress/token_lease.go) several times over, so the distributed lock
// meant to stop two replicas redeeming the same rotating refresh token could
// expire while the winner was still inside its own HTTP call. See
// MaxMintLeaseHold below for how this feeds that bound directly.
//
// 5s is chosen as a value comfortably above the measured 430ms (~11.6x) to
// leave real room for the unmeasured Twitch-side grant processing -- this is
// a judgement call about that headroom, not a benchmark of the grant itself
// -- while still being half the old blind guess and short enough that a
// genuinely hung request fails fast instead of sitting for 10s.
const tokenClientTimeout = 5 * time.Second

// tokenH2ReadIdleTimeout / tokenH2PingTimeout keep the pooled connection to
// id.twitch.tv genuinely alive, rather than merely believed-alive.
//
// tokenIdleConnTimeout above (4h30m) only tells OUR transport how long it may
// keep a connection pooled. The far end reaps on its own, much shorter,
// schedule, so without keepalive traffic a connection we hold for hours is a
// corpse: the next refresh is a POST, which net/http will NOT silently retry
// on a stale connection the way it retries idempotent requests, so the
// failure surfaces as a refresh error instead of a handshake. That makes a
// long idle timeout with no pings strictly worse than the stock 90s default
// it replaced, which at least discarded the connection before it went stale.
//
// The h2 PINGs fix that: they are real traffic on the wire, so the remote
// reaper never sees the flow as idle, and when the path dies anyway the
// missing PONG closes the connection within ReadIdle+Ping. Values and the
// net/http HTTP2Config rationale are taken wholesale from
// app/gossip/internal/core/http.go (see that file for the full derivation,
// including why PingTimeout must fit inside the request's own timeout
// budget). That property still holds here after tokenClientTimeout dropped
// to 5s: 3s leaves 2s of slack, comfortably above id.twitch.tv's measured
// 80.5ms TCP RTT and any retry it would take, though with less margin than
// gossip's own 10s-budget/3s-ping ratio -- worth stating rather than
// implying an identical margin.
const (
	tokenH2ReadIdleTimeout = 15 * time.Second
	tokenH2PingTimeout     = 3 * time.Second
)

func newTokenHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConnsPerHost = tokenMaxIdleConnsPerHost
	transport.IdleConnTimeout = tokenIdleConnTimeout
	transport.ForceAttemptHTTP2 = true
	transport.HTTP2 = &http.HTTP2Config{
		SendPingTimeout: tokenH2ReadIdleTimeout,
		PingTimeout:     tokenH2PingTimeout,
	}
	return &http.Client{Transport: transport, Timeout: tokenClientTimeout}
}

// MaxMintLeaseHold is the maximum time mintLeased (below) can hold a
// MintLease: one mint request, bounded by tokenClientTimeout, plus one
// Persist attempt, bounded by persistTimeout -- see mintLeased's doc for why
// only the FIRST persist attempt counts here, not the full persistAttempts
// retry budget. Exported so app/twitch/outgress/token_lease.go's mintLeaseTTL can be
// tested directly against it: the required invariant is
// mintLeaseTTL > MaxMintLeaseHold(), computed from these same constants, so a
// future change to any of them fails that test instead of silently
// reopening the multi-redemption race (see mintLeaseTTL's doc for the full
// story).
func MaxMintLeaseHold() time.Duration {
	return tokenClientTimeout + persistTimeout
}

// Source caches one OAuth access token and refreshes it before expiry or
// after Invalidate. All methods are safe for concurrent use.
type Source struct {
	mu      sync.RWMutex
	token   string
	expires time.Time
	refresh func(ctx context.Context) (string, time.Duration, error)
	group   singleflight.Group
	// gen is bumped by Invalidate so a refresh started after a 401 can
	// never join a singleflight call that began before it. See
	// singleflightRefresh.
	gen uint64

	// skipAdopt/invalidTok exist only for NewStoredUserTokenSource's refresh
	// closure (other constructors never adopt, so they never read these).
	// Invalidate sets both; consumeSkipAdopt reads-and-clears them so the
	// bypass applies to exactly the next refresh. See Invalidate's doc for
	// why a 401 must not be followed by re-adopting the very token that just
	// got rejected.
	skipAdopt  bool
	invalidTok string

	// currentRefresh is the latest known refresh token for constructors that
	// rotate one on every mint (NewUserTokenSource, NewStoredUserTokenSource).
	// Guarded by mu like the rest of this struct's mutable state: since
	// singleflightRefresh keys its group by generation (see gen/Invalidate),
	// two different generations' refresh closures -- one still in flight when
	// Invalidate starts the next -- can call into mintOnce concurrently, and
	// both read and, on a successful mint, write this value. Before the
	// generation keying existed, a bare local captured by the closure
	// sufficed because singleflight guaranteed exactly one refresh in flight
	// per Source at a time; it no longer does, so this needs the same real
	// synchronization as token/expires.
	currentRefresh string
}

// getCurrentRefresh reads the latest known refresh token.
func (s *Source) getCurrentRefresh() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentRefresh
}

// setCurrentRefresh records a refresh token Twitch just handed back on a
// successful mint. Unconditional -- unlike storeIfGen's cache publish, this
// is not about whether one generation's result should be trusted over
// another's; it is about keeping local bookkeeping in sync with what the
// Twitch OAuth server has already done for real. A rotation cannot be
// undone by discarding it locally, and whichever refresh runs next
// (whatever generation it belongs to) must present the token Twitch will
// actually still accept, not a value some other goroutine happened to
// capture first.
func (s *Source) setCurrentRefresh(v string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.currentRefresh = v
}

// ClientCredentials is the Twitch application's client id + secret, presented
// on every OAuth token grant (client-credentials and refresh-token alike).
type ClientCredentials struct {
	ID     string
	Secret string
}

// appGrant builds the client-credentials form (app access token).
func (c ClientCredentials) appGrant() url.Values {
	return url.Values{
		"client_id":     {c.ID},
		"client_secret": {c.Secret},
		"grant_type":    {"client_credentials"},
	}
}

// refreshGrant builds the refresh-token form (user access token) for one
// refresh token.
func (c ClientCredentials) refreshGrant(refreshToken string) url.Values {
	return url.Values{
		"client_id":     {c.ID},
		"client_secret": {c.Secret},
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	}
}

// NewAppTokenSource mints app access tokens through the client credentials
// grant. App tokens authorize most Helix endpoints the bot calls.
func NewAppTokenSource(creds ClientCredentials) *Source {

	return &Source{refresh: func(ctx context.Context) (string, time.Duration, error) {

		res, err := postToken(ctx, creds.appGrant())
		if err != nil {
			return "", 0, err
		}

		return res.AccessToken, time.Duration(res.ExpiresIn) * time.Second, nil
	}}
}

// NewStaticTokenSource returns a Source that always yields token without any
// network. It exists for tests and benchmarks that must run the full request
// path offline; production wiring never uses it.
func NewStaticTokenSource(token string) *Source {
	return &Source{refresh: func(context.Context) (string, time.Duration, error) {
		return token, 24 * time.Hour, nil
	}}
}

// NewUserTokenSource mints user access tokens for the bot account through
// the refresh token grant. Twitch may rotate the refresh token on every
// renewal, so the latest one is kept in memory (currentRefresh, guarded by
// mu -- see its doc for why this can't be a bare captured local).
func NewUserTokenSource(creds ClientCredentials, refreshToken string) *Source {

	s := &Source{currentRefresh: refreshToken}
	s.refresh = func(ctx context.Context) (string, time.Duration, error) {

		res, err := postToken(ctx, creds.refreshGrant(s.getCurrentRefresh()))
		if err != nil {
			return "", 0, err
		}

		if res.RefreshToken != "" {
			s.setCurrentRefresh(res.RefreshToken)
		}

		return res.AccessToken, time.Duration(res.ExpiresIn) * time.Second, nil
	}
	return s
}

// StoredLoad is what a StoredTokenIO.Load call returns: the refresh token to
// fall back on (loadedRefresh == "" means "keep whatever this Source already
// has"), plus -- when the store has one -- the stored access token and its
// absolute expiry.
//
// AccessTokenExpiresAt is nil whenever the store doesn't know the expiry:
// the row predates this field, it was written by a caller that doesn't track
// it (the admin token-set and dashboard OAuth-callback paths), or the reply
// came from a users-service build old enough not to send the field at all.
// NewStoredUserTokenSource's refresh closure treats nil as "not usable" and
// falls straight through to minting -- exactly today's behaviour -- so an
// old users service (or an old row) degrades safely with no version check.
type StoredLoad struct {
	RefreshToken         string
	AccessToken          string
	AccessTokenExpiresAt *time.Time
}

// StoredTokenIO wires a stored user token to the users service: load runs
// before every renewal (so a token the operator installs through the admin
// panel takes effect without a restart, and so a valid access token another
// replica already minted can be adopted -- see NewStoredUserTokenSource),
// and persist runs after every mint (so a restart never resurrects a stale
// refresh token).
type StoredTokenIO struct {
	Load func(ctx context.Context) StoredLoad
	// Persist writes back the token this refresh produced. expiresAt is
	// always a real value here (never the zero time): the refresh closure
	// only calls Persist right after a successful mint, where Twitch's own
	// expires_in is known.
	Persist func(ctx context.Context, accessToken, refreshToken string, expiresAt time.Time) error
}

// MintLease serializes real Twitch mints for one account (bot user id or
// broadcaster id) across outgress's 3 replicas, so two of them never redeem
// the SAME rotating refresh token at once -- see NewStoredUserTokenSource's
// adoption doc for why that is destructive, not just wasteful.
//
// Shaped as a single func field, the same pattern as StoredTokenIO, so this
// package stays free of a Valkey import: app/twitch/outgress/main.go builds the real
// implementation over its Valkey client and injects it; tests fake it
// directly. The zero value (Acquire == nil) means "no lease configured" --
// mint uncoordinated, exactly the behaviour before this fix existed. That is
// what NewUserTokenSource-style callers and any test that never touches
// Valkey get by default.
type MintLease struct {
	// Acquire claims the right to mint one refresh cycle for this Source's
	// account.
	//
	// ok is false in two DIFFERENT situations that callers must not treat
	// alike:
	//   - contended (unavailable == false): another replica already holds
	//     the lease. There IS a winner out there; the caller should wait
	//     briefly and try to adopt what it persists, or acquire the lease
	//     itself if that winner turns out to be gone (see
	//     waitForLeaseOrAdoption).
	//   - unavailable (unavailable == true): the lease backend itself
	//     could not be reached (e.g. Valkey down or timed out), not that
	//     someone else holds the lock. Nobody can be coordinating through a
	//     backend nobody can read, so there is no winner to wait for --
	//     waiting out the full poll budget here is guaranteed wasted time,
	//     arriving at the same uncoordinated mint anyway but slower. See
	//     mintOrAdopt for how it skips straight to that mint instead.
	//
	// release must be called exactly once regardless of how the mint
	// attempt ends; see mintLeased for why it is not simply deferred right
	// after the mint call returns.
	Acquire func(ctx context.Context) (release func(), ok bool, unavailable bool)
}

// NewStoredUserTokenSource works like NewUserTokenSource but sources the
// refresh token from the users service instead of the environment, and --
// this is the point of this Source over NewUserTokenSource -- adopts a
// stored access token instead of minting whenever one is already valid.
//
// WHY ADOPTION EXISTS: outgress runs 3 replicas, each holding its own
// in-memory token cache (twitch.BroadcasterTokens is an LRU of *Source per
// replica). A go-live fans a token pre-warm out to all 3 (see
// app/twitch/outgress/internal/worker/tokenwarm.go), and before this fix every one
// of them independently minted: each replica POSTed the SAME stored refresh
// token to id.twitch.tv within milliseconds of the other two. Twitch ROTATES
// the refresh token on redemption, so at most one of those 3 POSTs wins --
// the losers persist (or keep in memory) a refresh token Twitch has already
// invalidated, which is this repo's known "broadcaster grant dies silently"
// failure mode, not a theoretical one. Adoption sidesteps the race
// completely: when a valid access token is already sitting in the store,
// nothing gets redeemed, so nothing can collide.
//
// It is also far cheaper on the common path. A cold mint costs one NATS RPC
// (Load) plus a full round trip to id.twitch.tv: measured 2026-08-20,
// id.twitch.tv is 80.5ms TCP RTT from a pod and the whole TLS-handshake-plus-
// request cycle is ~320ms (roughly 3 round trips). Adoption replaces that
// with one NATS RPC plus one MySQL read on the users service, ~8ms --
// no network hop to Twitch at all.
//
// fallbackRefresh seeds the chain while the store is empty. lease, when
// non-nil-Acquire, serializes real mints across replicas -- see MintLease.
func NewStoredUserTokenSource(creds ClientCredentials, fallbackRefresh string, io StoredTokenIO, lease MintLease) *Source {

	// s is declared before its refresh closure is assigned (rather than in
	// one composite literal, as every other constructor in this file does)
	// so the closure can call back into s.consumeSkipAdopt -- see
	// Invalidate's doc for why that call has to happen. currentRefresh
	// (guarded by s.mu -- see its doc) replaces what used to be a bare
	// captured local: mintOrAdopt's mint path can now run concurrently
	// across two generations (see gen/Invalidate), so a plain local was no
	// longer safe to read and write unsynchronized.
	s := &Source{currentRefresh: fallbackRefresh}

	// m bundles the four collaborators (creds, s, io, lease) that are fixed
	// for the lifetime of this Source and were previously threaded as
	// separate parameters through every function in the mint/lease/persist
	// call chain below (mintOrAdopt, waitForLeaseOrAdoption,
	// pollLeaseOrAdoption, mintLeased, persistAsyncThen, ...). Built once
	// here and reused as a value receiver -- it is four small, already-
	// cheap-to-copy fields (a struct of two strings, a pointer, and two
	// func-field structs), so passing it by value on every call costs
	// nothing that mattered before as separate arguments. Only ctx and
	// whatever varies per call (forbid, release) still travel as explicit
	// parameters.
	m := minter{creds: creds, s: s, io: io, lease: lease}

	s.refresh = func(ctx context.Context) (token string, ttl time.Duration, err error) {

		stored := io.Load(ctx)
		if stored.RefreshToken != "" {
			s.setCurrentRefresh(stored.RefreshToken)
		}

		// forbid is "" (never matches a real stored token, which is never
		// empty) on every ordinary refresh. It is only ever the invalidated
		// token's value, and only for the one refresh right after a 401 --
		// see consumeSkipAdopt.
		forbid := ""
		skip, badToken := s.consumeSkipAdopt()
		if skip {
			forbid = badToken
		}

		// consumeSkipAdopt clears the flag, so it must be re-armed if this
		// refresh does not actually succeed in replacing the rejected token.
		// Without this, a single failed mint after a 401 (id.twitch.tv 5xx, a
		// network blip, or the losing-replica fallback erroring) drops the
		// guard, and the NEXT refresh adopts the very token the 401 rejected
		// -- reinstating the bug consumeSkipAdopt exists to prevent, one
		// attempt later. The flag has to survive until a refresh genuinely
		// succeeds, not merely until one is attempted.
		defer func() {
			if err != nil && skip {
				s.rearmSkipAdopt(badToken)
			}
		}()

		if token, ttl, ok := adoptCandidate(stored, forbid); ok {
			return token, ttl, nil
		}

		if s.getCurrentRefresh() == "" {
			return "", 0, ErrNoRefreshToken
		}

		return m.mintOrAdopt(ctx, forbid)
	}
	return s
}

// minter bundles the collaborators that mintOrAdopt and everything it calls
// need for the lifetime of one Source: the Twitch client credentials, the
// Source itself (for currentRefresh and consumeSkipAdopt), the users-service
// IO, and the cross-replica mint lease. Built once by
// NewStoredUserTokenSource's refresh closure -- see its doc -- so every
// method below takes only what actually varies per call (ctx, and forbid or
// release), instead of re-threading creds/s/io/lease through each one
// individually.
type minter struct {
	creds ClientCredentials
	s     *Source
	io    StoredTokenIO
	lease MintLease
}

// rearmSkipAdopt restores the post-401 guard after a failed refresh. It never
// clobbers a newer Invalidate: if one landed while this refresh was running,
// its invalidTok is the more recent rejection and must win.
func (s *Source) rearmSkipAdopt(badToken string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.skipAdopt {
		return
	}
	s.skipAdopt = true
	s.invalidTok = badToken
}

// adoptCandidate wraps adoptableStored with the post-401 guard: forbid is
// either "" (no restriction) or the exact access token a 401 just rejected,
// in which case a store that still reports that same value is treated as
// not adoptable even though it looks unexpired -- see Invalidate's doc.
func adoptCandidate(stored StoredLoad, forbid string) (string, time.Duration, bool) {
	token, ttl, ok := adoptableStored(stored)
	if !ok || token == forbid {
		return "", 0, false
	}
	return token, ttl, true
}

const (
	// leaseWaitInterval/leaseWaitAttempts bound how long a losing replica
	// polls before giving up and minting uncoordinated (mintOrAdopt's last
	// resort). On every poll it tries BOTH to adopt the winner's persisted
	// token and to acquire the lease itself (see waitForLeaseOrAdoption) --
	// so this budget only gets fully spent when neither happens: the winner
	// is still working and hasn't finished persisting.
	//
	// Sizing it: the winner's realistic completion is a mint (p50 310ms,
	// max 430ms measured 2026-08-20 for the network path alone -- see
	// tokenClientTimeout's doc; real Twitch-side grant processing on top of
	// that was NOT measured) plus one Persist attempt (~16-20ms measured,
	// see persistAttempts' doc). Exceeding the OLD budget here (5 attempts *
	// 150ms = 750ms) was routine, not exceptional, once the unmeasured grant
	// processing is accounted for -- that was this file's second safety bug:
	// a loser fell through to an uncoordinated mint under exactly the
	// concurrent-refresh load the lease exists to serialize.
	// 10 attempts spaced 200ms apart cover a 2s budget: several times the
	// measured 430ms network max, with real room left over for the
	// unmeasured grant-processing overhead. There is no measurement behind
	// the exact attempt count/interval split (only the total budget is
	// grounded in the numbers above); it is a judgement call about how to
	// spend that budget, not a tuned value.
	//
	// Worst-case added latency this can put on a losing replica's chat send:
	// the full 2s wait, then -- only if the lease is still held by someone
	// and nothing adoptable ever appeared -- an uncoordinated mint bounded
	// by tokenClientTimeout (5s). That is a real ~7s worst case, but it only
	// happens when the winner is stuck for multiple seconds AND never
	// releases the lease; the common "winner crashed" case is caught much
	// sooner because every poll also tries Acquire (see
	// waitForLeaseOrAdoption).
	leaseWaitInterval = 200 * time.Millisecond
	leaseWaitAttempts = 10
)

// mintOnce performs one real Twitch mint and rotates s's currentRefresh when
// Twitch hands back a new refresh token. Split out of the two mint paths
// below (uncoordinated and leased) so neither duplicates the rotation logic.
//
// Reads/writes through m.s's own synchronized accessors rather than a bare
// pointer dereference: two different generations' refresh closures can call
// this concurrently (see currentRefresh's doc on Source), so the
// read-then-conditionally-write has to go through s.getCurrentRefresh /
// s.setCurrentRefresh, not a captured local.
func (m minter) mintOnce(ctx context.Context) (oauthResponse, time.Duration, error) {
	res, err := postToken(ctx, m.creds.refreshGrant(m.s.getCurrentRefresh()))
	if err != nil {
		return oauthResponse{}, 0, err
	}
	if res.RefreshToken != "" {
		m.s.setCurrentRefresh(res.RefreshToken)
	}
	return res, time.Duration(res.ExpiresIn) * time.Second, nil
}

// mintAndPersistAsync is the uncoordinated mint path: no lease configured,
// or a losing replica that gave up waiting for the winner (see mintOrAdopt).
// Persist runs detached exactly as it always has -- see persistAsync's doc
// for the ~16-20ms this saves on the caller's path.
func (m minter) mintAndPersistAsync(ctx context.Context) (string, time.Duration, error) {
	res, ttl, err := m.mintOnce(ctx)
	if err != nil {
		return "", 0, err
	}
	// Read the refresh token back through m.s.getCurrentRefresh() (not a
	// captured local) so this detached write persists whatever mintOnce
	// just rotated it to, even if a concurrent generation's refresh mutates
	// currentRefresh again immediately after this one returns.
	m.persistAsync(res.AccessToken, m.s.getCurrentRefresh(), time.Now().Add(ttl))
	return res.AccessToken, ttl, nil
}

// mintOrAdopt runs the real mint, coordinated through lease when one is
// configured. See MintLease for why coordination matters at all: outgress
// runs 3 replicas and Twitch rotates the refresh token on redemption, so two
// replicas minting the SAME stored refresh token at once is destructive.
//
// forbid carries the post-401 guard through to the loser's wait-and-adopt
// retries too (waitForAdoption), not just the one check already done in the
// caller: within a single skip-round, no re-Load anywhere in this call may
// hand back the token that was just invalidated.
func (m minter) mintOrAdopt(ctx context.Context, forbid string) (string, time.Duration, error) {
	if m.lease.Acquire == nil {
		return m.mintAndPersistAsync(ctx)
	}

	release, ok, unavailable := m.lease.Acquire(ctx)
	if ok {
		return m.mintLeased(ctx, release)
	}
	if unavailable {
		// The lease BACKEND is unreachable (Valkey down/timed out), not
		// merely held by someone else -- see MintLease.Acquire's doc. No
		// replica can be coordinating through a backend nobody can read, so
		// waiting here (adoption poll or a lease that will never free up
		// because nobody could ever claim it) is guaranteed to burn the
		// full leaseWaitAttempts*leaseWaitInterval budget and land on this
		// exact same uncoordinated mint anyway, just ~2s slower. Skip
		// straight to it. The trade-off is the same one mintAndPersistAsync
		// always accepted uncoordinated: a rare double redemption here is
		// recoverable via adoption once the backend (and thus coordination)
		// is back, which beats adding latency to every mint for the
		// duration of an outage.
		return m.mintAndPersistAsync(ctx)
	}

	if token, ttl, done, err := m.waitForLeaseOrAdoption(ctx, forbid); done {
		return token, ttl, err
	}
	// Fallback: nobody ever showed an adoptable token, and the lease was
	// never free, within the wait budget above (the winner is stuck behind
	// something that outlasts leaseWaitAttempts*leaseWaitInterval). Deliberate
	// trade-off, not an oversight: mint anyway, uncoordinated. A rare double
	// redemption here is recoverable on the NEXT cycle via ordinary adoption;
	// a grant stuck forever because its only synchronization point vanished
	// is not. This path is meant to be rare: any winner that crashes or lets
	// mintLeaseTTL expire frees the lease immediately, and
	// waitForLeaseOrAdoption polling for that (not just for adoption) is
	// what catches it well before the budget runs out.
	return m.mintAndPersistAsync(ctx)
}

// mintLeased runs the real mint while holding lease, then hands the Persist
// call (and the lease release) to a background goroutine so the winning
// caller's Token() sees the same latency an uncoordinated mint always had --
// nothing here makes the caller wait on a MySQL write.
//
// The lease is released once the FIRST Persist attempt resolves (success or
// failure) -- not after the full persistAttempts retry loop, which keeps
// running in the background regardless. This is a deliberate change from
// holding across every retry: the lease's whole job is to bound how long a
// mint may run before another replica is allowed to try (see mintLeaseTTL,
// app/twitch/outgress/token_lease.go), and holding through persistAttempts retries
// made the real worst-case hold roughly persistAttempts times the persist
// budget alone, on top of the mint itself -- comfortably exceeding
// mintLeaseTTL and reopening the exact multi-redemption race the lease
// exists to prevent (see MaxMintLeaseHold, which now counts only the first
// attempt).
//
// Releasing after one attempt does trade away a slice of protection: on that
// attempt's rare failure (the store's first write blips), a follower can
// acquire the now-free lease before the retry lands and mint again with the
// same rotated-away refresh token in memory. That follower's mint fails at
// Twitch (the token is already spent) rather than corrupting anything --
// the same rare, recoverable trade-off mintOrAdopt's fallback already
// documents, not a new class of failure. The common case (first Persist
// attempt succeeds, ~16-20ms) is unaffected: the lease is released just as
// promptly as before.
func (m minter) mintLeased(ctx context.Context, release func()) (string, time.Duration, error) {
	res, ttl, err := m.mintOnce(ctx)
	if err != nil {
		release()
		return "", 0, err
	}
	m.persistAsyncThen(res.AccessToken, m.s.getCurrentRefresh(), time.Now().Add(ttl), release)
	return res.AccessToken, ttl, nil
}

// waitForLeaseOrAdoption polls, spaced leaseWaitInterval apart up to
// leaseWaitAttempts times, for either outcome that means this replica does
// not need to fall through to an uncoordinated mint:
//
//  1. the winner's persisted token becomes visible (adoption), or
//  2. the lease itself becomes acquirable, meaning the original winner is
//     gone -- crashed mid-mint, or simply let mintLeaseTTL expire -- in
//     which case THIS replica becomes the new winner and mints through the
//     same mintLeased path a first-attempt winner would, releasing the
//     lease itself when done.
//
// Acquire is tried first on every poll (including the first, with no wait)
// because a freed lease is the strongest possible signal: nothing else needs
// checking once this replica holds it. See mintOrAdopt for why falling all
// the way through to an uncoordinated mint stays a genuine last resort
// rather than being removed -- this is what makes that last resort rare in
// practice instead of routine.
//
// forbid excludes a token this Source already knows is dead (see
// mintOrAdopt).
//
// Return shape: done=true means the caller should return (token, ttl, err)
// immediately, whether that is a successful adoption (err == nil) or the
// result of a mint this replica just ran after acquiring the lease (err may
// be non-nil). done=false means the budget was exhausted with the lease
// still held by someone else and nothing adoptable -- the caller's
// uncoordinated fallback applies.
func (m minter) waitForLeaseOrAdoption(ctx context.Context, forbid string) (token string, ttl time.Duration, done bool, err error) {
	for attempt := 0; attempt < leaseWaitAttempts; attempt++ {
		if attempt > 0 && !waitTick(ctx) {
			return "", 0, false, nil
		}
		if token, ttl, done, stop, err := m.pollLeaseOrAdoption(ctx, forbid); done || stop {
			return token, ttl, done, err
		}
	}
	return "", 0, false, nil
}

// waitTick sleeps leaseWaitInterval, or returns false the moment ctx is
// cancelled -- so a cancelled caller stops polling immediately instead of
// sleeping out a budget nothing will use.
func waitTick(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(leaseWaitInterval):
		return true
	}
}

// pollLeaseOrAdoption runs ONE poll attempt: try to acquire the lease (see
// MintLease.Acquire's ok/unavailable split) and mint through it if so,
// otherwise try to adopt whatever the store currently shows.
//
// done means the caller should return (token, ttl, err) right now -- either
// a successful adoption (err == nil) or the result of the mint this replica
// just ran after acquiring the lease (err may be non-nil). stop means give
// up polling immediately even though done is false: the lease backend
// itself went unavailable mid-wait (see mintOrAdopt's unavailable branch),
// so spending the rest of the budget on it cannot help -- the caller's
// uncoordinated fallback applies right away, same as if the budget had run
// out normally.
func (m minter) pollLeaseOrAdoption(ctx context.Context, forbid string) (token string, ttl time.Duration, done bool, stop bool, err error) {
	release, ok, unavailable := m.lease.Acquire(ctx)
	if ok {
		token, ttl, err := m.mintLeased(ctx, release)
		return token, ttl, true, false, err
	}
	if unavailable {
		return "", 0, false, true, nil
	}
	if token, ttl, ok := adoptCandidate(m.io.Load(ctx), forbid); ok {
		return token, ttl, true, false, nil
	}
	return "", 0, false, false, nil
}

// adoptableStored reports whether stored carries an access token this
// process can serve directly instead of minting one, and if so its
// remaining TTL.
//
// refreshMargin (5m, declared above for the Token/cached freshness check) is
// reused here rather than a second constant: the property being guarded is
// identical in both places -- never hand out a token that is about to stop
// working -- whether the token came from this process's own cache or from
// the store, so one safety margin covers both. A nil or missing expiry is
// never treated as "valid forever"; it falls straight through to a mint,
// same as an empty AccessToken.
func adoptableStored(stored StoredLoad) (string, time.Duration, bool) {
	if stored.AccessToken == "" || stored.AccessTokenExpiresAt == nil {
		return "", 0, false
	}
	ttl := time.Until(*stored.AccessTokenExpiresAt)
	if ttl <= refreshMargin {
		return "", 0, false
	}
	return stored.AccessToken, ttl, true
}

const (
	// persistTimeout bounds one detached Persist call. It is generous relative
	// to the ~16-20ms this write normally takes (see above) because the whole
	// point of detaching it is that nothing is waiting on it; the bound exists
	// only to stop a stuck call from running forever, not to keep it fast.
	//
	// It also now doubles as half of MaxMintLeaseHold: mintLeased releases
	// its MintLease after the first Persist attempt resolves, so this bounds
	// that attempt's worst-case contribution to how long a mint may hold the
	// lease. Raising this value raises MaxMintLeaseHold and therefore the
	// floor mintLeaseTTL (app/twitch/outgress/token_lease.go) must clear -- the test
	// asserting that invariant will catch a mismatch.
	persistTimeout = 5 * time.Second

	// persistAttempts/persistRetryBackoff bound the detached write. Persist
	// returns an error so the retry is failure-conditional: a successful write
	// returns immediately and costs exactly ONE transaction. That matters --
	// the users-service persist is BEGIN/SELECT/UPDATE/COMMIT, four round trips
	// at 3.6 ms each plus a COMMIT averaging 1.89 ms server-side, so retrying
	// unconditionally would triple the token write load for no benefit.
	// Three attempts at 200 ms covers a NATS redelivery blip without holding a
	// goroutine long enough to matter; losing a rotated refresh token is
	// permanent (the grant dies and the broadcaster must re-authorise), which
	// is why this retries at all rather than firing once and hoping.
	persistAttempts     = 3
	persistRetryBackoff = 200 * time.Millisecond
)

// persistAsync runs persist off the caller's path and off the caller's
// context: ctx is cancelled the moment the send/refresh that triggered this
// completes, which would abort the store write mid-flight. A background
// context with its own persistTimeout keeps the write alive independently of
// that lifetime. See persistAttempts for why the retry is unconditional.
func (m minter) persistAsync(accessToken, refreshToken string, expiresAt time.Time) {
	m.persistAsyncThen(accessToken, refreshToken, expiresAt, nil)
}

// persistAsyncThen is persistAsync plus an optional onDone callback, run once
// the FIRST persist attempt resolves (success or failure) -- not after the
// whole retry loop, which keeps running underneath regardless of onDone.
// mintLeased uses this to release its MintLease as soon as the token is
// visible in the store in the common case, or at worst after one
// persistTimeout, rather than holding it through every one of persistAttempts
// retries -- see mintLeased's doc for the safety reasoning. persistAsync
// itself passes onDone as nil, so its behaviour (retry fully, nothing to
// call) is unchanged.
func (m minter) persistAsyncThen(accessToken, refreshToken string, expiresAt time.Time, onDone func()) {
	go func() {
		for attempt := 0; attempt < persistAttempts; attempt++ {
			if attempt > 0 {
				time.Sleep(persistRetryBackoff)
			}
			ok := persistOnce(m.io.Persist, accessToken, refreshToken, expiresAt)
			if attempt == 0 && onDone != nil {
				onDone()
			}
			if ok {
				return
			}
		}
	}()
}

// persistOnce runs a single detached Persist call and reports success. Split
// out so persistAsync stays a flat retry loop rather than nesting a
// context/defer/error branch inside it.
func persistOnce(persist func(ctx context.Context, accessToken, refreshToken string, expiresAt time.Time) error, accessToken, refreshToken string, expiresAt time.Time) bool {
	ctx, cancel := context.WithTimeout(context.Background(), persistTimeout)
	defer cancel()
	return persist(ctx, accessToken, refreshToken, expiresAt) == nil
}

// Token returns a valid access token, renewing it when missing or close to
// expiry. When renewal fails but the cached token is still within its
// lifetime, the cached token is returned so a transient id.twitch.tv outage
// does not take the egress path down with it.
func (s *Source) Token(ctx context.Context) (string, error) {
	if token, ok := s.cached(refreshMargin); ok {
		return token, nil
	}
	return s.singleflightRefresh(ctx)
}

// maxRefreshGenRetries bounds how many times singleflightRefresh retries
// after discovering its result went stale mid-flight -- see storeIfGen and
// singleflightRefreshOnce's doc for the race this guards against. One retry
// is enough for the realistic case (a single concurrent Invalidate); bounding
// it stops a pathological run of rapid concurrent Invalidates from spinning
// this call forever.
const maxRefreshGenRetries = 1

// singleflightRefresh runs s.refresh under the shared singleflight group, so
// this Source only ever has one refresh in flight regardless of how many
// callers (foreground Token calls, the background refresher below) triggered
// it concurrently. Factored out of Token so StartBackgroundRefresh can drive
// the exact same path -- and therefore the exact same one-refresh guarantee
// -- instead of risking a second, racing implementation.
//
// Wrapped in a small bounded retry: singleflightRefreshOnce can report its
// result as stale (Invalidate ran concurrently and bumped s.gen before this
// refresh's result could be published), in which case it must not be
// treated as success -- see storeIfGen's doc for the regression this closes.
func (s *Source) singleflightRefresh(ctx context.Context) (string, error) {
	for attempt := 0; ; attempt++ {
		token, stale, err := s.singleflightRefreshOnce(ctx)
		if !stale || attempt >= maxRefreshGenRetries {
			return token, err
		}
		// stale: retry once under the new generation, which re-consumes the
		// (now armed) skip flag and carries the correct forbid value -- see
		// consumeSkipAdopt.
	}
}

// singleflightRefreshOnce runs one refresh attempt under the singleflight
// group keyed by the generation captured at entry.
//
// REGRESSION THIS GUARDS AGAINST: Invalidate's gen bump (see its doc) stops
// a NEW caller from joining an OLD singleflight call, but on its own it does
// nothing to stop an ALREADY IN-FLIGHT old-generation call from overwriting
// s.token with a stale result once it finally returns. Sequence: caller A is
// blocked inside s.refresh (e.g. the NATS Load RPC) using generation 0, and
// has already read consumeSkipAdopt as false (nothing to skip yet). Caller B
// gets a 401 and calls Invalidate: token cleared, skipAdopt armed, gen
// becomes 1. A's Load then returns, still showing the now-dead token as
// unexpired -- A never re-checked the skip flag, so it adopts and would
// publish that exact token right over B's invalidation. storeIfGen is what
// stops this: the publish only lands if s.gen is still what it was when this
// call started; otherwise the result is known-stale and singleflightRefresh
// above retries under the new generation instead of trusting it.
func (s *Source) singleflightRefreshOnce(ctx context.Context) (token string, stale bool, err error) {
	s.mu.RLock()
	gen := s.gen
	s.mu.RUnlock()
	key := "refresh-" + strconv.FormatUint(gen, 10)

	value, err, _ := s.group.Do(key, func() (any, error) {
		// Another caller may have completed the refresh while this caller waited.
		if token, ok := s.cached(refreshMargin); ok {
			return refreshResult{token: token}, nil
		}

		// refresh performs NATS RPC and HTTP I/O. It intentionally runs outside
		// mu so status calls and invalidation never queue behind a slow network
		// operation; singleflight still guarantees one refresh per Source.
		token, ttl, err := s.refresh(ctx)
		if err != nil {
			if cached, ok := s.cached(0); ok {
				return refreshResult{token: cached}, nil
			}
			return nil, err
		}

		if !s.storeIfGen(gen, token, ttl) {
			return refreshResult{stale: true}, nil
		}
		return refreshResult{token: token}, nil
	})
	if err != nil {
		return "", false, err
	}
	r := value.(refreshResult)
	return r.token, r.stale, nil
}

// refreshResult is singleflightRefreshOnce's group.Do payload: token is only
// meaningful when stale is false.
type refreshResult struct {
	token string
	stale bool
}

// storeIfGen publishes token/ttl into the cache only if s.gen still equals
// gen, the generation captured before this refresh began, and reports
// whether it stored. If Invalidate ran concurrently and bumped gen, the
// value this refresh produced is stale (see singleflightRefreshOnce's doc)
// and must not overwrite whatever Invalidate -- or a newer refresh -- has
// already set.
func (s *Source) storeIfGen(gen uint64, token string, ttl time.Duration) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gen != gen {
		return false
	}
	s.token = token
	s.expires = time.Now().Add(ttl)
	return true
}

// backgroundRefreshInterval is how often StartBackgroundRefresh checks
// whether the cached token is due for renewal.
//
// Safety factor: refreshMargin (5m) is the window this must catch before
// expiry, and a 1m tick gives a 5x safety factor -- up to 4 ticks can be
// delayed or skipped entirely and the 5th still lands inside the margin. If
// every tick were somehow skipped for longer than that (the process
// stalled, ctx never fired), there is still a backstop: Token's own lazy
// path (singleflightRefresh via cached) renews on the very next real call
// once the cached token crosses refreshMargin, exactly as it did before this
// background refresher existed -- the only cost of a fully missed sweep is
// paying the mint latency on that one chat send instead of off-path.
//
// The check itself is a local, lock-protected read (cached), and the actual
// network refresh it may trigger still only happens once per ~4h, so a 1m
// tick adds no meaningful load regardless of the margin above.
const backgroundRefreshInterval = time.Minute

// StartBackgroundRefresh renews the cached token proactively, off the chat
// send path, until ctx is cancelled. Without this, Token renews lazily on
// whichever caller happens to observe expiry first: one random chat message
// mid-stream then eats the full ~320ms cold-refresh cost (measured: first
// chat message end-to-end ~360-390ms vs ~15-25ms for the rest, almost
// entirely this). It is safe to call on a Source that has never been used
// (cached reports "not ok" on a zero-value token, so the first tick just
// refreshes like any other), and it never panics on a failed refresh -- this
// package has no logger, so a failure here is swallowed and simply retried
// on the next tick, same as any other transient id.twitch.tv outage.
//
// It renews through singleflightRefresh, the same path and the same group
// Token uses, so a tick can never race a concurrent foreground Token call
// into two refreshes or a torn token/expires update.
func (s *Source) StartBackgroundRefresh(ctx context.Context) {
	go s.runBackgroundRefresh(ctx)
}

func (s *Source) runBackgroundRefresh(ctx context.Context) {
	ticker := time.NewTicker(backgroundRefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.refreshIfDue(ctx)
		}
	}
}

// refreshIfDue triggers a refresh only when the cached token is within
// refreshMargin of expiry, and discards the result: this path exists purely
// to warm the cache and the pooled connection (see tokenIdleConnTimeout)
// before a real caller needs either. Errors are intentionally not surfaced;
// see StartBackgroundRefresh.
func (s *Source) refreshIfDue(ctx context.Context) {
	if _, ok := s.cached(refreshMargin); ok {
		return
	}
	_, _ = s.singleflightRefresh(ctx)
}

func (s *Source) cached(margin time.Duration) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.token, s.token != "" && time.Until(s.expires) > margin
}

// Invalidate discards the cached token so the next Token call renews it.
// Called after a 401, which means Twitch revoked the token early -- the
// token's nominal expiry is meaningless once Twitch has already rejected it.
//
// REGRESSION THIS GUARDS AGAINST: NewStoredUserTokenSource's refresh closure
// adopts whatever access token the users service has on file, as long as its
// stored expiry is still comfortably in the future (see adoptableStored).
// Clearing s.token alone does not stop that: the very next refresh would
// call io.Load, see the SAME still-not-expired row, and hand the just-
// rejected token straight back out -- client.go's inline retry-once-on-401
// would then fail a second time, and every send after that would keep
// re-adopting the same dead token for up to ~4h (its nominal lifetime)
// instead of ever minting a real replacement. Recording invalidTok here and
// consuming it in the very next refresh (consumeSkipAdopt) closes that gap:
// that one refresh is forced to treat the recorded token as unusable no
// matter what io.Load returns, so it either adopts a genuinely different
// (fresher) token another replica already produced, or falls through to a
// real mint. See adoptCandidate and mintOrAdopt's forbid parameter.
func (s *Source) Invalidate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.invalidTok = s.token
	s.skipAdopt = true
	s.token = ""
	// Bumping gen re-keys the singleflight group. Without it, a retry issued
	// after a 401 can JOIN a refresh that started before the invalidation and
	// receive its result -- which is the token that was just rejected, handed
	// back without the forbid check ever running. That costs another
	// guaranteed 401 before the guard finally applies. Re-keying makes the
	// post-401 refresh a strictly separate call, while concurrent callers that
	// all observed the same rejection still share one refresh.
	s.gen++
}

// consumeSkipAdopt reports whether the very next refresh must bypass
// adoption of invalidTok, and clears both fields so only that one refresh is
// affected -- a single transient 401 must not permanently disable adoption
// on this Source. Holding s.mu for the read-and-clear (rather than two
// separate locked steps) is what makes this safe against Invalidate racing a
// concurrent singleflightRefresh: whichever happens first under the lock is
// the value the next refresh sees, and there is no window where a flag set
// by Invalidate can be read by one goroutine and then re-set as if unread by
// another.
func (s *Source) consumeSkipAdopt() (skip bool, badToken string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	skip, badToken = s.skipAdopt, s.invalidTok
	s.skipAdopt = false
	s.invalidTok = ""
	return skip, badToken
}

// ExpiresIn reports the remaining lifetime of the cached token, zero when
// none is held. Exposed through the system status RPC.
func (s *Source) ExpiresIn() time.Duration {

	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.token == "" {
		return 0
	}

	remaining := time.Until(s.expires)
	if remaining < 0 {
		return 0
	}

	return remaining
}

type oauthResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

func postToken(ctx context.Context, form url.Values) (oauthResponse, error) {

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return oauthResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	res, err := tokenHTTP.Do(req)
	if err != nil {
		return oauthResponse{}, err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return oauthResponse{}, &TokenError{Status: res.StatusCode, Body: string(body)}
	}

	var parsed oauthResponse
	if err := codec.NewDecoder(res.Body).Decode(&parsed); err != nil {
		return oauthResponse{}, err
	}

	return parsed, nil
}
