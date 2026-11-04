// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package projection

import (
	"context"
	"time"

	"ItsBagelBot/internal/domain/automation"
	automationrpc "ItsBagelBot/internal/domain/rpc/automation"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/cache"
)

const automationsCacheCapacity int64 = 4096

type automationEntry struct {
	list  []automation.Automation
	index *automation.TriggerIndex
}

type automationLists = cache.Cache[automationEntry]

func newAutomationCache(ttl time.Duration) *automationLists {
	return cache.New[automationEntry](automationsCacheCapacity, ttl, cache.StaleOnError(ttl))
}

func (c *Client) automationsSubject() string {
	if c.subjects.Automations != "" {
		return c.subjects.Automations
	}
	return automationrpc.ProjectionAutomationsSubj
}

// The returned slice is the cached one; callers must not mutate it.
func (c *Client) Automations(ctx context.Context, userID uint64) ([]automation.Automation, error) {
	entry, err := c.automationEntry(ctx, userID)
	return entry.list, err
}

// AutomationIndex returns the triggers compiled when the channel's list entered the cache.
func (c *Client) AutomationIndex(ctx context.Context, userID uint64) (*automation.TriggerIndex, error) {
	entry, err := c.automationEntry(ctx, userID)
	return entry.index, err
}

func (c *Client) automationEntry(ctx context.Context, userID uint64) (automationEntry, error) {
	return c.automations.GetOrLoad(ctx, key("automations", userID), func(ctx context.Context) (automationEntry, error) {
		if list, projected, err := c.store.GetAutomations(ctx, userID); err == nil && projected {
			loadSource(ctx, "projection.automations.source", "valkey")
			return automationEntry{list: list, index: automation.CompileIndex(list)}, nil
		}
		reply, err := bus.RequestJSONTimeout[automationrpc.AutomationsProjectionReply](ctx, c.nc, c.automationsSubject(), projectionRequest(userID), c.rpcTimeout)
		if err != nil {
			loadSource(ctx, "projection.automations.source", "rpc_error")
			return automationEntry{}, err
		}
		loadSource(ctx, "projection.automations.source", "rpc")
		return automationEntry{list: reply.Automations, index: automation.CompileIndex(reply.Automations)}, nil
	})
}
