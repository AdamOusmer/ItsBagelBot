// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package twitch

import (
	"ItsBagelBot/pkg/codec"
	"context"
	"net/http"
	"net/url"
	"strings"
)

type ChannelInfo struct {
	Title    string
	GameID   string
	GameName string
	Tags     []string
}

type Category struct {
	ID   string
	Name string
}

func (c *Client) ChannelInfo(ctx context.Context, broadcasterID string) (ChannelInfo, error) {
	res, err := c.request(ctx, c.app, getCall("/helix/channels?broadcaster_id="+url.QueryEscape(broadcasterID)))
	if err != nil {
		return ChannelInfo{}, err
	}
	defer drain(res)

	if res.StatusCode != http.StatusOK {
		return ChannelInfo{}, statusError(res, "get channel")
	}

	var payload struct {
		Data []struct {
			Title    string   `json:"title"`
			GameID   string   `json:"game_id"`
			GameName string   `json:"game_name"`
			Tags     []string `json:"tags"`
		} `json:"data"`
	}
	if err := codec.NewDecoder(res.Body).Decode(&payload); err != nil {
		return ChannelInfo{}, err
	}
	if len(payload.Data) == 0 {
		return ChannelInfo{}, &StatusError{Status: http.StatusNotFound, Op: "get channel", Body: "empty data"}
	}
	row := payload.Data[0]
	return ChannelInfo{Title: row.Title, GameID: row.GameID, GameName: row.GameName, Tags: row.Tags}, nil
}

func (c *Client) SearchCategory(ctx context.Context, query string) (Category, bool, error) {
	res, err := c.request(ctx, c.app, getCall("/helix/search/categories?first=1&query="+url.QueryEscape(query)))
	if err != nil {
		return Category{}, false, err
	}
	defer drain(res)

	if res.StatusCode != http.StatusOK {
		return Category{}, false, statusError(res, "search categories")
	}

	var payload struct {
		Data []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := codec.NewDecoder(res.Body).Decode(&payload); err != nil {
		return Category{}, false, err
	}
	if len(payload.Data) == 0 || payload.Data[0].ID == "" {
		return Category{}, false, nil
	}
	return Category{ID: payload.Data[0].ID, Name: payload.Data[0].Name}, true, nil
}

type ChannelPatch struct {
	Title  string   `json:"title,omitempty"`
	GameID string   `json:"game_id,omitempty"`
	Tags   []string `json:"tags,omitempty"`
}

func (c *Client) ModifyChannel(ctx context.Context, broadcasterID string, patch ChannelPatch) error {
	body, err := codec.Marshal(patch)
	if err != nil {
		return err
	}
	endpoint := "/helix/channels?broadcaster_id=" + url.QueryEscape(broadcasterID)
	res, err := c.ExecuteAs(ctx, IdentityBroadcaster, broadcasterID, HelixCall{
		Method:   http.MethodPatch,
		Endpoint: endpoint,
		Body:     body,
	})
	if err != nil {
		return err
	}
	defer drain(res)
	if res.StatusCode == http.StatusNoContent || res.StatusCode == http.StatusOK {
		return nil
	}
	return statusError(res, "modify channel")
}

func (c *Client) CreateMarker(ctx context.Context, broadcasterID, description string) error {
	body, err := codec.Marshal(struct {
		UserID      string `json:"user_id"`
		Description string `json:"description,omitempty"`
	}{broadcasterID, strings.TrimSpace(description)})
	if err != nil {
		return err
	}
	res, err := c.ExecuteAs(ctx, IdentityBroadcaster, broadcasterID, HelixCall{
		Method:   http.MethodPost,
		Endpoint: "/helix/streams/markers",
		Body:     body,
	})
	if err != nil {
		return err
	}
	defer drain(res)
	if res.StatusCode == http.StatusOK {
		return nil
	}
	return statusError(res, "create marker")
}

func (c *Client) StartCommercial(ctx context.Context, broadcasterID string, length int) error {
	body, err := codec.Marshal(struct {
		BroadcasterID string `json:"broadcaster_id"`
		Length        int    `json:"length"`
	}{broadcasterID, length})
	if err != nil {
		return err
	}
	res, err := c.ExecuteAs(ctx, IdentityBroadcaster, broadcasterID, HelixCall{
		Method:   http.MethodPost,
		Endpoint: "/helix/channels/commercial",
		Body:     body,
	})
	if err != nil {
		return err
	}
	defer drain(res)
	if res.StatusCode == http.StatusOK {
		return nil
	}
	return statusError(res, "start commercial")
}
