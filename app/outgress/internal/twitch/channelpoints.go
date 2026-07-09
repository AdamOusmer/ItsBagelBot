package twitch

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
)

// ErrMissingScope marks a channel-points call Twitch rejected because the
// broadcaster's stored grant lacks channel:manage:redemptions. It is not
// retryable: the broadcaster must re-consent. Callers surface it to the
// dashboard as a reconnect prompt instead of a generic failure.
var ErrMissingScope = errors.New("broadcaster grant missing channel:manage:redemptions scope")

// CustomReward is the flat, transport-agnostic view of one Twitch custom
// channel-points reward. The Helix GET response nests the limit controls under
// *_setting objects while the create/update body flattens them; this type hides
// that split so the RPC layer works with one shape. Only rewards our own client
// id created are manageable, which is why List passes only_manageable_rewards.
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

// helixReward decodes the Helix Custom Reward object (nested settings).
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

// customRewardBody is the create/update request body: the limit controls are
// flattened into is_*_enabled + value pairs. The dashboard always posts the full
// desired state, so every field is sent (no partial PATCH), which also makes an
// update authoritative. A disabled limit still carries a value >= 1 because
// Twitch rejects a 0 for these fields even when their toggle is off.
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

// ListCustomRewards returns the rewards this client id created for the channel
// (only_manageable_rewards=true), under the broadcaster's own token.
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

// CreateCustomReward creates one reward and returns it with the Twitch-assigned
// id.
func (c *Client) CreateCustomReward(ctx context.Context, broadcasterID string, r CustomReward) (CustomReward, error) {
	body, _ := json.Marshal(rewardBody(r))
	endpoint := customRewardsPath + "?broadcaster_id=" + url.QueryEscape(broadcasterID)
	return c.rewardMutate(ctx, broadcasterID, http.MethodPost, endpoint, body)
}

// UpdateCustomReward updates one reward (a pause toggle is just is_paused here).
func (c *Client) UpdateCustomReward(ctx context.Context, broadcasterID, rewardID string, r CustomReward) (CustomReward, error) {
	body, _ := json.Marshal(rewardBody(r))
	endpoint := customRewardsPath + "?broadcaster_id=" + url.QueryEscape(broadcasterID) + "&id=" + url.QueryEscape(rewardID)
	return c.rewardMutate(ctx, broadcasterID, http.MethodPatch, endpoint, body)
}

// DeleteCustomReward removes one reward. A 404 (already gone) is treated as
// success so a re-run converges.
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

// UpdateRedemptionStatus resolves one redemption in a reward's request queue to
// FULFILLED or CANCELED (a refund). Twitch only allows updating redemptions in
// the UNFULFILLED state, so a redemption on a skip-queue reward, or one a mod
// already resolved, returns a 4xx the caller drops. Runs under the broadcaster
// token (channel:manage:redemptions).
func (c *Client) UpdateRedemptionStatus(ctx context.Context, broadcasterID, rewardID, redemptionID, status string) error {
	endpoint := "/helix/channel_points/custom_rewards/redemptions?broadcaster_id=" + url.QueryEscape(broadcasterID) +
		"&reward_id=" + url.QueryEscape(rewardID) + "&id=" + url.QueryEscape(redemptionID)
	body, _ := json.Marshal(struct {
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

// rewardMutate runs a create/update and decodes the single reward Twitch returns.
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

// rewardCall executes one custom-rewards request under the broadcaster token and
// decodes the {"data":[...]} envelope. A missing-scope 401 maps to
// ErrMissingScope; any other non-2xx maps to a StatusError.
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
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return nil, err
	}
	return payload.Data, nil
}

// rewardStatusError reads the error body once and, when a 401 names a missing
// scope, returns ErrMissingScope; otherwise a StatusError carrying the code.
func rewardStatusError(res *http.Response, op string) error {
	body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
	if res.StatusCode == http.StatusUnauthorized && isMissingScope(body) {
		return ErrMissingScope
	}
	return &StatusError{Status: res.StatusCode, Op: op, Body: string(body)}
}
