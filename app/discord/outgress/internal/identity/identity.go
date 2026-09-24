// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package identity

import (
	_ "embed"
	"encoding/base64"
	"sync"
)

//go:embed premium-avatar.png
var premiumAvatarPNG []byte

var dataURI = sync.OnceValue(func() string {
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(premiumAvatarPNG)
})

func PremiumAvatarDataURI() string { return dataURI() }
