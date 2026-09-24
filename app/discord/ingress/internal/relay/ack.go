// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package relay

import (
	"context"

	"ItsBagelBot/internal/discordapi"
	"ItsBagelBot/pkg/codec"
)

const deferredResponseType = 5

type interactionHeader struct {
	ID    string `json:"id"`
	Token string `json:"token"`
}

func (r *Relay) deferInteraction(ctx context.Context, raw []byte) error {
	var in interactionHeader
	if err := codec.Unmarshal(raw, &in); err != nil {
		return err
	}
	if r.REST == nil {
		return nil
	}
	return r.REST.InteractionCallback(ctx, discordapi.Callback{
		Interaction: discordapi.Interaction{ID: in.ID, Token: in.Token},
		Type:        deferredResponseType,
	})
}
