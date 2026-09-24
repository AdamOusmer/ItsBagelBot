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

type EmoteEndpoints struct {
	BTTV string
	FFZ  string
	SVTV string
}

var DefaultEmoteEndpoints = EmoteEndpoints{
	BTTV: "https://api.betterttv.net/3/cached/emotes/global",
	FFZ:  "https://api.frankerfacez.com/v1/set/global",
	SVTV: "https://7tv.io/v3/emote-sets/global",
}

type EmoteFetcher struct {
	client    *http.Client
	endpoints EmoteEndpoints
	catalog   atomic.Pointer[EmoteCatalog]
}

func NewEmoteFetcher(client *http.Client, endpoints EmoteEndpoints) *EmoteFetcher {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &EmoteFetcher{client: client, endpoints: endpoints}
}

type EmoteCatalog struct {
	SevenTV []string
	BTTV    []string
	FFZ     []string
}

func (c EmoteCatalog) Codes() []string {
	out := make([]string, 0, len(c.SevenTV)+len(c.BTTV)+len(c.FFZ))
	out = append(out, c.SevenTV...)
	out = append(out, c.BTTV...)
	out = append(out, c.FFZ...)
	slices.Sort(out)
	return slices.Compact(out)
}

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

// Codes are echoed into chat unescaped: a leading slash would run a moderation verb.
func isWordCode(c string) bool {
	if c == "" || c[0] == '/' {
		return false
	}
	return strings.IndexFunc(c, func(r rune) bool { return r <= ' ' || r == '\x7f' }) < 0
}

func (f *EmoteFetcher) Catalog() EmoteCatalog {
	if cat := f.catalog.Load(); cat != nil {
		return *cat
	}
	return EmoteCatalog{}
}

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
