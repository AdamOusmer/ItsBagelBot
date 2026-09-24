// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package twitch

import (
	"ItsBagelBot/pkg/codec"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
)

var ErrMissingScope = errors.New("broadcaster grant missing channel:manage:redemptions scope")

var ErrDuplicateReward = errors.New("a reward with this title already exists on the channel")

var duplicateRewardMarker = []byte("DUPLICATE_REWARD")

type CustomReward struct {
	ID                         string
	Title                      string
	Cost                       int
	Prompt                     string
	IsEnabled                  bool
	IsPaused                   bool
	BackgroundColor            string
	IsUserInputRequired        bool
	ShouldSkipQueue            bool
	MaxPerStreamEnabled        bool
	MaxPerStream               int
	MaxPerUserPerStreamEnabled bool
	MaxPerUserPerStream        int
	GlobalCooldownEnabled      bool
	GlobalCooldownSeconds      int
}

type helixReward struct {
	ID                                string `json:"id"`
	Title                             string `json:"title"`
	Cost                              int    `json:"cost"`
	Prompt                            string `json:"prompt"`
	IsEnabled                         bool   `json:"is_enabled"`
	IsPaused                          bool   `json:"is_paused"`
	IsUserInputRequired               bool   `json:"is_user_input_required"`
	ShouldRedemptionsSkipRequestQueue bool   `json:"should_redemptions_skip_request_queue"`
	BackgroundColor                   string `json:"background_color"`
	MaxPerStreamSetting               struct {
		IsEnabled    bool `json:"is_enabled"`
		MaxPerStream int  `json:"max_per_stream"`
	} `json:"max_per_stream_setting"`
	MaxPerUserPerStreamSetting struct {
		IsEnabled           bool `json:"is_enabled"`
		MaxPerUserPerStream int  `json:"max_per_user_per_stream"`
	} `json:"max_per_user_per_stream_setting"`
	GlobalCooldownSetting struct {
		IsEnabled             bool `json:"is_enabled"`
		GlobalCooldownSeconds int  `json:"global_cooldown_seconds"`
	} `json:"global_cooldown_setting"`
}

func (h helixReward) toCustomReward() CustomReward {
	return CustomReward{
		ID:                         h.ID,
		Title:                      h.Title,
		Cost:                       h.Cost,
		Prompt:                     h.Prompt,
		IsEnabled:                  h.IsEnabled,
		IsPaused:                   h.IsPaused,
		BackgroundColor:            h.BackgroundColor,
		IsUserInputRequired:        h.IsUserInputRequired,
		ShouldSkipQueue:            h.ShouldRedemptionsSkipRequestQueue,
		MaxPerStreamEnabled:        h.MaxPerStreamSetting.IsEnabled,
		MaxPerStream:               h.MaxPerStreamSetting.MaxPerStream,
		MaxPerUserPerStreamEnabled: h.MaxPerUserPerStreamSetting.IsEnabled,
		MaxPerUserPerStream:        h.MaxPerUserPerStreamSetting.MaxPerUserPerStream,
		GlobalCooldownEnabled:      h.GlobalCooldownSetting.IsEnabled,
		GlobalCooldownSeconds:      h.GlobalCooldownSetting.GlobalCooldownSeconds,
	}
}

type customRewardBody struct {
	Title                             string `json:"title"`
	Cost                              int    `json:"cost"`
	Prompt                            string `json:"prompt"`
	IsEnabled                         bool   `json:"is_enabled"`
	BackgroundColor                   string `json:"background_color,omitempty"`
	IsUserInputRequired               bool   `json:"is_user_input_required"`
	IsPaused                          bool   `json:"is_paused"`
	ShouldRedemptionsSkipRequestQueue bool   `json:"should_redemptions_skip_request_queue"`
	IsMaxPerStreamEnabled             bool   `json:"is_max_per_stream_enabled"`
	MaxPerStream                      int    `json:"max_per_stream"`
	IsMaxPerUserPerStreamEnabled      bool   `json:"is_max_per_user_per_stream_enabled"`
	MaxPerUserPerStream               int    `json:"max_per_user_per_stream"`
	IsGlobalCooldownEnabled           bool   `json:"is_global_cooldown_enabled"`
	GlobalCooldownSeconds             int    `json:"global_cooldown_seconds"`
}

func rewardBody(r CustomReward) customRewardBody {
	return customRewardBody{
		Title:                             r.Title,
		Cost:                              r.Cost,
		Prompt:                            r.Prompt,
		IsEnabled:                         r.IsEnabled,
		BackgroundColor:                   r.BackgroundColor,
		IsUserInputRequired:               r.IsUserInputRequired,
		IsPaused:                          r.IsPaused,
		ShouldRedemptionsSkipRequestQueue: r.ShouldSkipQueue,
		IsMaxPerStreamEnabled:             r.MaxPerStreamEnabled,
		MaxPerStream:                      atLeast1(r.MaxPerStream),
		IsMaxPerUserPerStreamEnabled:      r.MaxPerUserPerStreamEnabled,
		MaxPerUserPerStream:               atLeast1(r.MaxPerUserPerStream),
		IsGlobalCooldownEnabled:           r.GlobalCooldownEnabled,
		GlobalCooldownSeconds:             atLeast1(r.GlobalCooldownSeconds),
	}
}

