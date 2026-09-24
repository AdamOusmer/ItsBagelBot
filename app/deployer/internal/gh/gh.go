// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

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
	apiTimeout = 30 * time.Second
	logTimeout = time.Minute
	cacheTTL   = 10 * time.Second
	fanout     = 8
)

type Config struct {
	AppID          int64
	InstallationID int64
	PrivateKey     []byte
	Deploy         ports.Config
	Transport      http.RoundTripper
	BaseURL        string
}

type Client struct {
	cfg   Config
	gh    *github.Client
	owner string
	repo  string
	raw   *http.Client
	now   func() time.Time

	openPRs  *ttlCache[ports.Branch, []deploy.PRInfo]
	checks   *ttlCache[deploy.SHA, ports.CheckSummary]
	required *ttlCache[ports.Branch, requirement]
	blobs    sync.Map
}

var _ ports.GitHub = (*Client)(nil)

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

func apiErr(resp *github.Response, err error) error {
	if err == nil {
		return nil
	}
	if sentinel, ok := sentinels[statusOf(resp)]; ok {
		return fmt.Errorf("%w: %w", sentinel, err)
	}
	return err
}

func unprocessable(resp *github.Response, err, sentinel error) error {
	if statusOf(resp) == http.StatusUnprocessableEntity {
		return fmt.Errorf("%w: %w", sentinel, err)
	}
	return apiErr(resp, err)
}

func found(resp *github.Response, err error) (bool, error) {
	if statusOf(resp) == http.StatusNotFound {
		return false, nil
	}
	return err == nil, apiErr(resp, err)
}

func accepted(err error) bool {
	var a *github.AcceptedError
	return errors.As(err, &a)
}

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
