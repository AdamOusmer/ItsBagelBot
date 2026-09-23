// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package gh implements ports.GitHub with go-github, authenticated as the
// deployer's GitHub App installation (ghinstallation).
package gh

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/google/go-github/v92/github"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

const (
	// apiTimeout caps one REST call. GitHub ends any request it has worked
	// on for 10 s server side, so 30 s only ever cuts a stalled connection,
	// never a slow but live answer.
	apiTimeout = 30 * time.Second
	// logTimeout caps a job log download, a plain storage read of a few MB
	// rather than an API call.
	logTimeout = time.Minute
	// cacheTTL bounds how stale OpenPRs and Checks may be. The Deploys page
	// asks for a plan on every load and each open PR costs three calls (PR,
	// check runs, statuses); 10 s keeps a refresh-happy owner far inside the
	// installation's 5000 requests an hour while the train's 5 s polls see
	// an answer at most two polls old.
	cacheTTL = 10 * time.Second
	// fanout bounds parallel per-PR and per-blob reads. GitHub's secondary
	// limit allows 100 concurrent requests; 8 stays far below it while a
	// 20-PR plan (60 calls, a few seconds sequential at usual API latency)
	// finishes well inside the 20 s RPC timeout.
	fanout = 8
)

// Config is the App identity plus the repository settings.
type Config struct {
	AppID          int64
	InstallationID int64
	PrivateKey     []byte
	Deploy         ports.Config
	// Transport is the base transport under the installation token; nil
	// means http.DefaultTransport.
	Transport http.RoundTripper
	// BaseURL is the REST API root; empty means https://api.github.com/.
	// Tests point it at an httptest server.
	BaseURL string
}

// Client implements ports.GitHub.
type Client struct {
	cfg   Config
	gh    *github.Client
	owner string
	repo  string
	// raw fetches pre-signed log URLs without the installation token.
	raw *http.Client
	now func() time.Time

	openPRs  *ttlCache[ports.Branch, []deploy.PRInfo]
	checks   *ttlCache[deploy.SHA, ports.CheckSummary]
	required *ttlCache[ports.Branch, requirement]
	// blobs is content addressed: a sha names its bytes forever, so entries
	// never go stale, and the set only grows by the manifests a deploy
	// changes until the deployer rolls itself at the end of that deploy.
	blobs sync.Map
}

var _ ports.GitHub = (*Client)(nil)

// New builds the installation-authenticated client.
func New(cfg Config) (*Client, error) {
	base := cfg.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	itr, err := ghinstallation.New(base, cfg.AppID, cfg.InstallationID, cfg.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("github app credentials: %w", err)
	}
	opts := []github.ClientOptionsFunc{github.WithTransport(itr), github.WithTimeout(apiTimeout)}
	if cfg.BaseURL != "" {
		itr.BaseURL = strings.TrimSuffix(cfg.BaseURL, "/")
		opts = append(opts, github.WithURLs(&cfg.BaseURL, &cfg.BaseURL))
	}
	client, err := github.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("github client: %w", err)
	}
	return &Client{
		cfg:      cfg,
		gh:       client,
		owner:    cfg.Deploy.Owner,
		repo:     cfg.Deploy.Repo,
		raw:      &http.Client{Transport: base, Timeout: logTimeout},
		now:      time.Now,
		openPRs:  newCache[ports.Branch, []deploy.PRInfo](cacheTTL),
		checks:   newCache[deploy.SHA, ports.CheckSummary](cacheTTL),
		required: newCache[ports.Branch, requirement](cacheTTL),
	}, nil
}

// sentinels maps the status codes stages branch on onto the ports
// sentinels. 405 is GitHub's "not mergeable" and 409 its "head moved" on a
// merge: both mean the PR must be re-read before trying again.
var sentinels = map[int]error{
	http.StatusNotFound:         ports.ErrNotFound,
	http.StatusMethodNotAllowed: ports.ErrConflict,
	http.StatusConflict:         ports.ErrConflict,
}

func statusOf(resp *github.Response) int {
	if resp == nil || resp.Response == nil {
		return 0
	}
	return resp.StatusCode
}

// apiErr wraps err with the sentinel for its status. go-github's error
// already names the method, URL and GitHub's message, so no context is added.
func apiErr(resp *github.Response, err error) error {
	if err == nil {
		return nil
	}
	if sentinel, ok := sentinels[statusOf(resp)]; ok {
		return fmt.Errorf("%w: %w", sentinel, err)
	}
	return err
}

// unprocessable maps a 422 to sentinel. GitHub answers 422 for "reference
// already exists", "reference does not exist" and "a pull request already
// exists", which mean different things per call site.
func unprocessable(resp *github.Response, err, sentinel error) error {
	if statusOf(resp) == http.StatusUnprocessableEntity {
		return fmt.Errorf("%w: %w", sentinel, err)
	}
	return apiErr(resp, err)
}

// found splits a lookup's outcome into absent (404, no error), present, and
// a real failure.
func found(resp *github.Response, err error) (bool, error) {
	if statusOf(resp) == http.StatusNotFound {
		return false, nil
	}
	return err == nil, apiErr(resp, err)
}

// accepted reports GitHub's 202 "scheduled in the background", which
// go-github surfaces as an *AcceptedError.
func accepted(err error) bool {
	var a *github.AcceptedError
	return errors.As(err, &a)
}

// collect walks every page of a list call. fetch reads list.Page, which
// collect advances until GitHub stops sending a next page.
func collect[T any](list *github.ListOptions, fetch func() ([]T, *github.Response, error)) ([]T, error) {
	var all []T
	for list.Page = 1; list.Page != 0; {
		items, resp, err := fetch()
		if err != nil {
			return nil, apiErr(resp, err)
		}
		all = append(all, items...)
		list.Page = resp.NextPage
	}
	return all, nil
}

func mapAll[S, T any](in []S, f func(S) T) []T {
	out := make([]T, len(in))
	for i, v := range in {
		out[i] = f(v)
	}
	return out
}

// ttlCache holds values for ttl. Expired entries are dropped on put, so the
// per-sha check cache never holds more than one TTL window's worth.
type ttlCache[K comparable, V any] struct {
	mu    sync.Mutex
	ttl   time.Duration
	items map[K]cached[V]
}

type cached[V any] struct {
	val V
	at  time.Time
}

func newCache[K comparable, V any](ttl time.Duration) *ttlCache[K, V] {
	return &ttlCache[K, V]{ttl: ttl, items: map[K]cached[V]{}}
}

// load returns the cached value for key, or calls fetch and caches a
// successful result. Concurrent misses each fetch: a duplicate read is
// cheaper than a singleflight sharing one caller's cancelled context.
func (c *ttlCache[K, V]) load(key K, now time.Time, fetch func() (V, error)) (V, error) {
	if v, ok := c.get(key, now); ok {
		return v, nil
	}
	v, err := fetch()
	if err == nil {
		c.put(key, v, now)
	}
	return v, err
}

func (c *ttlCache[K, V]) get(key K, now time.Time) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[key]
	if !ok || now.Sub(e.at) >= c.ttl {
		var zero V
		return zero, false
	}
	return e.val, true
}

func (c *ttlCache[K, V]) put(key K, val V, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, e := range c.items {
		if now.Sub(e.at) >= c.ttl {
			delete(c.items, k)
		}
	}
	c.items[key] = cached[V]{val: val, at: now}
}
