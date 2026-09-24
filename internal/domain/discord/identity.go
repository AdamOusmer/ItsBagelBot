// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

import "strings"

const PremiumNick = "ItsBagelBot - Premium"

const (
	StatusPaid = "paid"
	StatusVIP  = "vip"
)

func IsPremium(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case StatusPaid, StatusVIP:
		return true
	default:
		return false
	}
}

type GuildIdentity struct {
	Premium bool `json:"premium"`
}

func (g GuildIdentity) Nick() (string, bool) {
	if !g.Premium {
		return "", false
	}
	return PremiumNick, true
}

func IdentityFor(status string) GuildIdentity {
	return GuildIdentity{Premium: IsPremium(status)}
}

func (g GuildIdentity) Fingerprint() string {
	if g.Premium {
		return "premium"
	}
	return "default"
}
