// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package urchin

import (
	"context"
	"encoding/hex"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"ItsBagelBot/app/gossip/internal/core"
	"ItsBagelBot/pkg/codec"
)

// Cross-channel micro-batching for Coral blacklist lookups (issue #616).
//
// The per-key singleflight inside core.Cache collapses concurrent misses for
// ONE player, but N distinct players queried across channels in the same
// moment still cost N separate GET /v3/player/tags calls — and Coral's
// measured edge wall (~40 rapid requests, ~4/s refill; see maxBurst) is
// tripped by exactly that shape long before the 600/5min key quota runs out.
// This batcher collects UUID-shaped playertags misses over one short window
// and resolves all of them with a single POST /v3/players, then hydrates each
// player's entry back into the shared playertags cache so the tags command,
// the sniper uuid hop and every later single-player query read it as a hit.
//
// Why UUID-shaped accounts only: the batch endpoint accepts nothing else.
// Its contract (api.urchin.gg OpenAPI, batch_lookup) takes a bare uuid array;
// usernames are not resolved and malformed entries are silently skipped. A
// username still needs Coral's own Mojang resolution, which today only the
// individual GET performs, so username lookups keep the old one-request-per-
// player path unchanged. Minecraft usernames cap at 16 characters, so a
// 32-hex-digit identifier is unambiguously a UUID — there is no shape on which
// this routing could flip between calls.
const (
	// batchWindowDefault is how long the first miss of a wave waits for
	// company before its batch fires. The issue sketched 50–100ms; that was
	// written before it met this fleet's latency shape — sesame's hot path
	// answers at a 64µs p99, so even a cold upstream fetch must not inherit
	// tens of milliseconds of queueing on its account. 2ms is the compromise
	// measured against how co-tenancy actually arrives here: same-burst
	// lookups fanned out by the bus land on one pod within microseconds-to-
	// low-single-digit-ms of each other, so 2ms still merges a burst into one
	// POST, while chat-driven queries seconds apart were never going to share
	// a window at any size — those are served by the cache layers, not the
	// batcher. Anything the wider window would have caught, the per-key
	// singleflight underneath already collapses instead.
	batchWindowDefault = 2 * time.Millisecond
	// batchLimit is Coral's own ceiling on one batch_lookup call. A window
	// collecting more fires sequential POSTs of at most this many UUIDs.
	batchLimit = 100
)

// batchRequest / batchResponse mirror Coral's BatchRequest/BatchResponse.
// Tag objects carry more members than gossip reads (expires_at, added_by, ...)
// ; like tagsResponse, only the subset the replies need is decoded. AddedOn is
// unix milliseconds, matching the individual tags endpoint's field of the same
// name.
type batchRequest struct {
	UUIDs []string `json:"uuids"`
}

type batchTag struct {
	TagType string `json:"tag_type"`
	Reason  string `json:"reason"`
	AddedOn int64  `json:"added_on"`
}

type batchResponse struct {
	Players map[string][]batchTag `json:"players"`
}

// batchOutcome is what one waiter receives: either the player's tags-shaped
// record or the failure standing in for it. It reuses tagsResponse rather than
// a batch-native shape so everything downstream of playerTags (the tags reply
// shaper, sniper's uuid resolution) stays agnostic to how the answer arrived.
// DisplayName is nil on this path — the batch endpoint returns no display
// names — so those replies fall back to the identifier as typed.
type batchOutcome struct {
	resp tagsResponse
	err  error
}

// batchSlot holds every waiter that joined on one uuid within the window. The
// channels are per-waiter on purpose: a slot owns ONE outcome but MANY waiters,
// and a single shared channel would hand that outcome to whichever waiter woke
// first and hang the rest until their handler timeouts.
type batchSlot struct {
	waiters []chan batchOutcome
}

