// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package linkcheck

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

// maxFeedEntries caps one merged snapshot. The live feeds measure ~10-30k
// hosts; the cap exists so a compromised or replaced upstream cannot balloon
// sesame's memory with garbage entries.
const maxFeedEntries = 500_000

// feedFormat is how a source encodes hosts.
type feedFormat uint8

const (
	// FormatLines is one URL per line ("https://evil.example/path") — the
	// OpenPhish community feed shape.
	FormatLines feedFormat = iota
	// FormatHosts is an /etc/hosts-style file ("0.0.0.0 evil.example") — the
	// URLhaus hostfile shape, whose leading address column is discarded.
	FormatHosts
)

// FeedSource names one upstream blocklist and how to read it.
type FeedSource struct {
	Name string
	URL  string
	// AuthKey is sent as the Auth-Key header when set. abuse.ch endpoints
	// require one since mid-2023 (free registration); sources without it leave
	// this empty.
	AuthKey string
	Format  feedFormat
}

// DefaultFeedSources are the no-signup-required community feeds. URLhaus joins
// only when its auth key is configured (see Options in checker.go); OpenPhish
// needs nothing.
var DefaultFeedSources = []FeedSource{
	{Name: "openphish", URL: "https://openphish.com/feed.txt", Format: FormatLines},
}

// Feeds is the refreshable host blocklist fed by FeedSources. Has reads from
// an atomically swapped snapshot, so refresh failures keep serving the last
// good set and the hot path pays a map lookup and nothing else.
type Feeds struct {
	sources []FeedSource
	client  *http.Client
	set     atomic.Pointer[map[string]struct{}]
}

// NewFeeds builds the blocklist aggregator around sources (empty selects
// DefaultFeedSources) and an optional client. It starts EMPTY: until the first
// Refresh succeeds, Has answers false everywhere and the layer contributes
// nothing — same loaded-empty semantics as the emote suppression set.
func NewFeeds(sources []FeedSource, client *http.Client) *Feeds {
	if len(sources) == 0 {
		sources = DefaultFeedSources
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	f := &Feeds{sources: sources, client: client}
	empty := make(map[string]struct{})
	f.set.Store(&empty)
	return f
}

// Refresh pulls every source best-effort and swaps in the merged host set,
// returning how many hosts installed. One dead source never blocks the others;
// a total failure keeps the previous snapshot and returns the first error so
// the caller logs it without losing coverage.
func (f *Feeds) Refresh(ctx context.Context) (int, error) {
	merged := make(map[string]struct{}, 8192)
	var firstErr error

	for _, src := range f.sources {
		n, err := f.fetchSource(ctx, src, merged)
		switch {
		case err != nil && firstErr == nil:
			firstErr = err
		case err == nil:
			// count below
		}
		_ = n // per-source counts fold into len(merged); errors matter more
	}

	if len(merged) == 0 {
		return 0, fmt.Errorf("linkcheck feeds: every source failed: %w", firstErr)
	}
	if len(merged) > maxFeedEntries {
		return 0, fmt.Errorf("linkcheck feeds: merged snapshot %d exceeds cap %d", len(merged), maxFeedEntries)
	}
	f.set.Store(&merged)
	return len(merged), firstErr
}

// fetchSource downloads one source into merged, returning its contribution.
func (f *Feeds) fetchSource(ctx context.Context, src FeedSource, merged map[string]struct{}) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src.URL, nil)
	if err != nil {
		return 0, err
	}
	if src.AuthKey != "" {
		req.Header.Set("Auth-Key", src.AuthKey)
	}
	res, err := f.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<20))
		_ = res.Body.Close()
	}()
	if res.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("%s: status %d", src.Name, res.StatusCode)
	}

	added := 0
	sc := bufio.NewScanner(io.LimitReader(res.Body, 32<<20))
	sc.Buffer(make([]byte, 0, 64*1024), 64*1024)
	for sc.Scan() {
		host := parseFeedLine(sc.Bytes(), src.Format)
		if host == "" || len(merged) >= maxFeedEntries {
			continue
		}
		if _, dup := merged[host]; !dup {
			merged[host] = struct{}{}
			added++
		}
	}
	return added, sc.Err()
}

// parseFeedLine extracts the lowercase host from one feed line per format,
// returning "" for blanks and comments. Lines that do not carry a plausible
// host are skipped rather than fatal: feeds occasionally interleave prose.
func parseFeedLine(line []byte, format feedFormat) string {
	line = bytes.TrimSpace(line)
	if len(line) == 0 || line[0] == '#' {
		return ""
	}
	var host string
	switch format {
	case FormatLines:
		host = hostFromURLLine(string(line))
	case FormatHosts:
		host = hostFromHostsLine(string(line))
	}
	if !validHost(host) {
		return ""
	}
	return host
}

// hostFromURLLine pulls the host out of an OpenPhish URL line (scheme optional):
// past the scheme, then hostOf trims path, userinfo and port the same way chat
// tokens are trimmed.
func hostFromURLLine(line string) string {
	if k := strings.Index(line, "://"); k >= 0 {
		line = line[k+3:]
	}
	return strings.ToLower(strings.TrimSuffix(hostOf(line), "."))
}

// hostFromHostsLine pulls the host out of an /etc/hosts-style line
// ("0.0.0.0 evil.example"), discarding the leading address column.
func hostFromHostsLine(line string) string {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return ""
	}
	return strings.ToLower(fields[1])
}

// Has reports whether host (or any parent of it up three labels) sits on the
// current snapshot. Parent walking catches subdomain rotation — feeds list
// registrable domains but also full hosts, and scam infrastructure moves
// a.b.evil.example -> c.evil.example faster than feeds refresh. Three levels
// bounds the loop: deeper chains are CDN-shaped, not scam-shaped, and each
// level costs one map probe.
func (f *Feeds) Has(host string) bool {
	set := f.set.Load()
	h := host
	for depth := 0; depth <= 3 && h != ""; depth++ {
		if _, ok := (*set)[h]; ok {
			return true
		}
		dot := strings.IndexByte(h, '.')
		if dot < 0 {
			return false
		}
		h = h[dot+1:]
	}
	return false
}
