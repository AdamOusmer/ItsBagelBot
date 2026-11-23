// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"cmp"
	"context"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/channelevents"

	"go.uber.org/zap"
)

// sessionEvent is built before the handler returns because the context is pooled; ID is empty when nothing should publish.
func sessionEvent(c *module.Context, state channelevents.SessionState) channelevents.Event {
	event, _ := newFeedEvent(c, channelevents.Session)
	event.Change = &channelevents.SessionChange{State: state}
	return event
}

// publishSession stamps the Helix counts when they are known; a failed lookup publishes without them.
func publishSession(ctx context.Context, d engine.Deps, event channelevents.Event) error {
	if d.Pub == nil || event.ID == "" {
		return nil
	}
	if d.ChannelCounts != nil {
		counts, err := d.ChannelCounts.Lookup(ctx, event.ChannelID)
		if err != nil {
			cmp.Or(d.Log, zap.NewNop()).Warn("session counts unavailable", zap.String("channel_id", event.ChannelID), zap.Error(err))
		}
		stampCounts(&event, counts)
	}
	return engine.PublishChannelEvent(ctx, d.Pub, event)
}

func stampCounts(event *channelevents.Event, counts engine.ChannelCountsResult) {
	if counts.FollowersOK {
		event.Change.FollowerCount = int64(counts.Followers)
	}
	if counts.SubsOK {
		event.Change.SubCount = int64(counts.Subs)
	}
}
