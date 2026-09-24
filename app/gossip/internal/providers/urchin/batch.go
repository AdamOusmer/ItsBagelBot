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

const (
	batchWindowDefault = 2 * time.Millisecond
	batchLimit         = 100
)

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

type batchOutcome struct {
	resp tagsResponse
	err  error
}

type batchSlot struct {
	waiters []chan batchOutcome
}

type tagsBatcher struct {
	p      *api
	window time.Duration
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
		return tagsResponse{}, ctx.Err()
	case out := <-ch:
		return out.resp, out.err
	}
}

func (b *tagsBatcher) run() {
	for range b.notify {
		time.Sleep(b.window)
		b.flush()
	}
}

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

func (b *tagsBatcher) post(uuids []string, pending map[string]*batchSlot) {
	ctx, cancel := context.WithTimeout(context.Background(), httpTimeout)
	defer cancel()

	lookup, callErr := b.fetchBatch(ctx, uuids)
	for _, u := range uuids {
		deliver(pending[u], outcomeFor(u, lookup, callErr))
	}
	b.hydrate(ctx, uuids, lookup, callErr)
}

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
			k = c
		}
		lookup[k] = v
	}
	return lookup, nil
}

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

var notFoundErr = &core.UpstreamError{Status: 404, Message: "player not found"}

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

func toTagsResponse(tags []batchTag) []playerTag {
	out := make([]playerTag, len(tags))
	for i, t := range tags {
		out[i] = playerTag{TagType: t.TagType, Reason: t.Reason, AddedOn: t.AddedOn}
	}
	return out
}
