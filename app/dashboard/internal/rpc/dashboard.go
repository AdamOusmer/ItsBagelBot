package rpc

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"golang.org/x/sync/singleflight"
)

// ---------------------------------------------------------------------------
// Embedded TTL cache (bool-only, no sharding — the dashboard key space is
// small). Mirrors the jitter + sweeper pattern from pkg/cache without the
// cross-module import.
// ---------------------------------------------------------------------------

type cacheEntry struct {
	value     bool
	expiresAt int64 // unix nanoseconds
}

type grantCache struct {
	mu      sync.RWMutex
	entries map[string]cacheEntry
	group   singleflight.Group
	ttl     time.Duration
	jitter  time.Duration
	stop    chan struct{}
}

func newGrantCache(ttl time.Duration) *grantCache {
	c := &grantCache{
		entries: make(map[string]cacheEntry),
		ttl:     ttl,
		jitter:  ttl / 10,
		stop:    make(chan struct{}),
	}
	go c.sweep()
	return c
}

func (c *grantCache) get(key string) (bool, bool) {
	now := time.Now().UnixNano()

	c.mu.RLock()
	e, ok := c.entries[key]
	c.mu.RUnlock()

	if !ok || now > e.expiresAt {
		return false, false
	}
	return e.value, true
}

func (c *grantCache) set(key string, value bool) {
	expiresAt := time.Now().Add(c.ttl + rand.N(c.jitter+1)).UnixNano()

	c.mu.Lock()
	c.entries[key] = cacheEntry{value: value, expiresAt: expiresAt}
	c.mu.Unlock()
}

func (c *grantCache) invalidate(key string) {
	c.group.Forget(key)

	c.mu.Lock()
	delete(c.entries, key)
	c.mu.Unlock()
}

func (c *grantCache) close() {
	close(c.stop)
}

func (c *grantCache) sweep() {
	ticker := time.NewTicker(c.ttl)
	defer ticker.Stop()

	for {
		select {
		case <-c.stop:
			return
		case <-ticker.C:
		}

		now := time.Now().UnixNano()

		c.mu.Lock()
		for key, e := range c.entries {
			if now > e.expiresAt {
				delete(c.entries, key)
			}
		}
		c.mu.Unlock()
	}
}

// ---------------------------------------------------------------------------
// Dashboard RPC client
// ---------------------------------------------------------------------------

// Dashboard talks to the broadcaster-data service over NATS request-reply and
// keeps a local cache of grant lookups with NATS-driven invalidation.
type Dashboard struct {
	nc     *nats.Conn
	prefix string // e.g. "bagel.rpc.dashboard"
	cache  *grantCache
	sub    *nats.Subscription // invalidation lane subscription
}

// NewDashboard creates a Dashboard RPC client. It subscribes to
// invalidationSubject (without a queue group — every replica must invalidate
// its own cache) and starts a background cache sweeper.
func NewDashboard(nc *nats.Conn, prefix string, invalidationSubject string) (*Dashboard, error) {
	d := &Dashboard{
		nc:     nc,
		prefix: prefix,
		cache:  newGrantCache(5 * time.Minute),
	}

	sub, err := nc.Subscribe(invalidationSubject, func(msg *nats.Msg) {
		var payload struct {
			BroadcasterID string `json:"broadcaster_id"`
		}
		if json.Unmarshal(msg.Data, &payload) == nil && payload.BroadcasterID != "" {
			d.cache.invalidate(payload.BroadcasterID)
		}
	})
	if err != nil {
		d.cache.close()
		return nil, fmt.Errorf("subscribe to invalidation lane: %w", err)
	}
	d.sub = sub

	return d, nil
}

// UpsertUser sends the user record to the broadcaster-data service.
func (d *Dashboard) UpsertUser(ctx context.Context, userID, login, displayName string) error {
	req, _ := json.Marshal(map[string]string{
		"user_id":      userID,
		"username":     login,
		"display_name": displayName,
	})

	msg, err := d.nc.RequestWithContext(ctx, d.prefix+".upsert_user", req)
	if err != nil {
		return fmt.Errorf("upsert_user rpc: %w", err)
	}
	return checkReplyError(msg.Data)
}

// SaveBotGrant persists a broadcaster's bot grant via the data service and
// invalidates the local cache so the next HasBotGrant sees the new state.
func (d *Dashboard) SaveBotGrant(ctx context.Context, broadcasterID, scopes string, refreshTokenEnc []byte) error {
	req, _ := json.Marshal(map[string]string{
		"broadcaster_user_id": broadcasterID,
		"scopes":              scopes,
		"refresh_token_enc":   base64.StdEncoding.EncodeToString(refreshTokenEnc),
	})

	msg, err := d.nc.RequestWithContext(ctx, d.prefix+".grant_save", req)
	if err != nil {
		return fmt.Errorf("grant_save rpc: %w", err)
	}
	if err := checkReplyError(msg.Data); err != nil {
		return err
	}
	d.cache.invalidate(broadcasterID)
	return nil
}

// HasBotGrant checks whether a broadcaster has granted bot access. Results are
// cached and concurrent misses on the same key are collapsed via singleflight.
func (d *Dashboard) HasBotGrant(ctx context.Context, broadcasterID string) (bool, error) {
	if v, ok := d.cache.get(broadcasterID); ok {
		return v, nil
	}

	result, err, _ := d.cache.group.Do(broadcasterID, func() (any, error) {
		// Re-check after acquiring the flight — a prior caller may have filled it.
		if v, ok := d.cache.get(broadcasterID); ok {
			return v, nil
		}

		req, _ := json.Marshal(map[string]string{
			"broadcaster_user_id": broadcasterID,
		})

		msg, err := d.nc.RequestWithContext(ctx, d.prefix+".grant_has", req)
		if err != nil {
			return false, fmt.Errorf("grant_has rpc: %w", err)
		}

		var reply struct {
			HasGrant bool   `json:"has_grant"`
			Error    string `json:"error"`
		}
		if err := json.Unmarshal(msg.Data, &reply); err != nil {
			return false, fmt.Errorf("grant_has unmarshal: %w", err)
		}
		if reply.Error != "" {
			return false, fmt.Errorf("grant_has: %s", reply.Error)
		}

		d.cache.set(broadcasterID, reply.HasGrant)
		return reply.HasGrant, nil
	})
	if err != nil {
		return false, err
	}
	return result.(bool), nil
}

// Close unsubscribes from the invalidation lane and stops the cache sweeper.
func (d *Dashboard) Close() error {
	if d.sub != nil {
		_ = d.sub.Unsubscribe()
	}
	d.cache.close()
	return nil
}

// checkReplyError parses a NATS reply and returns an error if the payload
// contains an "error" field.
func checkReplyError(data []byte) error {
	var reply struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(data, &reply) == nil && reply.Error != "" {
		return fmt.Errorf("rpc error: %s", reply.Error)
	}
	return nil
}
