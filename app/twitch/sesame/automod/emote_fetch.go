// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automod

import (
	"ItsBagelBot/pkg/codec"
	"cmp"
	"context"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"time"
)

// EmoteEndpoints are the public global-emote APIs the fetcher reads. They are
// unauthenticated read-only CDN endpoints; overridable so tests can point them at
// a local server.
type EmoteEndpoints struct {
	BTTV string // BetterTTV cached global emotes
	FFZ  string // FrankerFaceZ global set
	SVTV string // 7TV global emote set
}

// DefaultEmoteEndpoints are the live global-emote APIs.
var DefaultEmoteEndpoints = EmoteEndpoints{
	BTTV: "https://api.betterttv.net/3/cached/emotes/global",
	FFZ:  "https://api.frankerfacez.com/v1/set/global",
	SVTV: "https://7tv.io/v3/emote-sets/global",
}

// EmoteFetcher pulls the global emote code sets. Each source is best-effort: a
// source that fails is logged by the caller and simply contributes nothing, so a
// single provider outage never blocks the others.
type EmoteFetcher struct {
	client    *http.Client
	endpoints EmoteEndpoints
	// catalog is the last successfully installed per-provider snapshot, read
	// by everything that wants the codes THEMSELVES rather than a membership
	// test. It is written only by Refresh, on the same slow ticker the gate's
	// set rides, which is what lets a second reader exist without a second
	// HTTP path.
	catalog atomic.Pointer[EmoteCatalog]
}

// NewEmoteFetcher builds a fetcher. A nil client gets a short-timeout default.
func NewEmoteFetcher(client *http.Client, endpoints EmoteEndpoints) *EmoteFetcher {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &EmoteFetcher{client: client, endpoints: endpoints}
}

// EmoteCatalog is one refresh's emote codes kept PER PROVIDER, beside the
// merged set the gate suppresses false positives on.
//
// Decision record. The gate only ever asks "is this code an emote", so it
// takes the union (Codes) and this split costs it nothing. The split exists
// for the command lane: {7tvemotes} names one provider by name, and a token
// answering out of the union would print BTTV codes under a 7TV heading.
// Keeping both shapes on ONE fetch is the whole point — the hourly refresh
// that feeds the gate feeds the tokens, so a chat command never adds an
// upstream call of its own.
//
// Each list is sorted and deduplicated, which the merged set does not need to
// be. The command lane truncates its list to one chat line, so an unsorted
// list would print a different arbitrary slice of the same set every refresh;
// FFZ's sets arrive through a map, so its order is not even stable inside one
// process. Sorted, {7tvemotes} prints the same line twice running and a test
// can assert on it.
type EmoteCatalog struct {
	SevenTV []string
	BTTV    []string
	FFZ     []string
}

// Codes is the merged, de-duplicated union across providers — the shape the
// gate installs.
func (c EmoteCatalog) Codes() []string {
	out := make([]string, 0, len(c.SevenTV)+len(c.BTTV)+len(c.FFZ))
	out = append(out, c.SevenTV...)
	out = append(out, c.BTTV...)
	out = append(out, c.FFZ...)
	slices.Sort(out)
	return slices.Compact(out)
}

// FetchCatalog reads all three global sets into a per-provider catalog. A
// per-source error is returned alongside whatever DID load, so the caller can
// log the failure and still install the partial catalog: one provider outage
// never blanks the other two.
func (f *EmoteFetcher) FetchCatalog(ctx context.Context) (EmoteCatalog, error) {
	bttv, bttvErr := f.fetchBTTV(ctx)
	ffz, ffzErr := f.fetchFFZ(ctx)
	seven, sevenErr := f.fetch7TV(ctx)
	return EmoteCatalog{
		SevenTV: sortedCodes(seven),
		BTTV:    sortedCodes(bttv),
		FFZ:     sortedCodes(ffz),
	}, cmp.Or(bttvErr, ffzErr, sevenErr)
}

