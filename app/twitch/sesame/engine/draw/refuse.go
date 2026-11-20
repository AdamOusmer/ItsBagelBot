// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package draw

import (
	"context"

	"ItsBagelBot/pkg/valkey"
)

type Refusal struct {
	Round  Round
	Viewer ViewerID
	Reason string
}

func (s *Store) Refuse(ctx context.Context, req Refusal) (bool, int64, error) {
	keys := keysFor(req.Round.Channel, req.Round.ID)
	return keys.Refuse(ctx, s.client, valkey.Refusal{Viewer: string(req.Viewer), Reason: req.Reason, TTL: roundTTL, Channel: uint64(req.Round.Channel)})
}
