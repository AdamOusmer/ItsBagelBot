// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package urchin

import (
	"context"
	"net/url"
	"strings"
	"time"

	"ItsBagelBot/app/gossip/internal/core"
	"ItsBagelBot/app/gossip/internal/provider"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/ratelimit"
)

const (
	sessionTTL  = 2 * time.Minute
	sniperTTL   = 10 * time.Minute
	tagsTTL     = 10 * time.Minute
	negativeTTL = 5 * time.Minute

	httpTimeout    = 10 * time.Second
	handlerTimeout = 15 * time.Second

	rateWindowSeconds = 300.0
	maxBurst          = 8.0

	millisPerSecond = 1000
)

type Config struct {
	BaseURL     string
	APIKey      string
	RateLimit   float64
	BatchWindow time.Duration
}

const providerName = "urchin"

type api struct {
	http    *core.HTTPClient
	cache   *core.Cache
	key     string
	limiter *ratelimit.Limiter
	buckets core.Buckets
	tags    *tagsBatcher
}

func New(cfg Config, d provider.Deps) provider.Provider {
	b := provider.NewProvider(providerName, d).Trusted()
	p := newAPI(cfg, d, b)
	for _, period := range []string{"daily", "weekly", "monthly"} {
		b.Endpoint(period).Timeout(handlerTimeout).
			Cached(sessionTTL, negativeTTL).
			Reply(sessionErrReply).
			Budget(p.budget).
			Fallback("stats lookup failed").
			Fetch(p.sessionFetch(period))
	}
	b.Endpoint("sniper").Timeout(handlerTimeout).
		Cached(sniperTTL, negativeTTL).
		Reply(sniperErrReply).
		Budget(p.sniperBudget).
		Fallback("score lookup failed").
		Fetch(p.sniperFetch)
	b.Endpoint("tags").Timeout(handlerTimeout).
		Cached(tagsTTL, negativeTTL).
		Reply(tagsErrReply).
		Budget(p.budget).
		Fallback("tags lookup failed").
		Fetch(p.tagsFetch)
	return b.Build()
}

func newAPI(cfg Config, d provider.Deps, b *provider.Builder) *api {
	base := strings.TrimSuffix(cfg.BaseURL, "/")
	if base == "" {
		base = "https://api.urchin.gg"
	}
	if cfg.RateLimit <= 0 {
		cfg.RateLimit = 600
	}
	window := cfg.BatchWindow
	if window <= 0 {
		window = batchWindowDefault
	}
	a := &api{
		http:    b.Client(base, map[string]string{"X-API-Key": cfg.APIKey}, httpTimeout),
		cache:   d.Cache,
		key:     cfg.APIKey,
		limiter: d.Limiter,
		buckets: core.NewPacedBuckets("ratelimit:gossip:urchin", cfg.RateLimit, rateWindowSeconds, maxBurst),
	}
	a.tags = newTagsBatcher(a, window)
	return a
}

func sessionErrReply(id, msg string) any {
	return gossiprpc.UrchinSessionReply{Player: id, Error: msg}
}
func sniperErrReply(id, msg string) any { return gossiprpc.UrchinSniperReply{Player: id, Error: msg} }
func tagsErrReply(id, msg string) any   { return gossiprpc.UrchinTagsReply{Player: id, Error: msg} }

func (p *api) budget(ctx context.Context, req gossiprpc.Request) error {
	return p.buckets.Enforce(ctx, p.limiter, req.IsPremium)
}

func (p *api) sniperBudget(ctx context.Context, req gossiprpc.Request) error {
	if err := p.budget(ctx, req); err != nil {
		return err
	}
	return p.budget(ctx, req)
}

type account string

func (a account) String() string { return string(a) }

func (a account) cacheKey() string { return strings.ToLower(strings.TrimSpace(string(a))) }

type sessionResponse struct {
	UUID        string           `json:"uuid"`
	DisplayName *string          `json:"displayname"`
	From        int64            `json:"from"`
	Delta       codec.RawMessage `json:"delta"`
}

type sessionDelta struct {
	Stats struct {
		Bedwars map[string]codec.RawMessage `json:"Bedwars"`
	} `json:"stats"`
	Achievements map[string]codec.RawMessage `json:"achievements"`
}

func numDelta(raw codec.RawMessage) int64 {
	if len(raw) == 0 {
		return 0
	}
	var n codec.Number
	if err := codec.Unmarshal(raw, &n); err != nil {
		return 0
	}
	f, err := n.Float64()
	if err != nil {
		return 0
	}
	return int64(f)
}

func (p *api) sessionFetch(period string) provider.FetchFunc {
	return func(ctx context.Context, req gossiprpc.Request, id provider.ID) (any, error) {
		return p.fetchSession(ctx, period, account(id.Display))
	}
}

