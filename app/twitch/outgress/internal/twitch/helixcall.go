// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package twitch

import (
	"context"
	"io"
	"net/http"

	"ItsBagelBot/pkg/codec"
)

func (c *Client) Send(ctx context.Context, id Identity, broadcasterID string, call HelixCall) error {
	res, err := c.ExecuteAs(ctx, id, broadcasterID, call)
	if err != nil {
		return err
	}
	defer drain(res)
	if res.StatusCode >= http.StatusOK && res.StatusCode < http.StatusMultipleChoices {
		return nil
	}
	return statusError(res, call.Method+" "+call.Endpoint)
}

type helixTarget struct {
	identity      Identity
	broadcasterID string
}

func broadcasterTarget(id string) helixTarget { return helixTarget{IdentityBroadcaster, id} }
func botTarget(id string) helixTarget         { return helixTarget{IdentityBot, id} }
func appTarget() helixTarget                  { return helixTarget{identity: IdentityApp} }

func (c *Client) sendJSON(ctx context.Context, target helixTarget, call HelixCall, body any) error {
	raw, err := codec.Marshal(body)
	if err != nil {
		return err
	}
	call.Body = raw
	return c.Send(ctx, target.identity, target.broadcasterID, call)
}

type helixRead struct {
	target   helixTarget
	endpoint string
}

func readData[T any](ctx context.Context, c *Client, read helixRead) ([]T, error) {
	res, err := c.ExecuteAs(ctx, read.target.identity, read.target.broadcasterID, getCall(read.endpoint))
	if err != nil {
		return nil, err
	}
	defer drain(res)
	if res.StatusCode != http.StatusOK {
		return nil, statusError(res, "GET "+read.endpoint)
	}
	var payload struct {
		Data []T `json:"data"`
	}
	if err := codec.NewDecoder(io.LimitReader(res.Body, 2<<20)).Decode(&payload); err != nil {
		return nil, err
	}
	return payload.Data, nil
}

func readFirst[T any](ctx context.Context, c *Client, read helixRead) (T, bool, error) {
	var zero T
	data, err := readData[T](ctx, c, read)
	if err != nil || len(data) == 0 {
		return zero, false, err
	}
	return data[0], true, nil
}
