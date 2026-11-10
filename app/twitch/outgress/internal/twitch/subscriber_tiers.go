// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package twitch

import (
	"context"
	"net/url"
	"slices"
)

const SubscriptionsPath = "/helix/subscriptions"

const subscriberTiersBatch = 100

// SubscriberTiers maps each subscribed user to its tier ("1000", "2000",
// "3000"); non-subscribers are absent. Batches already read survive an error.
func (c *Client) SubscriberTiers(ctx context.Context, broadcasterID string, userIDs []string) (map[string]string, error) {
	tiers := make(map[string]string)
	for batch := range slices.Chunk(userIDs, subscriberTiersBatch) {
		if err := c.subscriberTierBatch(ctx, broadcasterID, batch, tiers); err != nil {
			return tiers, err
		}
	}
	return tiers, nil
}

func (c *Client) subscriberTierBatch(ctx context.Context, broadcasterID string, userIDs []string, into map[string]string) error {
	q := url.Values{"broadcaster_id": {broadcasterID}, "user_id": userIDs}
	subs, err := readData[struct {
		UserID string `json:"user_id"`
		Tier   string `json:"tier"`
	}](ctx, c, helixRead{broadcasterTarget(broadcasterID), SubscriptionsPath + "?" + q.Encode()})
	if err != nil {
		return err
	}
	for _, sub := range subs {
		into[sub.UserID] = sub.Tier
	}
	return nil
}
