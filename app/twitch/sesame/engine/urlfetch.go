// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"sync"

	"ItsBagelBot/app/twitch/sesame/module"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"go.uber.org/zap"
)

// urlFetchTokenPrefix namespaces one {urlfetch:name} fetch's redelivery claim,
// so a replayed command line cannot burn the broadcaster's fetch quota twice.
// The token grammar itself lives in scope.External; this is only the effect
// key, kept spelled the same way so a claim written before the scope split is
// still recognised after it.
const urlFetchTokenPrefix = "urlfetch:"

// maxUrlFetchTokens caps how many distinct {urlfetch:...} payloads one
// response may fan out; it is also the concurrency bound (one goroutine per
// payload). Save-time validation allows 3 tokens per response (the console
// contract), so 8 is purely the emit-side backstop against rows that predate
// or bypass validation — the same backstop role MaxResponseLines plays for
// line count. Uncapped was rejected: a corrupt or hostile template row could
// otherwise fan out unbounded concurrent RPCs from one chat line. Payloads
// beyond the cap stay verbatim, exactly like an unknown token.
const maxUrlFetchTokens = 8

// A failed fetch resolves to "" (ok=true), never authored English — never
// echoed from a reply body either, but also never the bot's own prose. It
// used to render "[source unavailable]" / "[source error]" / "[source timed
// out]": the one family in the palette that spoke in the bot's voice instead
// of the broadcaster's, because every other token in it already resolves
// failure to empty and lets {token|fallback} say whatever the broadcaster
// wrote. {urlfetch:x|down} now decides the prose the same way {followage|not
// yet} does. A replay of an already-spent claim renders the same empty value:
// the replay guarantee is that it neither re-fetches nor burns quota twice,
// not that it reproduces the original one. Only a missing or inactive
// definition (FetchBadDef) stays literal — that is an authoring mistake worth
// showing, not a fetch failure worth softening.

// fetchUrlValues resolves a response's {urlfetch:<name>} payloads: each
// distinct name — already folded and capped by scope.External — fans out one
// custom.fetch request to gossip, concurrently, and the rendered values come
// back keyed by that name. It is the external scope's Plan, so it runs AFTER
// the gate has claimed the command's cooldown — even a definition that fails
// every time cannot be hot-looped faster than its cooldown window.
//
// errgroup-style cancellation: the first failing token cancels the batch while
// every completed result still lands in the map — a sibling cancelled
// mid-flight renders the timeout-family text rather than blocking the reply.
// No cross-command single-flight is added on purpose: gossip's reply cache is
// the shared flight, and a second command arriving mid-fetch saves only
// microseconds here while costing a coordination map on every custom-command
// run.
//
// Redelivery safety mirrors claimedCounterValue: each fetch claims
// EffectRef{Identity: EventIdentity(&c.Env), Effect: "urlfetch:"+name} first,
// so a redelivered line skips the network call entirely and renders the
// fallback text — a replay must never burn the broadcaster's fetch quota
// twice. A fetch that produced no fresh value releases its claim so a
// quorum-loss redelivery retries it.
func (p *Pipeline) fetchUrlValues(ctx context.Context, c *module.Context, command string, names []string) map[string]string {
	// One segment per fan-out; the event/command/broadcaster identity rides as
	// attributes, never in the span name.
	seg := startStage(ctx, "sesame.urlfetch")
	if seg != nil {
		seg.AddAttribute("command", command)
		seg.AddAttribute("broadcaster_id", c.BroadcasterID)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sink := &urlTokenSink{results: make(map[string]string, len(names)), cancel: cancel}
	for _, name := range names {
		p.launchTokenFetch(ctx, c, name, sink)
	}
	sink.wg.Wait()

	endStage(seg, sink.verdict())
	return sink.results
}

// urlTokenSink collects one fan-out's outcomes: resolved values land in the
// results map, failures count toward the stage verdict and may release a
// dedup claim. A name absent from the map is the leave-verbatim outcome
// (bad_def): expandCommand then preserves the token. It also owns the
// fan-out's WaitGroup and cancel handle, so a launch site passes one
// collaborator instead of the same four loose values per token.
type urlTokenSink struct {
	mu       sync.Mutex
	results  map[string]string
	failures int

	wg     sync.WaitGroup
	cancel context.CancelFunc
}

// tokenOutcome is one name's finished resolution: text is the rendered value
// (successful fetch) or "" (any failure this family renders empty rather
// than literal). visible says whether it belongs in the results map at all —
// false is the one case (a missing/inactive definition) that must stay
// literal instead. release is an optional claim release on failure.
type tokenOutcome struct {
	name    string
	text    string
	visible bool
	failed  bool
	release func()
}

func (s *urlTokenSink) record(o tokenOutcome) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if o.failed {
		s.failures++
	}
	if o.visible {
		s.results[o.name] = o.text
	}
	if o.release != nil {
		o.release()
	}
}

