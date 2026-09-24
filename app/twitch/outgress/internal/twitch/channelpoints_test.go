// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package twitch

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestRewardStatusErrorClassifiesHelixRefusals(t *testing.T) {
	cases := map[string]struct {
		status int
		body   string
		want   error
	}{
		"duplicate create": {http.StatusBadRequest, `{"error":"Bad Request","status":400,"message":"CREATE_CUSTOM_REWARD_DUPLICATE_REWARD"}`, ErrDuplicateReward},
		"duplicate update": {http.StatusBadRequest, `{"error":"Bad Request","status":400,"message":"UPDATE_CUSTOM_REWARD_DUPLICATE_REWARD"}`, ErrDuplicateReward},
		"missing scope":    {http.StatusUnauthorized, `{"error":"Unauthorized","status":401,"message":"Missing scope: channel:manage:redemptions"}`, ErrMissingScope},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			res := &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body))}
			if err := rewardStatusError(res, "POST custom_rewards"); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestRewardStatusErrorKeepsOtherBadRequestsGeneric(t *testing.T) {
	body := `{"error":"Bad Request","status":400,"message":"cost must be greater than 0"}`
	res := &http.Response{StatusCode: http.StatusBadRequest, Body: io.NopCloser(strings.NewReader(body))}
	var se *StatusError
	if err := rewardStatusError(res, "POST custom_rewards"); !errors.As(err, &se) || se.Status != http.StatusBadRequest {
		t.Fatalf("got %v, want a 400 StatusError", err)
	}
}
