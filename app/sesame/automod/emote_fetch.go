package automod

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
}

// NewEmoteFetcher builds a fetcher. A nil client gets a short-timeout default.
func NewEmoteFetcher(client *http.Client, endpoints EmoteEndpoints) *EmoteFetcher {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &EmoteFetcher{client: client, endpoints: endpoints}
}

// Fetch reads all three global sets and returns their merged, de-duplicated codes.
// A per-source error is returned alongside whatever codes did load, so the caller
// can log the failure but still install the partial set.
func (f *EmoteFetcher) Fetch(ctx context.Context) ([]string, error) {
	seen := make(map[string]struct{}, 2048)
	var firstErr error

	add := func(codes []string, err error) {
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			return
		}
		for _, c := range codes {
			if c != "" {
				seen[c] = struct{}{}
			}
		}
	}

	add(f.fetchBTTV(ctx))
	add(f.fetchFFZ(ctx))
	add(f.fetch7TV(ctx))

	out := make([]string, 0, len(seen))
	for c := range seen {
		out = append(out, c)
	}
	return out, firstErr
}

// Refresh fetches the global sets and installs them on the gate, returning how
// many codes were installed. A partial fetch (one source down) still installs the
// codes that did load and returns the first source error, so the caller can log
// it while keeping the working suppression set. Nothing is installed only when
// every source fails (zero codes); the gate keeps its previous set.
func (f *EmoteFetcher) Refresh(ctx context.Context, gate *Gate) (int, error) {
	codes, err := f.Fetch(ctx)
	if len(codes) > 0 {
		gate.SetEmotes(NewEmoteSet(codes))
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
	return json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(dst)
}