func (s *urlTokenSink) verdict() string {
	if s.failures > 0 {
		return "fallback"
	}
	return "ok"
}

// launchTokenFetch starts one name's resolution: the redelivery claim runs on
// the caller's goroutine (claims must order with the command's other effects),
// and a fresh claim fans the network call out. The sink's cancel fires on
// first failure so sibling in-flight fetches stop early; recorded results stand.
func (p *Pipeline) launchTokenFetch(ctx context.Context, c *module.Context, name string, sink *urlTokenSink) {
	dup, release := p.claimedUrlValue(ctx, c, name)
	if dup {
		sink.record(tokenOutcome{name: name, visible: true, failed: true}) // replay: empty, no network
		return
	}
	sink.wg.Add(1)
	go func() {
		defer sink.wg.Done()
		text, visible, failed := p.resolveUrlToken(ctx, c, name)
		out := tokenOutcome{name: name, text: text, visible: visible, failed: failed}
		if failed {
			// Only a failed fetch releases its claim: a successful one keeps
			// it, so a redelivered line renders the same value again without
			// a second round trip.
			out.release = release
			defer sink.cancel()
		}
		sink.record(out)
	}()
}

// claimedUrlValue applies the redelivery guard to one {urlfetch:name} fetch,
// the exact shape claimedCounterValue gives counter bumps: a fresh claim lets
// the network call run, a replay reports dup so the caller renders fallback
// text without touching gossip, and the release handle lets a failed fetch
// retry on a later delivery. The kill switch (nil dedup) degrades to the
// plain unguarded fetch.
func (p *Pipeline) claimedUrlValue(ctx context.Context, c *module.Context, name string) (dup bool, release func()) {
	if p.dedup == nil {
		return false, func() {}
	}
	return p.dedup.Claim(ctx, EffectRef{Identity: EventIdentity(&c.Env), Effect: urlFetchTokenPrefix + name})
}

// resolveUrlToken performs one custom.fetch round trip and maps the typed
// reply onto (text, visible, failed):
//
//	FetchOK with an extractable value -> the value, visible, not failed
//	FetchBadDef (missing/inactive)    -> "", NOT visible — the unknown-token
//	                                     authoring signal, the one case this
//	                                     family still leaves literal
//	anything else (denied, limited,
//	  upstream_error, timeout,
//	  transport failure, ok-but-empty,
//	  unknown future status)          -> "", visible, failed — renders empty
//	                                     so {urlfetch:x|down} decides the
//	                                     prose (see the const block above)
//
// Whatever body gossip DID extract always passes ExternalVar before rendering,
// regardless of gossip's own 5x256 server-side cap — the variable-provider
// boundary does not trust upstream capping (the sanitizeVar slash-strip also
// stops a hostile upstream from minting a "/ban ..." line through
// emitCommand's per-line split). An ok reply with nothing extractable counts
// as upstream-shaped breakage, not a missing definition. Upstream bodies are
// never logged.
func (p *Pipeline) resolveUrlToken(ctx context.Context, c *module.Context, name string) (text string, visible, failed bool) {
	reply, err := p.customFetch.Fetch(ctx, gossiprpc.Request{
		DefID:     name,
		ChannelID: c.Env.BroadcasterUserID,
		IsPremium: c.Regress.IsPremium(),
	})
	if err != nil {
		p.log.Warn("urlfetch token failed",
			module.BIDField(c.BroadcasterID),
			zap.String("def", name),
			zap.Error(err),
		)
		return "", true, true
	}
	if reply.Status == gossiprpc.FetchBadDef {
		return "", false, true // leave the token visible, like every unknown token
	}
	if fetchYieldedValue(reply) {
		return ExternalVar(reply.Values[0]), true, false
	}
	return "", true, true
}

// fetchYieldedValue reports whether reply carries an extractable value from
// a successful fetch.
func fetchYieldedValue(reply gossiprpc.CustomFetchReply) bool {
	return reply.Status == gossiprpc.FetchOK && len(reply.Values) > 0 && reply.Values[0] != ""
}