// sortedCodes keeps the codes that are a single printable word and returns
// them sorted and unique.
//
// The filter is at this boundary rather than at either reader because both
// readers want it and neither can express it as cheaply. The gate matches a
// message WORD against the set, so a "code" carrying a space or a control
// character could never match one and only costs memory. The command lane
// prints the codes into a chat line, where a control character would mint an
// extra line through the response splitter and a leading slash would turn it
// into a moderation verb — the {args} threat, arriving from a third-party API
// instead of a viewer. No real emote code has either: Twitch tokenizes chat on
// whitespace, so a code with a space in it is unusable on every provider.
func sortedCodes(codes []string) []string {
	out := make([]string, 0, len(codes))
	for _, c := range codes {
		if isWordCode(c) {
			out = append(out, c)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// isWordCode reports whether c is one printable, space-free token that does
// not open with a slash.
func isWordCode(c string) bool {
	if c == "" || c[0] == '/' {
		return false
	}
	return strings.IndexFunc(c, func(r rune) bool { return r <= ' ' || r == '\x7f' }) < 0
}

// Catalog returns the last installed per-provider snapshot. Before the first
// successful refresh — and on a process whose refresher is switched off — that
// is the zero catalog: three empty lists, which is a real answer ("nothing
// loaded") rather than an error the caller has to branch on.
func (f *EmoteFetcher) Catalog() EmoteCatalog {
	if cat := f.catalog.Load(); cat != nil {
		return *cat
	}
	return EmoteCatalog{}
}

// Refresh fetches the global sets and installs them on the gate, returning how
// many codes were installed. A partial fetch (one source down) still installs the
// codes that did load and returns the first source error, so the caller can log
// it while keeping the working suppression set. Nothing is installed only when
// every source fails (zero codes); the gate keeps its previous set.
func (f *EmoteFetcher) Refresh(ctx context.Context, gate *Gate) (int, error) {
	cat, err := f.FetchCatalog(ctx)
	codes := cat.Codes()
	if len(codes) > 0 {
		gate.SetEmotes(NewEmoteSet(codes))
		f.catalog.Store(&cat)
	}
	return len(codes), err
}

func (f *EmoteFetcher) fetchBTTV(ctx context.Context) ([]string, error) {
	var arr []struct {
		Code string `json:"code"`
	}
	if err := f.getJSON(ctx, f.endpoints.BTTV, &arr); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(arr))
	for _, e := range arr {
		out = append(out, e.Code)
	}
	return out, nil
}

func (f *EmoteFetcher) fetchFFZ(ctx context.Context) ([]string, error) {
	var doc struct {
		Sets map[string]struct {
			Emoticons []struct {
				Name string `json:"name"`
			} `json:"emoticons"`
		} `json:"sets"`
	}
	if err := f.getJSON(ctx, f.endpoints.FFZ, &doc); err != nil {
		return nil, err
	}
	var out []string
	for _, set := range doc.Sets {
		for _, e := range set.Emoticons {
			out = append(out, e.Name)
		}
	}
	return out, nil
}

func (f *EmoteFetcher) fetch7TV(ctx context.Context) ([]string, error) {
	var doc struct {
		Emotes []struct {
			Name string `json:"name"`
		} `json:"emotes"`
	}
	if err := f.getJSON(ctx, f.endpoints.SVTV, &doc); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(doc.Emotes))
	for _, e := range doc.Emotes {
		out = append(out, e.Name)
	}
	return out, nil
}

// getJSON GETs url and decodes the body into dst. The body is size-limited so a
// misbehaving endpoint cannot exhaust memory.
func (f *EmoteFetcher) getJSON(ctx context.Context, url string, dst any) error {
	if url == "" {
		return fmt.Errorf("empty endpoint")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	res, err := f.client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<20))
		_ = res.Body.Close()
	}()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: status %d", url, res.StatusCode)
	}
	return codec.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(dst)
}
