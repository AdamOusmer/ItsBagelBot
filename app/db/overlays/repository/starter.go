// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"fmt"

	"ItsBagelBot/app/db/overlays/templates"
	overlaysrpc "ItsBagelBot/internal/domain/rpc/overlays"
)

// IsStarter reports whether key names an embedded starter document.
func IsStarter(key string) bool {
	_, found, _ := templates.Lookup(key)
	return found
}

// AddStarter creates a custom overlay from the embedded starter document and returns its link token once.
func (r *Overlays) AddStarter(ctx context.Context, userID uint64, key string) (overlaysrpc.Overlay, string, error) {
	doc, found, err := templates.Lookup(key)
	if !found {
		return overlaysrpc.Overlay{}, "", ErrUnknownTemplate
	}
	if err != nil {
		return overlaysrpc.Overlay{}, "", fmt.Errorf("%w: %w", ErrInvalidDocument, err)
	}
	return r.CreateCustom(ctx, userID, doc)
}