func (p *api) fetchSession(ctx context.Context, period string, acct account) (gossiprpc.UrchinSessionReply, error) {
	var resp sessionResponse
	q := url.Values{"player": {acct.String()}}
	if err := p.http.GetJSON(ctx, "/v3/player/sessions/"+period, q, &resp); err != nil {
		return gossiprpc.UrchinSessionReply{}, err
	}

	reply := gossiprpc.UrchinSessionReply{
		Player:    playerName(acct, resp.DisplayName),
		SinceUnix: resp.From / millisPerSecond,
	}
	if len(resp.Delta) > 0 {
		var d sessionDelta
		if err := codec.Unmarshal(resp.Delta, &d); err == nil {
			bw := d.Stats.Bedwars
			reply.Wins = numDelta(bw["wins_bedwars"])
			reply.Losses = numDelta(bw["losses_bedwars"])
			reply.FinalKills = numDelta(bw["final_kills_bedwars"])
			reply.FinalDeaths = numDelta(bw["final_deaths_bedwars"])
			reply.BedsBroken = numDelta(bw["beds_broken_bedwars"])
			reply.GamesPlayed = numDelta(bw["games_played_bedwars"])
			reply.Levels = numDelta(d.Achievements["bedwars_level"])
		}
	}
	return reply, nil
}

type tagsResponse struct {
	UUID        string      `json:"uuid"`
	DisplayName *string     `json:"displayname"`
	Tags        []playerTag `json:"tags"`
}

type playerTag struct {
	TagType string `json:"tag_type"`
	Reason  string `json:"reason"`
	AddedOn int64  `json:"added_on"`
}

func (p *api) fetchTags(ctx context.Context, acct account) (tagsResponse, error) {
	var resp tagsResponse
	return resp, p.http.GetJSON(ctx, "/v3/player/tags", url.Values{"player": {acct.String()}}, &resp)
}

func (p *api) playerTags(ctx context.Context, acct account) (tagsResponse, error) {
	id := acct.cacheKey()
	fetch := func(ctx context.Context) (tagsResponse, error) { return p.fetchTags(ctx, acct) }
	if cuuid, ok := canonicalUUID(acct); ok {
		id = cuuid
		fetch = func(ctx context.Context) (tagsResponse, error) { return p.tags.await(ctx, cuuid) }
	}
	key := core.Key(providerName, "playertags", id)
	return core.Cached(ctx, p.cache, key, tagsTTL, negativeTTL, nil, fetch)
}

func (p *api) tagsFetch(ctx context.Context, req gossiprpc.Request, id provider.ID) (any, error) {
	acct := account(id.Display)
	resp, err := p.playerTags(ctx, acct)
	if err != nil {
		return nil, err
	}
	out := gossiprpc.UrchinTagsReply{
		Player: playerName(acct, resp.DisplayName),
		Tags:   make([]gossiprpc.UrchinTag, 0, len(resp.Tags)),
	}
	for _, t := range resp.Tags {
		out.Tags = append(out.Tags, gossiprpc.UrchinTag{Type: t.TagType, Reason: t.Reason, AddedOn: t.AddedOn / millisPerSecond})
	}
	return out, nil
}

type cubelifyResponse struct {
	Score struct {
		Value float64 `json:"value"`
		Mode  string  `json:"mode"`
	} `json:"score"`
	Tags []codec.RawMessage `json:"tags"`
}

func (p *api) sniperFetch(ctx context.Context, req gossiprpc.Request, id provider.ID) (any, error) {
	acct := account(id.Display)
	tags, err := p.playerTags(ctx, acct)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(tags.UUID) == "" {
		return nil, &core.UpstreamError{Status: 404, Message: "player not found"}
	}
	var resp cubelifyResponse
	name := playerName(acct, tags.DisplayName)
	q := url.Values{"uuid": {tags.UUID}, "key": {p.key}, "name": {name}}
	if err := p.http.GetJSON(ctx, "/v3/cubelify", q, &resp); err != nil {
		return nil, err
	}
	return gossiprpc.UrchinSniperReply{
		Player:   name,
		Score:    resp.Score.Value,
		Mode:     resp.Score.Mode,
		TagCount: len(resp.Tags),
	}, nil
}

func playerName(acct account, display *string) string {
	if _, isUUID := canonicalUUID(acct); !isUUID {
		return acct.String()
	}
	return displayOr(display, acct.String())
}

func displayOr(display *string, fallback string) string {
	if display != nil && *display != "" {
		return stripMinecraftCodes(*display)
	}
	return fallback
}

func stripMinecraftCodes(s string) string {
	if !strings.Contains(s, "§") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	skip := false
	for _, r := range s {
		if skip {
			skip = false
			continue
		}
		if r == '§' {
			skip = true
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
