// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package identity holds the bot's premium guild avatar as an embedded asset.
//
// It lives in outgress because outgress is the only process that talks to
// Discord, and it is embedded rather than mounted because these images run on
// distroless containers with no filesystem to read from -- the same reason
// every other asset in this fleet is embedded.
package identity

import (
	_ "embed"
	"encoding/base64"
	"sync"
)

// premiumAvatarPNG is the premium bot avatar. It is the same artwork as
// web/src/assets/premium-logo.png, resized from 1024x1024 to 256x256:
//
//	1024x1024   ~1085 KB as a base64 data URI
//	 256x256      ~86 KB
//
// Discord renders a member avatar at 128px or smaller nearly everywhere, so
// the larger source bought nothing and cost 12x the upload on a call that is
// made once per premium guild. Do not regenerate this from anything but the
// committed brand asset.
//
//go:embed premium-avatar.png
var premiumAvatarPNG []byte

// dataURI is computed once. The encoding is deterministic and the asset never
// changes at runtime, so re-encoding ~66 KB on every apply would be pure waste
// on a path that can fire once per guild during a reconnect storm.
var dataURI = sync.OnceValue(func() string {
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(premiumAvatarPNG)
})

// PremiumAvatarDataURI returns the avatar in Discord's image-data format.
func PremiumAvatarDataURI() string { return dataURI() }
