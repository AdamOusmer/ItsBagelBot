// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package youtube

import (
	"ItsBagelBot/pkg/codec"
	"context"
	"time"

	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/cache"
)

// ChatDirectory maps a broadcaster's channel id to its currently-active
// liveChatId, learned from the watcher's stream lifecycle events on
// youtube.ingress.event.stream (the same shape outgress already consumes for
// Twitch on twitch.ingress.event.stream).
//
// There is deliberately no Data API discovery fallback here: resolving a
// live chat without the lifecycle feed would burn search quota per send
// (search.list eventType=live costs 100 units) and silently paper over a
// broken feed. A cache miss drops the send loudly; fixing the feed fixes
// sends. The TTL is set comfortably above the watcher's poll cadence so an
// offline channel's entry ages out instead of pinning a dead chat id.
type ChatDirectory struct {
	cache *cache.Cache[string]
}

// DirectoryCapacity / DirectoryTTL bound one pod's copy. The working set is
// the number of simultaneously-live watched channels — small by definition,
// since every entry also costs the ingress a poll.
const (
	DirectoryCapacity = 1024
	DirectoryTTL      = 3 * time.Minute
)

func NewChatDirectory() *ChatDirectory {
	return &ChatDirectory{cache: cache.New[string](DirectoryCapacity, DirectoryTTL)}
}

// Set records the active chat for a channel (stream.online) or clears it
// (stream.offline, empty liveChatID).
func (d *ChatDirectory) Set(channelID, liveChatID string) {
	if d == nil {
		return
	}
	if liveChatID == "" {
		d.cache.Invalidate(channelID)
		return
	}
	d.cache.SetFor(channelID, liveChatID, DirectoryTTL)
}

// Get returns the cached chat id, or "" when unknown.
func (d *ChatDirectory) Get(channelID string) string {
	if d == nil {
		return ""
	}
	v, err := d.cache.GetOrLoad(context.Background(), channelID, func(context.Context) (string, error) {
		return "", errUnknownChat
	})
	if err != nil {
		return ""
	}
	return v
}

var errUnknownChat = errorString("youtube: no known live chat for channel")

type errorString string

func (e errorString) Error() string { return string(e) }

// HandleLifecycleEvent feeds the directory from the watcher's stream lifecycle
// lane (youtube.ingress.event.stream): stream.online carries the active chat,
// stream.offline clears it. Always acks (returns nil): the directory is a
// cache, and a malformed event must never poison or replay the lane — the
// same posture as the Twitch stream-lane handler.
func (d *ChatDirectory) HandleLifecycleEvent(msg *bus.Message) error {
	var event struct {
		Type              string `json:"type"`
		BroadcasterUserID string `json:"broadcaster_user_id"`
		LiveChatID        string `json:"live_chat_id"`
	}
	if err := codec.Unmarshal(msg.Payload, &event); err != nil {
		return nil
	}

	switch event.Type {
	case "stream.online":
		if event.BroadcasterUserID != "" && event.LiveChatID != "" {
			d.Set(event.BroadcasterUserID, event.LiveChatID)
		}
	case "stream.offline":
		if event.BroadcasterUserID != "" {
			d.Set(event.BroadcasterUserID, "")
		}
	}
	return nil
}
