// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package projection

import (
	"context"

	"github.com/newrelic/go-agent/v3/newrelic"
)

// LoadChannel optionally overlaps independent user and module cache misses.
// Cached and single-miss loads stay on the caller goroutine. Each cache keeps
// its own singleflight and invalidation. Module errors propagate; User retains
// its standard fallback. Returned module maps must remain read-only.
func (c *Client) LoadChannel(ctx context.Context, userID uint64, needModules bool) (map[string]ModuleView, User, error) {
	if !needModules {
		return nil, c.channelUser(ctx, userID), nil
	}
	user, userCached := c.users.Get(key("user", userID))
	mods, modulesCached := c.modules.Get(key("modules", userID))
	recordCachedSources(ctx, userCached, modulesCached)
	switch {
	case userCached && modulesCached:
		return mods, user, nil
	case userCached:
		mods, err := c.Modules(ctx, userID)
		return mods, user, err
	case modulesCached:
		return mods, c.userOrStandard(ctx, userID), nil
	default:
		return c.loadColdChannel(ctx, userID)
	}
}

func (c *Client) channelUser(ctx context.Context, userID uint64) User {
	if user, ok := c.users.Get(key("user", userID)); ok {
		loadSource(ctx, "projection.user.source", "local")
		return user
	}
	return c.userOrStandard(ctx, userID)
}

func (c *Client) userOrStandard(ctx context.Context, userID uint64) User {
	user, err := c.User(ctx, userID)
	if err != nil {
		return User{Status: "standard"}
	}
	return user
}

func (c *Client) loadColdChannel(ctx context.Context, userID uint64) (map[string]ModuleView, User, error) {
	// Each goroutine needs a separate New Relic transaction reference while
	// preserving the caller's cancellation and deadline.
	userCtx := ctx
	if txn := newrelic.FromContext(ctx); txn != nil {
		userCtx = newrelic.NewContext(ctx, txn.NewGoroutine())
	}
	done := make(chan User, 1)
	go func() {
		done <- c.userOrStandard(userCtx, userID)
	}()
	mods, err := c.Modules(ctx, userID)
	return mods, <-done, err
}

func recordCachedSources(ctx context.Context, userCached, modulesCached bool) {
	if userCached {
		loadSource(ctx, "projection.user.source", "local")
	}
	if modulesCached {
		loadSource(ctx, "projection.modules.source", "local")
	}
}

// Attribute names and values are fixed and low cardinality; IDs and command
// names never enter load-source telemetry.
func loadSource(ctx context.Context, attribute, source string) {
	if txn := newrelic.FromContext(ctx); txn != nil {
		txn.AddAttribute(attribute, source)
	}
}
