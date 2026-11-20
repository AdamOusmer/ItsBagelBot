// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package twitchauth

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"
)

var RequiredBroadcasterScopes = []string{
	"bits:read",
	"channel:bot",
	"channel:edit:commercial",
	"channel:manage:ads",
	"channel:manage:broadcast",
	"channel:manage:moderators",
	"channel:manage:polls",
	"channel:manage:predictions",
	"channel:manage:raids",
	"channel:manage:redemptions",
	"channel:manage:vips",
	"channel:moderate",
	"channel:read:ads",
	"channel:read:subscriptions",
	"clips:edit",
	"moderation:read",
	"moderator:read:followers",
	"moderator:read:unban_requests",
	"user:read:chat",
	"user:read:email",
	"user:read:moderated_channels",
	"user:write:chat",
}

var SetVersion = scopeSetVersion()

func scopeSetVersion() string {
	sorted := slices.Sorted(slices.Values(RequiredBroadcasterScopes))
	digest := sha256.Sum256([]byte(strings.Join(sorted, "\n")))
	return hex.EncodeToString(digest[:8])
}

func Gap(granted []string) []string {
	have := make(map[string]struct{}, len(granted))
	for _, scope := range granted {
		have[scope] = struct{}{}
	}

	var missing []string
	for _, scope := range RequiredBroadcasterScopes {
		if _, ok := have[scope]; !ok {
			missing = append(missing, scope)
		}
	}
	slices.Sort(missing)
	return missing
}