// tagsBatcher coalesces uuid-keyed playertags misses into batch_lookup calls.
// It is pod-local by design: coordination beyond one process would need a
// shared queue for the marginal players a second replica sees in the same
// window, while the Valkey-backed cache already de-duplicates the far larger
// repeat traffic across replicas.
type tagsBatcher struct {
	p *api
	// window is the collection period; the FIRST caller of a wave pays it in
	// full, everyone joining behind it rides along free.
	window time.Duration
	// notify wakes the flush loop; capacity 1 makes it a level trigger — a
	// burst of arrivals costs one wakeup, and an arrival during a flush folds
	// into the next window instead of being lost.
	notify chan struct{}

	mu     sync.Mutex
	slots  map[string]*batchSlot
	starts sync.Once
}

func newTagsBatcher(p *api, window time.Duration) *tagsBatcher {
	return &tagsBatcher{
		p:      p,
		window: window,
		notify: make(chan struct{}, 1),
		slots:  make(map[string]*batchSlot),
	}
}

// canonicalUUID reports whether acct is a bare player UUID (dashes optional,
// any case) and returns its canonical form: lowercase, undashed — the exact
// spelling Coral's batch request and response keys use. Normalizing here means
// dashed/undashed/mixed-case spellings of one player collapse onto one cache
// entry AND one batch line instead of three.
func canonicalUUID(a account) (string, bool) {
	s := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(string(a))), "-", "")
	if len(s) != 32 {
		return "", false
	}
	if _, err := hex.DecodeString(s); err != nil {
		return "", false
	}
	return s, true
}

// await registers ctx's caller under uuid and blocks until the batch carrying
// it lands. Duplicate registrations within one window append to the same slot
// and are answered by the same upstream call. A caller whose context dies
// mid-window simply stops listening; its channel is buffered, so the eventual
// delivery never blocks the flush and gets collected by the garbage collector.
func (b *tagsBatcher) await(ctx context.Context, uuid string) (tagsResponse, error) {
	b.starts.Do(func() { go b.run() })

	ch := make(chan batchOutcome, 1)
	b.mu.Lock()
	slot, ok := b.slots[uuid]
	if !ok {
		slot = &batchSlot{}
		b.slots[uuid] = slot
	}
	slot.waiters = append(slot.waiters, ch)
	b.mu.Unlock()
	if !ok {
		select {
		case b.notify <- struct{}{}:
		default:
		}
	}

	select {
	case <-ctx.Done():
		// The handler timeout or a bus cancellation caught up with us. The
		// slot may already have been swapped out for flushing; either way the
		// buffered delivery below is fire-and-forget.
		return tagsResponse{}, ctx.Err()
	case out := <-ch:
		return out.resp, out.err
	}
}

// run is the flush loop: sleep one window after each wake, then post whatever
// gathered. The loop lives for the process — gossip providers have no shutdown
// path — and idles as a blocked receive when no uuid-shaped lookups arrive.
func (b *tagsBatcher) run() {
	for range b.notify {
		time.Sleep(b.window)
		b.flush()
	}
}

// flush swaps out the pending slots and drains them in batchLimit-sized
// chunks. UUIDs are sorted so a given set of callers always produces the same
// request body: deterministic bodies make the upstream-side caching Coral may
// do behind batch_lookup maximally effective, and the wire format testable.
func (b *tagsBatcher) flush() {
	b.mu.Lock()
	pending := b.slots
	b.slots = make(map[string]*batchSlot)
	b.mu.Unlock()

	uuids := make([]string, 0, len(pending))
	for u := range pending {
		uuids = append(uuids, u)
	}
	slices.Sort(uuids)

	for len(uuids) > 0 {
		chunk := uuids[:min(len(uuids), batchLimit)]
		uuids = uuids[len(chunk):]
		b.post(chunk, pending)
	}
}

