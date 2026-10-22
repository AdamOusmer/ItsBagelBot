// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package twitch

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"slices"

	"ItsBagelBot/pkg/codec"
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
	res, err := c.ExecuteAs(ctx, IdentityBroadcaster, broadcasterID, getCall(SubscriptionsPath+"?"+q.Encode()))
	if err != nil {
		return err
	}
	defer drain(res)
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return &StatusError{Status: res.StatusCode, Body: string(body)}
	}
	var payload struct {
		Data []struct {
			UserID string `json:"user_id"`
			Tier   string `json:"tier"`
		} `json:"data"`
	}
	if err := codec.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&payload); err != nil {
		return err
	}
	for _, sub := range payload.Data {
		into[sub.UserID] = sub.Tier
	}
	return nil
}
