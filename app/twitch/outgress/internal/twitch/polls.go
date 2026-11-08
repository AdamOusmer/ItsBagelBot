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

func titles(options CreateOptions) []titled {
	out := make([]titled, len(options.Items))
	for i, item := range options.Items {
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

type ContestChannel struct{ BroadcasterID string }
type ContestCreation struct {
	Channel ContestChannel
	Options CreateOptions
}
type PollUpdate struct {
	Channel    ContestChannel
	Resolution PollResolution
}
type PredictionUpdate struct {
	Channel    ContestChannel
	Resolution PredictionResolution
}

func (c *Client) CreatePoll(ctx context.Context, request ContestCreation) error {
	broadcasterID, options := request.Channel.BroadcasterID, request.Options
	return c.sendJSON(ctx, broadcasterTarget(broadcasterID), HelixCall{Method: http.MethodPost, Endpoint: "/helix/polls"}, map[string]any{
		"broadcaster_id": broadcasterID,
		"title":          options.Title,
		"choices":        titles(options),
		"duration":       options.Seconds,
	})
}

func (c *Client) LatestPoll(ctx context.Context, channel ContestChannel) (Poll, bool, error) {
	return readFirst[Poll](ctx, c, helixRead{broadcasterTarget(channel.BroadcasterID), pollResource.latestEndpoint(channel)})
}

func (c *Client) EndPoll(ctx context.Context, update PollUpdate) error {
	broadcasterID, resolution := update.Channel.BroadcasterID, update.Resolution
	return c.sendJSON(ctx, broadcasterTarget(broadcasterID), HelixCall{Method: http.MethodPatch, Endpoint: "/helix/polls"}, map[string]any{
		"broadcaster_id": broadcasterID,
		"id":             resolution.ID,
		"status":         resolution.Status,
	})
}

func (c *Client) CreatePrediction(ctx context.Context, request ContestCreation) error {
	broadcasterID, options := request.Channel.BroadcasterID, request.Options
	return c.sendJSON(ctx, broadcasterTarget(broadcasterID), HelixCall{Method: http.MethodPost, Endpoint: "/helix/predictions"}, map[string]any{
		"broadcaster_id":    broadcasterID,
		"title":             options.Title,
		"outcomes":          titles(options),
		"prediction_window": options.Seconds,
	})
}

func (c *Client) LatestPrediction(ctx context.Context, channel ContestChannel) (Prediction, bool, error) {
	return readFirst[Prediction](ctx, c, helixRead{broadcasterTarget(channel.BroadcasterID), predictionResource.latestEndpoint(channel)})
}

func (c *Client) EndPrediction(ctx context.Context, update PredictionUpdate) error {
	broadcasterID, resolution := update.Channel.BroadcasterID, update.Resolution
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

type contestResource struct{ path string }

var (
	pollResource       = contestResource{path: "/helix/polls"}
	predictionResource = contestResource{path: "/helix/predictions"}
)

func (resource contestResource) latestEndpoint(channel ContestChannel) string {
	q := url.Values{}
	q.Set("broadcaster_id", channel.BroadcasterID)
	q.Set("first", "1")
	return resource.path + "?" + q.Encode()
}
