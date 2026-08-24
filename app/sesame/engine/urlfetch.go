// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"slices"
	"strings"
	"sync"

	"ItsBagelBot/app/sesame/module"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"go.uber.org/zap"
)

// urlFetchTokenPrefix marks the external-fetch substitution inside a response
// template: {urlfetch:name} renders the value gossip's custom.fetch endpoint
// extracted for the broadcaster-authored definition "name" ({urlfetch:name.a.b}
// selects a dotted path into the fetched document). Nothing is mutated — the
// token is a pure read — but it rides the counters' pre-expansion shape because
// module.Expand's repl callback is synchronous and ctx-free: the network call
// must happen before expansion, never inside it.
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

// The static fallback texts a failed fetch renders instead of any upstream
// content — authored here, never echoed from a reply body. A replay of an
// already-spent claim reuses the unavailable text: the replay guarantee is
// that it neither re-fetches nor burns quota twice, not that it reproduces
// the original value.
const (
	urlFetchUnavailableText = "[source unavailable]" // denied / limited / replay
	urlFetchErrorText       = "[source error]"       // upstream_error, empty ok
	urlFetchTimeoutText     = "[source timed out]"   // timeout / transport failure
)

// urlFetchNames scans a response template for {urlfetch:<name>} tokens and
// returns the distinct normalized payloads, in first-appearance order — the
// byte-for-byte mirror of counterTokenNames: the same strings.Contains fast
// path, the same Index/IndexByte zero-alloc scan over the brace grammar
// module.Expand re-parses later, and the same NormalizeCounterName fold (so
// "{URLFETCH:Temp}" scans as "temp" AND expands by looking up "temp"). nil
// when the template references none — the fast path for every ordinary command.
func urlFetchNames(tmpl string) []string {
	var names []string
	rest := tmpl
	for {
		i := strings.Index(rest, "{"+urlFetchTokenPrefix)
		if i < 0 {
			return names
		}
		rest = rest[i+len(urlFetchTokenPrefix)+1:]
		end := strings.IndexByte(rest, '}')
		if end < 0 {
			return names
		}
		name := NormalizeCounterName(rest[:end])
		rest = rest[end+1:]
		if name != "" && !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
}

// fetchUrlTokens resolves a response's {urlfetch:<name>} tokens: each distinct
// payload fans out one custom.fetch request to gossip, concurrently, and the
// rendered values come back keyed by the normalized payload expandCommand
// looks up. Runs beside bumpCounterTokens in runCustom, AFTER the gate has
// claimed the command's cooldown — so even a definition that fails every time
// cannot be hot-looped faster than its cooldown window.
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
func (p *Pipeline) fetchUrlTokens(ctx context.Context, c *module.Context, command, response string) map[string]string {
	if p.customFetch == nil || !strings.Contains(response, "{"+urlFetchTokenPrefix) {
		return nil
	}
	names := urlFetchNames(response)
	if len(names) == 0 {
		return nil
	}
	if len(names) > maxUrlFetchTokens {
		names = names[:maxUrlFetchTokens]
	}

	// One segment per fan-out; the event/command/broadcaster identity rides as
	// attributes, never in the span name.
	seg := startStage(ctx, "sesame.urlfetch")
	if seg != nil {
		seg.AddAttribute("command", command)
		seg.AddAttribute("broadcaster_id", c.BroadcasterID)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		results  = make(map[string]string, len(names))
		mu       sync.Mutex
		wg       sync.WaitGroup
		failures int
	)
	fail := func(name, text string, release func()) {
		mu.Lock()
		defer mu.Unlock()
		failures++
		// An empty text is the leave-verbatim outcome (bad_def): the name must
		// stay ABSENT from the map so expandCommand preserves the token.
		if text != "" {
			results[name] = text
		}
		if release != nil {
			release()
		}
	}

	for _, name := range names {
		dup, release := p.claimedUrlValue(ctx, c, name)
		if dup {
			fail(name, urlFetchUnavailableText, nil) // replay: fallback, no network
			continue
		}
		wg.Add(1)
		go func(name string, release func()) {
			defer wg.Done()
			render, resolved := p.resolveUrlToken(ctx, c, name)
			if !resolved {
				fail(name, render, release)
				cancel() // first failure cancels the batch; recorded results stand
				return
			}
			mu.Lock()
			results[name] = render
			mu.Unlock()
		}(name, release)
	}
	wg.Wait()

	result := "ok"
	if failures > 0 {
		result = "fallback"
	}
	endStage(seg, result)
	return results
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
// reply onto the render text per the failure-semantics table (the same
// mapping gossiprpc.FetchStatus documents):
//
//	denied | limited            -> [source unavailable]
//	upstream_error              -> [source error]
//	timeout | transport failure -> [source timed out]
//	bad_def (missing/inactive)  -> token left verbatim (resolved=false),
//	                               the unknown-token authoring signal
//
// Whatever body gossip DID extract always passes ExternalVar before rendering,
// regardless of gossip's own 5x256 server-side cap — the variable-provider
// boundary does not trust upstream capping (the sanitizeVar slash-strip also
// stops a hostile upstream from minting a "/ban ..." line through
// emitResponse's per-line split). An ok reply with nothing extractable counts
// as upstream-shaped breakage, not a missing definition. Unknown future
// statuses fail toward "unavailable". Upstream bodies are never logged.
func (p *Pipeline) resolveUrlToken(ctx context.Context, c *module.Context, name string) (render string, resolved bool) {
	reply, err := p.customFetch.Fetch(ctx, gossiprpc.Request{
		DefID:     name,
		ChannelID: c.Env.BroadcasterUserID,
		IsPremium: c.Regress.IsPremium(),
	})
	if err != nil {
		p.log.Warn("urlfetch token failed",
			zap.Uint64("broadcaster_id", c.BroadcasterID),
			zap.String("def", name),
			zap.Error(err),
		)
		return urlFetchTimeoutText, false
	}
	switch reply.Status {
	case gossiprpc.FetchOK:
		if len(reply.Values) == 0 || reply.Values[0] == "" {
			return urlFetchErrorText, false
		}
		return ExternalVar(reply.Values[0]), true
	case gossiprpc.FetchBadDef:
		return "", false // leave the token visible, like every unknown token
	case gossiprpc.FetchUpstreamError:
		return urlFetchErrorText, false
	case gossiprpc.FetchTimeout:
		return urlFetchTimeoutText, false
	default: // denied, limited, anything new: fail toward "unavailable"
		return urlFetchUnavailableText, false
	}
}