// post resolves one chunk: one POST /v3/players, one parse, then demux to
// every waiter plus hydration of each player's individual cache entry.
//
// Budget: the POST deliberately spends NO token of its own. Every route into
// await is preceded on this same pod by an endpoint-level admission — the
// flow's Budget check for a cold lookup, sniperBudget's doubled spend for the
// uuid hop, or the SWR refresh's admit before its rebuild — so fleet-wide,
// batched upstream calls can never outnumber tokens spent, and batching only
// shrinks real load under the existing metering. Charging once more per POST
// was considered and rejected: it double-bills busy windows, and skipping the
// per-caller charge instead (the literal "one token per batch" reading) would
// move the premium-reserve decision out of the per-caller admission the flow
// doctrine requires it live in.
func (b *tagsBatcher) post(uuids []string, pending map[string]*batchSlot) {
	// Background context, not a waiter's: waiters may give up (handler
	// timeout, channel close) but the call must finish regardless, because its
	// answer hydrates entries for players nobody is still waiting on.
	ctx, cancel := context.WithTimeout(context.Background(), httpTimeout)
	defer cancel()

	lookup, callErr := b.fetchBatch(ctx, uuids)
	for _, u := range uuids {
		deliver(pending[u], outcomeFor(u, lookup, callErr))
	}
	b.hydrate(ctx, uuids, lookup, callErr)
}

// fetchBatch performs the chunk's one POST /v3/players and returns the parsed
// response keyed by canonical uuid. On a failed call the lookup is nil and the
// error travels separately — a whole-request failure says nothing about any
// individual player (a 404 here would mean we malformed the request, not that
// these players don't exist), so it must never be mistaken for per-player truth.
func (b *tagsBatcher) fetchBatch(ctx context.Context, uuids []string) (map[string][]batchTag, error) {
	var resp batchResponse
	body, err := codec.Marshal(batchRequest{UUIDs: uuids})
	if err == nil {
		err = b.p.http.Do(ctx, core.Request{
			Method: http.MethodPost,
			Path:   "/v3/players",
			Body:   body,
		}, &resp)
	}
	if err != nil {
		return nil, err
	}

	lookup := make(map[string][]batchTag, len(resp.Players))
	for k, v := range resp.Players {
		if c, ok := canonicalUUID(account(k)); ok {
			k = c // defensive: a dashed or uppercase echo would strand its player as "missing"
		}
		lookup[k] = v
	}
	return lookup, nil
}

// outcomeFor shapes one player's answer out of the batch result. Absent from a
// successful response = no Coral record for this uuid (present-but-tagless
// players DO appear, with an empty array); it becomes the same 404 the
// individual GET produces for an unknown player.
func outcomeFor(uuid string, lookup map[string][]batchTag, callErr error) batchOutcome {
	if callErr != nil {
		return batchOutcome{err: callErr}
	}
	tags, found := lookup[uuid]
	if !found {
		return batchOutcome{err: notFoundErr}
	}
	return batchOutcome{resp: tagsResponse{UUID: uuid, Tags: toTagsResponse(tags)}}
}

// hydrate writes each player's individual playertags entry through StoreCached.
// A failed CALL hydrates nothing: only a successful response carries per-player
// truth worth remembering, so on failure every key stays uncached and the next
// window retries it.
func (b *tagsBatcher) hydrate(ctx context.Context, uuids []string, lookup map[string][]batchTag, callErr error) {
	for _, u := range uuids {
		out := outcomeFor(u, lookup, callErr)
		core.StoreCached(ctx, b.p.cache, core.StoreRequest[tagsResponse]{
			Key:         core.Key(providerName, "playertags", u),
			TTL:         tagsTTL,
			NegativeTTL: negativeTTL,
			Value:       out.resp,
			Err:         out.err,
		})
	}
}

// notFoundErr is the shared "player has no Coral record" negative.
var notFoundErr = &core.UpstreamError{Status: 404, Message: "player not found"}

// deliver fans one outcome out to a slot's waiters. Channels are buffered and
// sends non-blocking: every waiter gets exactly its own copy, a departed
// waiter's slot fills and is dropped, and the flush never waits on anyone.
func deliver(s *batchSlot, out batchOutcome) {
	if s == nil {
		return
	}
	for _, ch := range s.waiters {
		select {
		case ch <- out:
		default:
		}
	}
}

// toTagsResponse converts batch tag records into the shape the rest of the
// provider reads. AddedOn stays in milliseconds: tagsFetch divides by 1000
// when shaping the reply, and the batch path must land in that same unit.
func toTagsResponse(tags []batchTag) []playerTag {
	out := make([]playerTag, len(tags))
	for i, t := range tags {
		out[i] = playerTag{TagType: t.TagType, Reason: t.Reason, AddedOn: t.AddedOn}
	}
	return out
}
