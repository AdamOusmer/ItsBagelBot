// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package twitch

import (
	"ItsBagelBot/pkg/codec"
	"context"
	"errors"
	"net/http"
	"net/url"
)

var (
	ErrRewardNotManageable = errors.New("reward was not created by this client")
	ErrRewardNotFound      = errors.New("reward no longer exists")
)

// Fields left nil stay untouched on Twitch; CustomReward's full body would overwrite title, prompt and limits.
type RewardPatch struct {
	IsEnabled *bool `json:"is_enabled,omitempty"`
	Cost      *int  `json:"cost,omitempty"`
}

func (c *Client) GetManageableReward(ctx context.Context, broadcasterID, rewardID string) (CustomReward, error) {
	endpoint := rewardEndpoint(broadcasterID, rewardID) + "&only_manageable_rewards=true"
	page, err := c.rewardCall(ctx, broadcasterID, http.MethodGet, endpoint, nil)
	if err != nil {
		return CustomReward{}, rewardRefusal(err)
	}
	if len(page) == 0 {
		return CustomReward{}, ErrRewardNotManageable
	}
	return page[0].toCustomReward(), nil
}

func (c *Client) PatchCustomReward(ctx context.Context, broadcasterID, rewardID string, patch RewardPatch) error {
	body, err := codec.Marshal(patch)
	if err != nil {
		return err
	}
	_, err = c.rewardCall(ctx, broadcasterID, http.MethodPatch, rewardEndpoint(broadcasterID, rewardID), body)
	return rewardRefusal(err)
}

func rewardEndpoint(broadcasterID, rewardID string) string {
	return customRewardsPath + "?broadcaster_id=" + url.QueryEscape(broadcasterID) + "&id=" + url.QueryEscape(rewardID)
}

func rewardRefusal(err error) error {
	var status *StatusError
	if !errors.As(err, &status) {
		return err
	}
	switch status.Status {
	case http.StatusForbidden:
		return ErrRewardNotManageable
	case http.StatusNotFound:
		return ErrRewardNotFound
	default:
		return err
	}
}
