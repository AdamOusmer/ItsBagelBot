// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package twitch

import (
	"context"
	"net/http"
	"net/url"
)

type PollChoice struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Votes int    `json:"votes"`
}

type Poll struct {
	ID      string       `json:"id"`
	Title   string       `json:"title"`
	Status  string       `json:"status"`
	Choices []PollChoice `json:"choices"`
}

type PredictionOutcome struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	Users         int    `json:"users"`
	ChannelPoints int    `json:"channel_points"`
}

type Prediction struct {
	ID       string              `json:"id"`
	Title    string              `json:"title"`
	Status   string              `json:"status"`
	Outcomes []PredictionOutcome `json:"outcomes"`
}

type titled struct {
	Title string `json:"title"`
}

func titles(items []string) []titled {
	out := make([]titled, len(items))
	for i, item := range items {
		out[i] = titled{Title: item}
	}
	return out
}

type CreateOptions struct {
	Title   string
	Items   []string
	Seconds int
}
type PollResolution struct{ ID, Status string }
type PredictionResolution struct{ ID, Status, WinningOutcomeID string }

func (c *Client) CreatePoll(ctx context.Context, broadcasterID string, options CreateOptions) error {
	return c.sendJSON(ctx, broadcasterTarget(broadcasterID), HelixCall{Method: http.MethodPost, Endpoint: "/helix/polls"}, map[string]any{
		"broadcaster_id": broadcasterID,
		"title":          options.Title,
		"choices":        titles(options.Items),
		"duration":       options.Seconds,
	})
}

func (c *Client) LatestPoll(ctx context.Context, broadcasterID string) (Poll, bool, error) {
	return readFirst[Poll](ctx, c, helixRead{broadcasterTarget(broadcasterID), latestQuery("/helix/polls", broadcasterID)})
}

func (c *Client) EndPoll(ctx context.Context, broadcasterID string, resolution PollResolution) error {
	return c.sendJSON(ctx, broadcasterTarget(broadcasterID), HelixCall{Method: http.MethodPatch, Endpoint: "/helix/polls"}, map[string]any{
		"broadcaster_id": broadcasterID,
		"id":             resolution.ID,
		"status":         resolution.Status,
	})
}

func (c *Client) CreatePrediction(ctx context.Context, broadcasterID string, options CreateOptions) error {
	return c.sendJSON(ctx, broadcasterTarget(broadcasterID), HelixCall{Method: http.MethodPost, Endpoint: "/helix/predictions"}, map[string]any{
		"broadcaster_id":    broadcasterID,
		"title":             options.Title,
		"outcomes":          titles(options.Items),
		"prediction_window": options.Seconds,
	})
}

func (c *Client) LatestPrediction(ctx context.Context, broadcasterID string) (Prediction, bool, error) {
	return readFirst[Prediction](ctx, c, helixRead{broadcasterTarget(broadcasterID), latestQuery("/helix/predictions", broadcasterID)})
}

func (c *Client) EndPrediction(ctx context.Context, broadcasterID string, resolution PredictionResolution) error {
	body := map[string]any{
		"broadcaster_id": broadcasterID,
		"id":             resolution.ID,
		"status":         resolution.Status,
	}
	if resolution.WinningOutcomeID != "" {
		body["winning_outcome_id"] = resolution.WinningOutcomeID
	}
	return c.sendJSON(ctx, broadcasterTarget(broadcasterID), HelixCall{Method: http.MethodPatch, Endpoint: "/helix/predictions"}, body)
}

func latestQuery(path, broadcasterID string) string {
	q := url.Values{}
	q.Set("broadcaster_id", broadcasterID)
	q.Set("first", "1")
	return path + "?" + q.Encode()
}