func atLeast1(v int) int {
	if v < 1 {
		return 1
	}
	return v
}

const customRewardsPath = "/helix/channel_points/custom_rewards"

func (c *Client) ListCustomRewards(ctx context.Context, broadcasterID string) ([]CustomReward, error) {
	endpoint := customRewardsPath + "?broadcaster_id=" + url.QueryEscape(broadcasterID) + "&only_manageable_rewards=true"
	page, err := c.rewardCall(ctx, broadcasterID, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	out := make([]CustomReward, 0, len(page))
	for _, h := range page {
		out = append(out, h.toCustomReward())
	}
	return out, nil
}

func (c *Client) CreateCustomReward(ctx context.Context, broadcasterID string, r CustomReward) (CustomReward, error) {
	body, _ := codec.Marshal(rewardBody(r))
	endpoint := customRewardsPath + "?broadcaster_id=" + url.QueryEscape(broadcasterID)
	return c.rewardMutate(ctx, broadcasterID, http.MethodPost, endpoint, body)
}

func (c *Client) UpdateCustomReward(ctx context.Context, broadcasterID, rewardID string, r CustomReward) (CustomReward, error) {
	body, _ := codec.Marshal(rewardBody(r))
	endpoint := customRewardsPath + "?broadcaster_id=" + url.QueryEscape(broadcasterID) + "&id=" + url.QueryEscape(rewardID)
	return c.rewardMutate(ctx, broadcasterID, http.MethodPatch, endpoint, body)
}

func (c *Client) DeleteCustomReward(ctx context.Context, broadcasterID, rewardID string) error {
	endpoint := customRewardsPath + "?broadcaster_id=" + url.QueryEscape(broadcasterID) + "&id=" + url.QueryEscape(rewardID)
	res, err := c.ExecuteAs(ctx, IdentityBroadcaster, broadcasterID, HelixCall{Method: http.MethodDelete, Endpoint: endpoint})
	if err != nil {
		return err
	}
	defer drain(res)
	if res.StatusCode == http.StatusNoContent || res.StatusCode == http.StatusNotFound {
		return nil
	}
	return rewardStatusError(res, "custom reward delete")
}

func (c *Client) UpdateRedemptionStatus(ctx context.Context, broadcasterID, rewardID, redemptionID, status string) error {
	endpoint := "/helix/channel_points/custom_rewards/redemptions?broadcaster_id=" + url.QueryEscape(broadcasterID) +
		"&reward_id=" + url.QueryEscape(rewardID) + "&id=" + url.QueryEscape(redemptionID)
	body, _ := codec.Marshal(struct {
		Status string `json:"status"`
	}{status})
	res, err := c.ExecuteAs(ctx, IdentityBroadcaster, broadcasterID, HelixCall{Method: http.MethodPatch, Endpoint: endpoint, Body: body})
	if err != nil {
		return err
	}
	defer drain(res)
	if res.StatusCode == http.StatusOK {
		return nil
	}
	return rewardStatusError(res, "redemption update")
}

func (c *Client) rewardMutate(ctx context.Context, broadcasterID, method, endpoint string, body []byte) (CustomReward, error) {
	page, err := c.rewardCall(ctx, broadcasterID, method, endpoint, body)
	if err != nil {
		return CustomReward{}, err
	}
	if len(page) == 0 {
		return CustomReward{}, errors.New("twitch returned no reward")
	}
	return page[0].toCustomReward(), nil
}

func (c *Client) rewardCall(ctx context.Context, broadcasterID, method, endpoint string, body []byte) ([]helixReward, error) {
	res, err := c.ExecuteAs(ctx, IdentityBroadcaster, broadcasterID, HelixCall{Method: method, Endpoint: endpoint, Body: body})
	if err != nil {
		return nil, err
	}
	defer drain(res)

	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		return nil, rewardStatusError(res, method+" "+customRewardsPath)
	}

	var payload struct {
		Data []helixReward `json:"data"`
	}
	if err := codec.NewDecoder(res.Body).Decode(&payload); err != nil {
		return nil, err
	}
	return payload.Data, nil
}

func rewardStatusError(res *http.Response, op string) error {
	body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
	if res.StatusCode == http.StatusUnauthorized && isMissingScope(body) {
		return ErrMissingScope
	}
	if res.StatusCode == http.StatusBadRequest && bytes.Contains(body, duplicateRewardMarker) {
		return ErrDuplicateReward
	}
	return &StatusError{Status: res.StatusCode, Op: op, Body: string(body)}
}
