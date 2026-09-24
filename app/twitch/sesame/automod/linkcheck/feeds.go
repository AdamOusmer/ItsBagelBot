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

const maxFeedEntries = 500_000

type feedFormat uint8

const (
	FormatLines feedFormat = iota
	FormatHosts
)

type FeedSource struct {
	Name    string
	URL     string
	AuthKey string
	Format  feedFormat
}

var DefaultFeedSources = []FeedSource{
	{Name: "openphish", URL: "https://openphish.com/feed.txt", Format: FormatLines},
}

type Feeds struct {
	sources []FeedSource
	client  *http.Client
	set     atomic.Pointer[map[string]struct{}]
}

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

func (f *Feeds) Refresh(ctx context.Context) (int, error) {
	merged := make(map[string]struct{}, 8192)
	var firstErr error

	for _, src := range f.sources {
		if _, err := f.fetchSource(ctx, src, merged); err != nil && firstErr == nil {
			firstErr = err
		}
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

func (f *Feeds) fetchSource(ctx context.Context, src FeedSource, merged map[string]struct{}) (int, error) {
	res, err := f.get(ctx, src)
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
	return mergeFeed(res.Body, src.Format, merged)
}

func (f *Feeds) get(ctx context.Context, src FeedSource) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src.URL, nil)
	if err != nil {
		return nil, err
	}
	if src.AuthKey != "" {
		req.Header.Set("Auth-Key", src.AuthKey)
	}
	return f.client.Do(req)
}

func mergeFeed(body io.Reader, format feedFormat, merged map[string]struct{}) (int, error) {
	added := 0
	sc := bufio.NewScanner(io.LimitReader(body, 32<<20))
	sc.Buffer(make([]byte, 0, 64*1024), 64*1024)
	for sc.Scan() {
		host := parseFeedLine(sc.Bytes(), format)
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

func hostFromURLLine(line string) string {
	if k := strings.Index(line, "://"); k >= 0 {
		line = line[k+3:]
	}
	return strings.ToLower(strings.TrimSuffix(hostOf(line), "."))
}

func hostFromHostsLine(line string) string {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return ""
	}
	return strings.ToLower(fields[1])
}

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
